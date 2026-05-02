package service

import (
	"context"
	"errors"
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
	ListQueue(limit int) ([]model.AIAnalysisQueueItem, error)
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
	FailedCount  int  `json:"failedCount"`
	Processing   bool `json:"processing"`
}

type aiAnalysisJobSpec struct {
	EntryID     int64
	FeedID      int64
	Content     string
	Title       string
	ContentMode string
	Language    string
}

type aiStreamProcessor struct {
	entries      repository.EntryRepository
	jobs         repository.AIAnalysisJobRepository
	settingsRepo repository.SettingsRepository
	aiService    AIService
	queue        chan int64
	wakeCh       chan struct{}
	stopCh       chan struct{}
	dispatcherCh chan struct{}
	wg           sync.WaitGroup

	mu      sync.Mutex
	pending map[int64]struct{}
	running map[int64]struct{}
	closed  bool
}

func NewAIStreamProcessor(
	entries repository.EntryRepository,
	jobs repository.AIAnalysisJobRepository,
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
		jobs:         jobs,
		settingsRepo: settingsRepo,
		aiService:    aiService,
		queue:        make(chan int64, queueSize),
		wakeCh:       make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
		dispatcherCh: make(chan struct{}),
		pending:      make(map[int64]struct{}),
		running:      make(map[int64]struct{}),
	}

	p.wg.Add(1)
	go p.dispatcher()

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

	ctx, cancel := context.WithTimeout(context.Background(), aiStatusCheckTimeout)
	defer cancel()

	queued, err := p.ensureQueuedJob(ctx, entryID, model.AIAnalysisJobSourceAuto)
	if err != nil {
		logger.Warn("ai stream enqueue failed", "module", "service", "action", "enqueue", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
		return
	}
	if queued {
		p.signalDispatch()
	}
}

func (p *aiStreamProcessor) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()

	close(p.stopCh)
	<-p.dispatcherCh
	close(p.queue)
	p.wg.Wait()
	logger.Info("ai stream processor stopped", "module", "service", "action", "stop", "resource", "ai_stream", "result", "ok")
}

func (p *aiStreamProcessor) dispatcher() {
	defer p.wg.Done()
	defer close(p.dispatcherCh)

	ctx := context.Background()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	p.resumeQueuedJobs(ctx)
	p.backfillMissingJobs(ctx)
	p.dispatchQueuedJobs(ctx)

	for {
		select {
		case <-p.stopCh:
			return
		case <-p.wakeCh:
			p.backfillMissingJobs(ctx)
			p.dispatchQueuedJobs(ctx)
		case <-ticker.C:
			p.backfillMissingJobs(ctx)
			p.dispatchQueuedJobs(ctx)
		}
	}
}

func (p *aiStreamProcessor) worker() {
	defer p.wg.Done()

	for entryID := range p.queue {
		p.markRunning(entryID, true)
		if p.jobs != nil {
			if err := p.jobs.MarkRunning(context.Background(), entryID); err != nil {
				logger.Warn("ai stream mark running failed", "module", "service", "action", "update", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
			}
		}

		spec, err := p.processEntry(entryID)
		if err != nil {
			if p.jobs != nil {
				if markErr := p.jobs.MarkFailed(context.Background(), entryID, err.Error()); markErr != nil {
					logger.Warn("ai stream mark failed failed", "module", "service", "action", "update", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", markErr)
				}
			}
		} else if spec != nil && p.jobs != nil {
			if markErr := p.jobs.MarkSucceeded(context.Background(), entryID, spec.ContentMode, spec.Language); markErr != nil {
				logger.Warn("ai stream mark succeeded failed", "module", "service", "action", "update", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", markErr)
			}
		}

		p.markRunning(entryID, false)
		p.finish(entryID)
		p.signalDispatch()
	}
}

func (p *aiStreamProcessor) GetProcessingStatus(entryID int64) AIEntryProcessingStatus {
	p.mu.Lock()
	_, queued := p.pending[entryID]
	_, running := p.running[entryID]
	p.mu.Unlock()

	if queued || running {
		return AIEntryProcessingStatus{
			Queued:     queued,
			Running:    running,
			Processing: true,
		}
	}

	if p.jobs != nil && entryID != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), aiStatusCheckTimeout)
		defer cancel()

		job, err := p.jobs.GetByEntryID(ctx, entryID)
		if err == nil && job != nil {
			queued = job.Status == model.AIAnalysisJobStatusQueued
			running = job.Status == model.AIAnalysisJobStatusRunning
			return AIEntryProcessingStatus{
				Queued:     queued,
				Running:    running,
				Processing: queued || running,
			}
		}
	}

	return AIEntryProcessingStatus{}
}

