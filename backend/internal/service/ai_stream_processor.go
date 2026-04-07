package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"gist/backend/internal/model"
	"gist/backend/internal/repository"
	"gist/backend/pkg/logger"
)

const (
	defaultAIStreamWorkers   = 2
	defaultAIStreamQueueSize = 256
	aiTaskTimeout            = 2 * time.Minute
	aiStatusCheckTimeout     = 5 * time.Second
)

// EntryIngestor accepts newly discovered entry IDs for asynchronous processing.
type EntryIngestor interface {
	Enqueue(entryID int64)
}

// EntryAIStreamProcessor processes newly ingested entries in the background.
type EntryAIStreamProcessor interface {
	EntryIngestor
	GetProcessingStatus(entryID int64) AIEntryProcessingStatus
	GetQueueStats() AIQueueStats
	EnsureQueued(entryID int64) AIEntryProcessingStatus
	Close()
}

type AIEntryProcessingStatus struct {
	Queued     bool `json:"queued"`
	Running    bool `json:"running"`
	Processing bool `json:"processing"`
}

type AIQueueStats struct {
	PendingCount int  `json:"pendingCount"`
	QueuedCount  int  `json:"queuedCount"`
	RunningCount int  `json:"runningCount"`
	Processing   bool `json:"processing"`
}

type aiStreamProcessor struct {
	entries      repository.EntryRepository
	settingsRepo repository.SettingsRepository
	aiService    AIService
	queue        chan int64
	wg           sync.WaitGroup

	mu      sync.Mutex
	pending map[int64]struct{}
	running map[int64]struct{}
	closed  bool
}

func NewAIStreamProcessor(
	entries repository.EntryRepository,
	settingsRepo repository.SettingsRepository,
	aiService AIService,
	workerCount int,
	queueSize int,
) EntryAIStreamProcessor {
	if workerCount <= 0 {
		workerCount = defaultAIStreamWorkers
	}
	if queueSize <= 0 {
		queueSize = defaultAIStreamQueueSize
	}

	p := &aiStreamProcessor{
		entries:      entries,
		settingsRepo: settingsRepo,
		aiService:    aiService,
		queue:        make(chan int64, queueSize),
		pending:      make(map[int64]struct{}),
		running:      make(map[int64]struct{}),
	}

	for i := 0; i < workerCount; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	logger.Info("ai stream processor started", "module", "service", "action", "start", "resource", "ai_stream", "result", "ok", "workers", workerCount, "queue_size", queueSize)
	return p
}

func (p *aiStreamProcessor) Enqueue(entryID int64) {
	if entryID == 0 {
		return
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	if _, exists := p.pending[entryID]; exists {
		p.mu.Unlock()
		return
	}
	p.pending[entryID] = struct{}{}
	p.mu.Unlock()

	defer func() {
		if recover() != nil {
			p.finish(entryID)
		}
	}()

	select {
	case p.queue <- entryID:
		logger.Debug("ai stream entry queued", "module", "service", "action", "enqueue", "resource", "ai_stream", "result", "ok", "entry_id", entryID)
	default:
		p.finish(entryID)
		logger.Warn("ai stream queue full", "module", "service", "action", "enqueue", "resource", "ai_stream", "result", "failed", "entry_id", entryID)
	}
}

func (p *aiStreamProcessor) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.queue)
	p.mu.Unlock()

	p.wg.Wait()
	logger.Info("ai stream processor stopped", "module", "service", "action", "stop", "resource", "ai_stream", "result", "ok")
}

func (p *aiStreamProcessor) worker() {
	defer p.wg.Done()

	for entryID := range p.queue {
		p.markRunning(entryID, true)
		p.processEntry(entryID)
		p.markRunning(entryID, false)
		p.finish(entryID)
	}
}

func (p *aiStreamProcessor) GetProcessingStatus(entryID int64) AIEntryProcessingStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, queued := p.pending[entryID]
	_, running := p.running[entryID]
	return AIEntryProcessingStatus{
		Queued:     queued,
		Running:    running,
		Processing: queued || running,
	}
}

func (p *aiStreamProcessor) GetQueueStats() AIQueueStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	pendingCount := len(p.pending)
	runningCount := len(p.running)
	queuedCount := pendingCount - runningCount
	if queuedCount < 0 {
		queuedCount = 0
	}

	return AIQueueStats{
		PendingCount: pendingCount,
		QueuedCount:  queuedCount,
		RunningCount: runningCount,
		Processing:   pendingCount > 0,
	}
}