func (p *aiStreamProcessor) GetQueueStats() AIQueueStats {
	if p.jobs != nil {
		ctx, cancel := context.WithTimeout(context.Background(), aiStatusCheckTimeout)
		defer cancel()

		stats, err := p.jobs.GetQueueStats(ctx)
		if err == nil {
			pendingCount := stats.QueuedCount + stats.RunningCount
			return AIQueueStats{
				PendingCount: pendingCount,
				QueuedCount:  stats.QueuedCount,
				RunningCount: stats.RunningCount,
				FailedCount:  stats.FailedCount,
				Processing:   pendingCount > 0,
			}
		}
	}

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
		FailedCount:  0,
		Processing:   pendingCount > 0,
	}
}

func (p *aiStreamProcessor) ListQueue(limit int) ([]model.AIAnalysisQueueItem, error) {
	if p.jobs == nil {
		return []model.AIAnalysisQueueItem{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), aiStatusCheckTimeout)
	defer cancel()

	items, err := p.jobs.ListQueue(ctx, limit)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []model.AIAnalysisQueueItem{}, nil
	}
	return items, nil
}

func (p *aiStreamProcessor) EnsureQueued(entryID int64) AIEntryProcessingStatus {
	status := p.GetProcessingStatus(entryID)
	if entryID == 0 {
		return status
	}
	if status.Processing {
		p.signalDispatch()
		return status
	}

	ctx, cancel := context.WithTimeout(context.Background(), aiStatusCheckTimeout)
	defer cancel()

	queued, err := p.ensureQueuedJob(ctx, entryID, model.AIAnalysisJobSourceManual)
	if err != nil {
		logger.Warn("ai stream status check failed", "module", "service", "action", "status", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
		return status
	}
	if !queued {
		return status
	}

	p.dispatchQueuedJobs(ctx)
	p.signalDispatch()
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

func (p *aiStreamProcessor) processEntry(entryID int64) (*aiAnalysisJobSpec, error) {
	ctx, cancel := context.WithTimeout(context.Background(), aiTaskTimeout)
	defer cancel()

	spec, skipReason, err := p.loadJobSpec(ctx, entryID, model.AIAnalysisJobSourceManual)
	if err != nil {
		logger.Warn("ai stream load entry failed", "module", "service", "action", "fetch", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
		return nil, err
	}
	if spec == nil {
		logger.Debug("ai stream skip entry", "module", "service", "action", "process", "resource", "ai_stream", "result", "skipped", "entry_id", entryID, "reason", skipReason)
		return nil, errors.New(skipReason)
	}

	isReadability := spec.ContentMode == model.AIAnalysisContentModeReadability
	cached, err := p.aiService.GetCachedAnalysis(ctx, entryID, isReadability)
	if err != nil {
		logger.Warn("ai stream cached analysis lookup failed", "module", "service", "action", "process", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
		return spec, err
	}
	if cached != nil {
		return spec, nil
	}

	_, err = p.aiService.Analyze(ctx, entryID, spec.Content, spec.Title, isReadability)
	if err != nil {
		logger.Warn("ai stream analysis failed", "module", "service", "action", "process", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
		return spec, err
	}
	return spec, nil
}

func (p *aiStreamProcessor) shouldEnqueue(ctx context.Context, entryID int64, source string) (bool, *aiAnalysisJobSpec, error) {
	spec, skipReason, err := p.loadJobSpec(ctx, entryID, source)
	if err != nil {
		return false, nil, err
	}
	if spec == nil {
		if skipReason != "" {
			logger.Debug("ai stream enqueue skipped", "module", "service", "action", "enqueue", "resource", "ai_stream", "result", "skipped", "entry_id", entryID, "reason", skipReason)
		}
		return false, nil, nil
	}

	isReadability := spec.ContentMode == model.AIAnalysisContentModeReadability
	cached, err := p.aiService.GetCachedAnalysis(ctx, entryID, isReadability)
	if err != nil {
		return false, nil, err
	}
	if cached != nil {
		if markErr := p.jobs.MarkSucceeded(ctx, entryID, spec.ContentMode, spec.Language); markErr != nil {
			logger.Warn("ai stream mark succeeded from cache failed", "module", "service", "action", "update", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", markErr)
		}
		return false, spec, nil
	}

	return true, spec, nil
}

func (p *aiStreamProcessor) ensureQueuedJob(ctx context.Context, entryID int64, source string) (bool, error) {
	if p.jobs == nil {
		return false, nil
	}

	shouldEnqueue, spec, err := p.shouldEnqueue(ctx, entryID, source)
	if err != nil {
		return false, err
	}
	if !shouldEnqueue || spec == nil {
		return false, nil
	}

	if err := p.jobs.UpsertQueued(ctx, spec.EntryID, spec.FeedID, source, spec.ContentMode, spec.Language); err != nil {
		return false, err
	}
	return true, nil
}

func (p *aiStreamProcessor) loadJobSpec(ctx context.Context, entryID int64, source string) (*aiAnalysisJobSpec, string, error) {
	entry, err := p.entries.GetByID(ctx, entryID)
	if err != nil {
		return nil, "", err
	}

	content, isReadability := entryContentForAI(entry)
	if content == "" {
		return nil, "empty content", nil
	}

	autoAnalysis := p.getBoolSetting(ctx, keyAIAutoAnalysis) || p.getBoolSetting(ctx, keyAIAutoSummary)
	if !autoAnalysis {
		return nil, "auto analysis disabled", nil
	}
	if source == model.AIAnalysisJobSourceAuto && !isEntryWithinCurrentLocalDay(entry, time.Now()) {
		return nil, "outside current day", nil
	}

	title := ""
	if entry.Title != nil {
		title = strings.TrimSpace(*entry.Title)
	}

	contentMode := model.AIAnalysisContentModeOriginal
	if isReadability {
		contentMode = model.AIAnalysisContentModeReadability
	}

	return &aiAnalysisJobSpec{
		EntryID:     entry.ID,
		FeedID:      entry.FeedID,
		Content:     content,
		Title:       title,
		ContentMode: contentMode,
		Language:    p.aiService.GetSummaryLanguage(ctx),
	}, "", nil
}

func (p *aiStreamProcessor) getBoolSetting(ctx context.Context, key string) bool {
	setting, err := p.settingsRepo.Get(ctx, key)
	if err != nil || setting == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(setting.Value), "true")
}

func (p *aiStreamProcessor) backfillMissingJobs(ctx context.Context) {
	if p.jobs == nil {
		return
	}

	const batchSize = 64
	dayStart, dayEnd := currentLocalDayRange(time.Now())
	entryIDs, err := p.jobs.ListBackfillEntryIDs(ctx, dayStart, dayEnd, batchSize)
	if err != nil {
		logger.Warn("ai stream backfill query failed", "module", "service", "action", "enqueue", "resource", "ai_stream", "result", "failed", "error", err)
		return
	}

	for _, entryID := range entryIDs {
		queued, err := p.ensureQueuedJob(ctx, entryID, model.AIAnalysisJobSourceAuto)
		if err != nil {
			logger.Warn("ai stream backfill enqueue failed", "module", "service", "action", "enqueue", "resource", "ai_stream", "result", "failed", "entry_id", entryID, "error", err)
			continue
		}
		if queued {
			logger.Debug("ai stream backfill queued", "module", "service", "action", "enqueue", "resource", "ai_stream", "result", "ok", "entry_id", entryID)
		}
	}
}

func (p *aiStreamProcessor) resumeQueuedJobs(ctx context.Context) {
	if p.jobs == nil {
		return
	}

	if _, err := p.jobs.RequeueRunning(ctx); err != nil {
		logger.Warn("ai stream requeue running jobs failed", "module", "service", "action", "start", "resource", "ai_stream", "result", "failed", "error", err)
	}
}

func (p *aiStreamProcessor) dispatchQueuedJobs(ctx context.Context) {
	if p.jobs == nil {
		return
	}

	freeSlots := cap(p.queue) - len(p.queue)
	if freeSlots <= 0 {
		return
	}

	dayStart, dayEnd := currentLocalDayRange(time.Now())
	hasTodayPending, err := p.jobs.HasPendingForDay(ctx, dayStart, dayEnd)
	if err != nil {
		logger.Warn("ai stream current-day pending query failed", "module", "service", "action", "enqueue", "resource", "ai_stream", "result", "failed", "error", err)
		return
	}

	entryIDs, err := p.jobs.ListQueuedEntryIDs(ctx, dayStart, dayEnd, !hasTodayPending, freeSlots)
	if err != nil {
		logger.Warn("ai stream dispatch query failed", "module", "service", "action", "enqueue", "resource", "ai_stream", "result", "failed", "error", err)
		return
	}

	for _, entryID := range entryIDs {
		if !p.enqueuePending(entryID) {
			break
		}
	}
}

func (p *aiStreamProcessor) enqueuePending(entryID int64) bool {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false
	}
	if _, exists := p.pending[entryID]; exists {
		p.mu.Unlock()
		return true
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
		return true
	default:
		p.finish(entryID)
		return false
	}
}

func (p *aiStreamProcessor) signalDispatch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}

	select {
	case p.wakeCh <- struct{}{}:
	default:
	}
}

func firstChannelError(errCh <-chan error) error {
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func currentLocalDayRange(now time.Time) (time.Time, time.Time) {
	localNow := now.In(time.Local)
	dayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	return dayStart, dayStart.Add(24 * time.Hour)
}

func entryAnalysisTime(entry model.Entry) time.Time {
	if entry.PublishedAt != nil && !entry.PublishedAt.IsZero() {
		return *entry.PublishedAt
	}
	return entry.CreatedAt
}

func isEntryWithinCurrentLocalDay(entry model.Entry, now time.Time) bool {
	dayStart, dayEnd := currentLocalDayRange(now)
	entryTime := entryAnalysisTime(entry)
	return !entryTime.Before(dayStart) && entryTime.Before(dayEnd)
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