func (p *aiStreamProcessor) EnsureQueued(entryID int64) AIEntryProcessingStatus {
	status := p.GetProcessingStatus(entryID)
	if entryID == 0 || status.Processing {
		return status
	}

	ctx, cancel := context.WithTimeout(context.Background(), aiStatusCheckTimeout)
	defer cancel()

	shouldEnqueue, err := p.shouldEnqueue(ctx, entryID)
	if err != nil {
		logger.Warn("ai stream status check failed", "module", "service", "action", "status", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
		return status
	}
	if !shouldEnqueue {
		return status
	}

	p.Enqueue(entryID)
	return p.GetProcessingStatus(entryID)
}

func (p *aiStreamProcessor) finish(entryID int64) {
	p.mu.Lock()
	delete(p.pending, entryID)
	p.mu.Unlock()
}

func (p *aiStreamProcessor) markRunning(entryID int64, running bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if running {
		p.running[entryID] = struct{}{}
		return
	}
	delete(p.running, entryID)
}

func (p *aiStreamProcessor) processEntry(entryID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), aiTaskTimeout)
	defer cancel()

	entry, err := p.entries.GetByID(ctx, entryID)
	if err != nil {
		logger.Warn("ai stream load entry failed", "module", "service", "action", "fetch", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
		return
	}

	content, isReadability := entryContentForAI(entry)
	if content == "" {
		logger.Debug("ai stream skip empty content", "module", "service", "action", "process", "resource", "ai_stream", "result", "skipped", "entry_id", entryID)
		return
	}

	title := ""
	if entry.Title != nil {
		title = strings.TrimSpace(*entry.Title)
	}

	autoSummary := p.getBoolSetting(ctx, keyAIAutoSummary)
	autoAnalysis := p.getBoolSetting(ctx, keyAIAutoAnalysis)
	if !autoSummary && !autoAnalysis {
		return
	}

	if autoSummary {
		if err := p.ensureSummary(ctx, entryID, content, title, isReadability); err != nil {
			logger.Warn("ai stream summary failed", "module", "service", "action", "process", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
		}
	}

	if autoAnalysis {
		if err := p.ensureAnalysis(ctx, entryID, content, title, isReadability); err != nil {
			logger.Warn("ai stream analysis failed", "module", "service", "action", "process", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
		}
	}
}

func (p *aiStreamProcessor) shouldEnqueue(ctx context.Context, entryID int64) (bool, error) {
	entry, err := p.entries.GetByID(ctx, entryID)
	if err != nil {
		return false, err
	}

	content, isReadability := entryContentForAI(entry)
	if content == "" {
		return false, nil
	}

	autoSummary := p.getBoolSetting(ctx, keyAIAutoSummary)
	autoAnalysis := p.getBoolSetting(ctx, keyAIAutoAnalysis)
	if !autoSummary && !autoAnalysis {
		return false, nil
	}

	if autoSummary {
		cached, err := p.aiService.GetCachedSummary(ctx, entryID, isReadability)
		if err != nil {
			return false, err
		}
		if cached == nil {
			return true, nil
		}
	}

	if autoAnalysis {
		cached, err := p.aiService.GetCachedAnalysis(ctx, entryID, isReadability)
		if err != nil {
			return false, err
		}
		if cached == nil {
			return true, nil
		}
	}

	return false, nil
}

func (p *aiStreamProcessor) ensureSummary(ctx context.Context, entryID int64, content, title string, isReadability bool) error {
	cached, err := p.aiService.GetCachedSummary(ctx, entryID, isReadability)
	if err != nil {
		return err
	}
	if cached != nil {
		return nil
	}

	textCh, errCh, err := p.aiService.Summarize(ctx, entryID, content, title, isReadability)
	if err != nil {
		return err
	}

	var builder strings.Builder
	for text := range textCh {
		builder.WriteString(text)
	}
	if err := firstChannelError(errCh); err != nil {
		return err
	}
	if builder.Len() == 0 {
		return nil
	}
	return p.aiService.SaveSummary(ctx, entryID, isReadability, builder.String())
}

func (p *aiStreamProcessor) ensureAnalysis(ctx context.Context, entryID int64, content, title string, isReadability bool) error {
	cached, err := p.aiService.GetCachedAnalysis(ctx, entryID, isReadability)
	if err != nil {
		return err
	}
	if cached != nil {
		return nil
	}

	_, err = p.aiService.Analyze(ctx, entryID, content, title, isReadability)
	return err
}

func (p *aiStreamProcessor) getBoolSetting(ctx context.Context, key string) bool {
	setting, err := p.settingsRepo.Get(ctx, key)
	if err != nil || setting == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(setting.Value), "true")
}

func firstChannelError(errCh <-chan error) error {
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func entryContentForAI(entry model.Entry) (content string, isReadability bool) {
	if entry.ReadableContent != nil {
		if trimmed := strings.TrimSpace(*entry.ReadableContent); trimmed != "" {
			return trimmed, true
		}
	}
	if entry.Content != nil {
		return strings.TrimSpace(*entry.Content), false
	}
	return "", false
}
