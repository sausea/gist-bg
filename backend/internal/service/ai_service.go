//go:generate mockgen -source=$GOFILE -destination=mock/$GOFILE -package=mock
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gist/backend/internal/config"
	"gist/backend/internal/hashutil"
	"gist/backend/internal/model"
	"gist/backend/internal/repository"
	"gist/backend/internal/service/ai"
	"gist/backend/pkg/logger"
)

// TranslateBlockResult represents a translated block result.
type TranslateBlockResult struct {
	Index int    `json:"index"`
	HTML  string `json:"html"`
}

// TranslateBlockInfo represents original block info.
type TranslateBlockInfo struct {
	Index         int
	HTML          string
	NeedTranslate bool
}

// BatchArticleInput represents input for batch translation.
type BatchArticleInput struct {
	ID      string
	Title   string
	Summary string
}

// BatchTranslateResult represents a single article's translation result.
type BatchTranslateResult struct {
	ID      string  `json:"id"`
	Title   *string `json:"title"`
	Summary *string `json:"summary"`
	Cached  bool    `json:"cached,omitempty"`
}

type ArticleAnalysisResult struct {
	Tag        string   `json:"tag"`
	Summary    string   `json:"summary"`
	Entities   []string `json:"entities"`
	Sentiment  string   `json:"sentiment"`
	Importance int      `json:"importance"`
	Latitude   *float64 `json:"latitude,omitempty"`
	Longitude  *float64 `json:"longitude,omitempty"`
}

type ArticleLocationResult struct {
	Location  *string  `json:"location,omitempty"`
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
}

type DailyReportNarrativeResult struct {
	Overview     string `json:"overview"`
	RiskReview   string `json:"riskReview"`
	TrendOutlook string `json:"trendOutlook"`
}

type dailyReportNarrativeCachePayload struct {
	Signature    string `json:"signature"`
	Overview     string `json:"overview"`
	RiskReview   string `json:"riskReview"`
	TrendOutlook string `json:"trendOutlook"`
	UpdatedAt    string `json:"updatedAt"`
}

type geocodeSearchResult struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
}

var geocodeSearchEndpoint = "https://nominatim.openstreetmap.org/search"

const storedAnalysisTitleLanguage = "zh-CN"

const (
	aiSceneAnalysis    = "analysis"
	aiSceneTranslation = "translation"
	aiSceneReport      = "report"

	dailyReportNarrativeCacheKeyPrefix = "cache.ai_daily_report."
	dailyArchiveReportStartMarker      = "<!-- GIST_DAILY_REPORT_START -->"
	dailyArchiveReportEndMarker        = "<!-- GIST_DAILY_REPORT_END -->"
	aiUsageRecordTimeout               = 3 * time.Second
)

type AIServiceOption func(*aiService)

type dailyArchiveItem struct {
	Root           string
	FolderSegments []string
	FeedTitle      string
	EntryTitle     string
	OriginalTitle  *string
	Analysis       model.StoredAIAnalysis
}

// AIService provides AI-related operations like summarization and translation.
type AIService interface {
	// GetCachedSummary returns a cached summary if available.
	GetCachedSummary(ctx context.Context, entryID int64, isReadability bool) (*model.AISummary, error)
	// Summarize generates a summary using AI streaming.
	// Returns channels for text chunks and errors.
	Summarize(ctx context.Context, entryID int64, content, title string, isReadability bool) (<-chan string, <-chan error, error)
	// SaveSummary saves a summary to cache.
	SaveSummary(ctx context.Context, entryID int64, isReadability bool, summary string) error
	// GetSummaryLanguage returns the configured summary language.
	GetSummaryLanguage(ctx context.Context) string

	// GetCachedTranslation returns a cached translation if available.
	GetCachedTranslation(ctx context.Context, entryID int64, isReadability bool) (*model.AITranslation, error)
	// GetCachedAnalysis returns a cached structured analysis if available.
	GetCachedAnalysis(ctx context.Context, entryID int64, isReadability bool) (*model.AIAnalysis, error)
	// TranslateBlocks parses HTML into blocks and translates them in parallel.
	// Returns block info, a channel of results (in completion order), and an error channel.
	TranslateBlocks(ctx context.Context, entryID int64, content, title string, isReadability bool) ([]TranslateBlockInfo, <-chan TranslateBlockResult, <-chan error, error)
	// SaveTranslation saves a translation to cache.
	SaveTranslation(ctx context.Context, entryID int64, isReadability bool, content string) error
	// Analyze generates a structured analysis for the article.
	Analyze(ctx context.Context, entryID int64, content, title string, isReadability bool) (*model.AIAnalysis, error)
	// ListStoredAnalyses returns stored AI analyses with entry metadata.
	ListStoredAnalyses(ctx context.Context, limit, offset int) ([]model.StoredAIAnalysis, error)
	// BuildDailyAnalysisReport aggregates stored AI analyses for the given day.
	BuildDailyAnalysisReport(ctx context.Context, day time.Time) (*model.AIDailyReport, error)
	// TranslateBatch translates multiple articles' titles and summaries.
	// Returns a channel of results and an error channel.
	TranslateBatch(ctx context.Context, articles []BatchArticleInput) (<-chan BatchTranslateResult, <-chan error, error)
	// ClearAllCache deletes all AI cache data (summaries, translations, list translations).
	// Returns the number of deleted records for each type.
	ClearAllCache(ctx context.Context) (summaries, translations, listTranslations, analyses int64, err error)
}

type aiService struct {
	summaryRepo         repository.AISummaryRepository
	translationRepo     repository.AITranslationRepository
	listTranslationRepo repository.AIListTranslationRepository
	analysisRepo        repository.AIAnalysisRepository
	focusTagRepo        repository.EntryFocusTagRepository
	settingsRepo        repository.SettingsRepository
	rateLimiter         *ai.RateLimiter
	entryRepo           repository.EntryRepository
	feedRepo            repository.FeedRepository
	folderRepo          repository.FolderRepository
	analysisArchiveDir  string
	promptManager       *ai.PromptManager
	usageMu             sync.Mutex
}

// NewAIService creates a new AI service.
func NewAIService(
	summaryRepo repository.AISummaryRepository,
	translationRepo repository.AITranslationRepository,
	listTranslationRepo repository.AIListTranslationRepository,
	analysisRepo repository.AIAnalysisRepository,
	settingsRepo repository.SettingsRepository,
	rateLimiter *ai.RateLimiter,
	options ...AIServiceOption,
) AIService {
	svc := &aiService{
		summaryRepo:         summaryRepo,
		translationRepo:     translationRepo,
		listTranslationRepo: listTranslationRepo,
		analysisRepo:        analysisRepo,
		settingsRepo:        settingsRepo,
		rateLimiter:         rateLimiter,
		promptManager:       ai.NewPromptManager(""),
	}
	for _, option := range options {
		if option != nil {
			option(svc)
		}
	}
	return svc
}

func WithAIAnalysisArchive(
	archiveDir string,
	entryRepo repository.EntryRepository,
	feedRepo repository.FeedRepository,
	folderRepo repository.FolderRepository,
) AIServiceOption {
	return func(s *aiService) {
		s.analysisArchiveDir = strings.TrimSpace(archiveDir)
		s.entryRepo = entryRepo
		s.feedRepo = feedRepo
		s.folderRepo = folderRepo
	}
}

func WithAIEntryFocusTags(repo repository.EntryFocusTagRepository) AIServiceOption {
	return func(s *aiService) {
		s.focusTagRepo = repo
	}
}

func WithAIPromptManager(manager *ai.PromptManager) AIServiceOption {
	return func(s *aiService) {
		if manager != nil {
			s.promptManager = manager
		}
	}
}

func (s *aiService) prompts() *ai.PromptManager {
	if s.promptManager == nil {
		s.promptManager = ai.NewPromptManager("")
	}
	return s.promptManager
}

func (s *aiService) GetCachedSummary(ctx context.Context, entryID int64, isReadability bool) (*model.AISummary, error) {
	language := s.GetSummaryLanguage(ctx)
	summary, err := s.summaryRepo.Get(ctx, entryID, isReadability, language)
	if err != nil || summary != nil {
		return summary, err
	}

	analysis, err := s.analysisRepo.Get(ctx, entryID, isReadability, language)
	if err != nil || analysis == nil || strings.TrimSpace(analysis.Summary) == "" {
		return nil, err
	}

	return &model.AISummary{
		EntryID:       analysis.EntryID,
		IsReadability: analysis.IsReadability,
		Language:      analysis.Language,
		Summary:       analysis.Summary,
		CreatedAt:     analysis.CreatedAt,
	}, nil
}

func (s *aiService) Summarize(ctx context.Context, entryID int64, content, title string, isReadability bool) (<-chan string, <-chan error, error) {
	// Get AI configuration
	cfg, err := s.getAIConfig(ctx, aiSceneAnalysis)
	if err != nil {
		return nil, nil, err
	}

	// Create provider
	provider, err := ai.NewProvider(cfg)
	if err != nil {
		logger.Warn("ai provider create failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "provider", cfg.Provider, "model", cfg.Model, "error", err)
		return nil, nil, fmt.Errorf("create provider: %w", err)
	}

	// Wait for rate limiter
	if err := s.rateLimiter.Wait(ctx); err != nil {
		logger.Warn("ai rate limit wait failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "error", err)
		return nil, nil, fmt.Errorf("rate limit: %w", err)
	}

	// Get language setting
	language := s.GetSummaryLanguage(ctx)

	// Build system prompt
	systemPrompt := s.prompts().SummarizePrompt(title, language)

	// Convert HTML to plain text to save tokens
	plainText := ai.HTMLToText(content)

	// Wrap input with <input> tags
	wrappedInput := ai.WrapInput(plainText)

	// Start streaming
	upstreamTextCh, upstreamErrCh := provider.SummarizeStream(ctx, systemPrompt, wrappedInput)
	textCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(textCh)
		defer close(errCh)

		var builder strings.Builder
		var upstreamErr error
		currentTextCh := upstreamTextCh
		currentErrCh := upstreamErrCh

		for currentTextCh != nil || currentErrCh != nil {
			select {
			case chunk, ok := <-currentTextCh:
				if !ok {
					currentTextCh = nil
					continue
				}
				builder.WriteString(chunk)
				select {
				case textCh <- chunk:
				case <-ctx.Done():
					return
				}
			case err, ok := <-currentErrCh:
				if !ok {
					currentErrCh = nil
					continue
				}
				if err == nil {
					continue
				}
				upstreamErr = err
				select {
				case errCh <- err:
				default:
				}
			case <-ctx.Done():
				return
			}
		}

		if upstreamErr == nil && ctx.Err() == nil {
			s.recordAIUsageEstimate(aiSceneAnalysis, systemPrompt, wrappedInput, builder.String())
		}
	}()

	logger.Info("ai summarize stream started", "module", "service", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID, "provider", cfg.Provider, "model", cfg.Model)

	return textCh, errCh, nil
}

func (s *aiService) SaveSummary(ctx context.Context, entryID int64, isReadability bool, summary string) error {
	language := s.GetSummaryLanguage(ctx)
	if err := s.summaryRepo.Save(ctx, entryID, isReadability, language, summary); err != nil {
		logger.Warn("ai summary save failed", "module", "service", "action", "save", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return err
	}
	logger.Info("ai summary saved", "module", "service", "action", "save", "resource", "ai", "result", "ok", "entry_id", entryID, "readability", isReadability)
	return nil
}

func (s *aiService) GetSummaryLanguage(ctx context.Context) string {
	setting, err := s.settingsRepo.Get(ctx, "ai.summary_language")
	if err != nil || setting == nil || setting.Value == "" {
		return "zh-CN" // default
	}
	return setting.Value
}

func (s *aiService) getAIConfig(ctx context.Context, scene string) (ai.Config, error) {
	var cfg ai.Config

	// Batch fetch all ai.* settings in a single query
	settings, err := s.settingsRepo.GetByPrefix(ctx, "ai.")
	if err != nil {
		return cfg, fmt.Errorf("get AI settings: %w", err)
	}

	// Build a map for quick lookup
	settingsMap := make(map[string]string, len(settings))
	for _, s := range settings {
		settingsMap[s.Key] = s.Value
	}

	prefix := "ai."
	switch scene {
	case aiSceneTranslation:
		prefix = "ai.translate."
	case aiSceneReport:
		prefix = "ai.report."
	}

	getValue := func(suffix string) string {
		if prefix != "ai." {
			if value := settingsMap[prefix+suffix]; value != "" {
				return value
			}
		}
		return settingsMap["ai."+suffix]
	}

	// Get provider
	cfg.Provider = getValue("provider")
	if cfg.Provider == "" {
		cfg.Provider = ai.ProviderOpenAI
	}

	// Get API key
	cfg.APIKey = getValue("api_key")
	if cfg.APIKey == "" {
		return cfg, fmt.Errorf("AI API key is not configured")
	}

	// Get base URL
	cfg.BaseURL = getValue("base_url")

	// Get model
	cfg.Model = getValue("model")
	if cfg.Model == "" {
		return cfg, fmt.Errorf("AI model is not configured")
	}

	// Get thinking settings
	if strings.EqualFold(getValue("thinking"), "true") {
		cfg.Thinking = true
	}

	if val := getValue("thinking_budget"); val != "" {
		var budget int
		fmt.Sscanf(val, "%d", &budget)
		cfg.ThinkingBudget = budget
	}

	cfg.Endpoint = getValue("openai_endpoint")
	if cfg.Endpoint == "" {
		cfg.Endpoint = "responses"
	}
	cfg.ReasoningEffort = getValue("reasoning_effort")

	return cfg, nil
}

func (s *aiService) recordAIUsageEstimate(scene, promptText, inputText, outputText string) {
	promptTokens := estimateAITokens(promptText) + estimateAITokens(inputText)
	completionTokens := estimateAITokens(outputText)
	s.recordAIUsageCounts(scene, promptTokens, completionTokens)
}

func (s *aiService) recordAIUsageCounts(scene string, promptTokens, completionTokens int) {
	if s.settingsRepo == nil {
		return
	}
	if promptTokens <= 0 && completionTokens <= 0 {
		return
	}

	recordCtx, cancel := context.WithTimeout(context.Background(), aiUsageRecordTimeout)
	defer cancel()

	scene = normalizeAIUsageScene(scene)
	key := aiUsageDailyKey(time.Now())

	s.usageMu.Lock()
	defer s.usageMu.Unlock()

	var day storedAIUsageDayStats
	setting, err := s.settingsRepo.Get(recordCtx, key)
	if err != nil {
		logger.Warn("ai usage stats load failed", "module", "service", "action", "fetch", "resource", "ai_usage", "result", "failed", "key", key, "error", err)
		return
	}
	if setting != nil && strings.TrimSpace(setting.Value) != "" {
		day, err = decodeStoredAIUsageDayStats(setting.Value, parseAIUsageDateFromKey(key))
		if err != nil {
			logger.Warn("ai usage stats decode failed", "module", "service", "action", "fetch", "resource", "ai_usage", "result", "failed", "key", key, "error", err)
			day = storedAIUsageDayStats{}
		}
	}
	normalizeStoredAIUsageDayStats(&day, parseAIUsageDateFromKey(key))

	day.Totals.Add(promptTokens, completionTokens)
	sceneCounter := day.Scenes[scene]
	sceneCounter.Add(promptTokens, completionTokens)
	day.Scenes[scene] = sceneCounter

	data, err := json.Marshal(day)
	if err != nil {
		logger.Warn("ai usage stats encode failed", "module", "service", "action", "save", "resource", "ai_usage", "result", "failed", "key", key, "error", err)
		return
	}
	if err := s.settingsRepo.Set(recordCtx, key, string(data)); err != nil {
		logger.Warn("ai usage stats save failed", "module", "service", "action", "save", "resource", "ai_usage", "result", "failed", "key", key, "error", err)
	}
}

func (s *aiService) GetCachedTranslation(ctx context.Context, entryID int64, isReadability bool) (*model.AITranslation, error) {
	language := s.GetSummaryLanguage(ctx)
	translation, err := s.translationRepo.Get(ctx, entryID, isReadability, language)
	if err != nil {
		logger.Warn("ai translation cache lookup failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, err
	}
	return translation, nil
}

func (s *aiService) SaveTranslation(ctx context.Context, entryID int64, isReadability bool, content string) error {
	language := s.GetSummaryLanguage(ctx)
	if err := s.translationRepo.Save(ctx, entryID, isReadability, language, content); err != nil {
		logger.Warn("ai translation save failed", "module", "service", "action", "save", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return err
	}
	logger.Info("ai translation saved", "module", "service", "action", "save", "resource", "ai", "result", "ok", "entry_id", entryID, "readability", isReadability)
	return nil
}

func (s *aiService) GetCachedAnalysis(ctx context.Context, entryID int64, isReadability bool) (*model.AIAnalysis, error) {
	language := s.GetSummaryLanguage(ctx)
	analysis, err := s.analysisRepo.Get(ctx, entryID, isReadability, language)
	if err != nil {
		logger.Warn("ai analysis cache lookup failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, err
	}
	if analysis != nil && (analysis.Latitude == nil || analysis.Longitude == nil) {
		if enrichErr := s.enrichAnalysisCoordinates(ctx, analysis, "", ""); enrichErr != nil {
			logger.Warn("ai analysis coordinate backfill failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", enrichErr)
		} else if analysis.Latitude != nil && analysis.Longitude != nil {
			if saveErr := s.analysisRepo.Save(ctx, entryID, isReadability, language, *analysis); saveErr != nil {
				logger.Warn("ai analysis coordinate backfill save failed", "module", "service", "action", "save", "resource", "ai", "result", "failed", "entry_id", entryID, "error", saveErr)
			}
		}
	}
	if analysis != nil {
		s.ensureCachedAnalysisArchive(ctx, entryID, analysis)
	}
	return analysis, nil
}

func (s *aiService) Analyze(ctx context.Context, entryID int64, content, title string, isReadability bool) (*model.AIAnalysis, error) {
	cfg, err := s.getAIConfig(ctx, aiSceneAnalysis)
	if err != nil {
		logger.Warn("ai analysis get config failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, err
	}

	provider, err := ai.NewProvider(cfg)
	if err != nil {
		logger.Warn("ai analysis provider create failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "provider", cfg.Provider, "model", cfg.Model, "error", err)
		return nil, fmt.Errorf("create provider: %w", err)
	}

	if err := s.rateLimiter.Wait(ctx); err != nil {
		logger.Warn("ai analysis rate limit wait failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, fmt.Errorf("rate limit: %w", err)
	}

	language := s.GetSummaryLanguage(ctx)
	systemPrompt := s.prompts().ArticleAnalysisPrompt(title, language)
	plainText := ai.HTMLToText(content)
	wrappedInput := ai.WrapInput(plainText)

	raw, err := provider.Complete(ctx, systemPrompt, wrappedInput)
	if err != nil {
		logger.Warn("ai analysis complete failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, err
	}
	s.recordAIUsageEstimate(aiSceneAnalysis, systemPrompt, wrappedInput, raw)

	parsed, err := parseArticleAnalysis(raw)
	if err != nil {
		logger.Warn("ai analysis parse failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, err
	}

	analysis := &model.AIAnalysis{
		EntryID:       entryID,
		IsReadability: isReadability,
		Language:      language,
		Tag:           parsed.Tag,
		Summary:       parsed.Summary,
		Entities:      parsed.Entities,
		Sentiment:     parsed.Sentiment,
		Importance:    parsed.Importance,
		Latitude:      parsed.Latitude,
		Longitude:     parsed.Longitude,
	}

	if err := s.enrichAnalysisCoordinates(ctx, analysis, title, plainText); err != nil {
		logger.Warn("ai analysis coordinate enrich failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
	}

	if err := s.analysisRepo.Save(ctx, entryID, isReadability, language, *analysis); err != nil {
		logger.Warn("ai analysis cache save failed", "module", "service", "action", "save", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, err
	}
	analysis.CreatedAt = time.Now().In(time.Local)

	s.ensureStoredAnalysisTitleTranslation(ctx, entryID, title)
	s.archiveAnalysisMarkdown(ctx, entryID, *analysis)

	logger.Info("ai analysis completed", "module", "service", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID, "readability", isReadability)
	return analysis, nil
}

func (s *aiService) ListStoredAnalyses(ctx context.Context, limit, offset int) ([]model.StoredAIAnalysis, error) {
	items, err := s.analysisRepo.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	return s.attachFocusTags(ctx, items)
}

func (s *aiService) PublishDailyArchiveReport(ctx context.Context, day time.Time) error {
	report, err := s.BuildDailyAnalysisReport(ctx, day)
	if err != nil {
		return err
	}

	groupedItems, err := s.collectDailyArchiveItemsByRoot(ctx, day)
	if err != nil {
		return err
	}
	if len(groupedItems) == 0 {
		return nil
	}

	reportBlock := buildDailyArchiveReportBlock(report)
	roots := make([]string, 0, len(groupedItems))
	for root := range groupedItems {
		roots = append(roots, root)
	}
	sort.Strings(roots)

	for _, root := range roots {
		if err := s.writeDailyArchiveFile(day, root, groupedItems[root], reportBlock); err != nil {
			return err
		}
	}

	return nil
}

func (s *aiService) ensureStoredAnalysisTitleTranslation(ctx context.Context, entryID int64, title string) string {
	title = strings.TrimSpace(title)
	if entryID == 0 || title == "" {
		return title
	}

	cached, err := s.listTranslationRepo.Get(ctx, entryID, storedAnalysisTitleLanguage)
	if err != nil {
		logger.Warn("ai analysis title cache lookup failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return title
	}
	if cached != nil && strings.TrimSpace(cached.Title) != "" {
		return strings.TrimSpace(cached.Title)
	}

	cfg, err := s.getAIConfig(ctx, aiSceneTranslation)
	if err != nil {
		logger.Warn("ai analysis title get config failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return title
	}

	provider, err := ai.NewProvider(cfg)
	if err != nil {
		logger.Warn("ai analysis title provider create failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "provider", cfg.Provider, "model", cfg.Model, "error", err)
		return title
	}

	if err := s.rateLimiter.Wait(ctx); err != nil {
		logger.Warn("ai analysis title rate limit wait failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return title
	}

	systemPrompt := s.prompts().TranslateTextPrompt("title", storedAnalysisTitleLanguage)
	wrappedTitle := ai.WrapInput(title)
	translatedTitle, err := provider.Complete(ctx, systemPrompt, wrappedTitle)
	if err != nil {
		logger.Warn("ai analysis title translate failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return title
	}
	s.recordAIUsageEstimate(aiSceneTranslation, systemPrompt, wrappedTitle, translatedTitle)

	translatedTitle = strings.TrimSpace(translatedTitle)
	if translatedTitle == "" {
		return title
	}

	summary := ""
	if cached != nil {
		summary = cached.Summary
	}

	if err := s.listTranslationRepo.Save(ctx, entryID, storedAnalysisTitleLanguage, translatedTitle, summary); err != nil {
		logger.Warn("ai analysis title save failed", "module", "service", "action", "save", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return title
	}

	logger.Info("ai analysis title translated", "module", "service", "action", "save", "resource", "ai", "result", "ok", "entry_id", entryID, "language", storedAnalysisTitleLanguage)
	return translatedTitle
}

func (s *aiService) archiveAnalysisMarkdown(ctx context.Context, entryID int64, analysis model.AIAnalysis) {
	if s.entryRepo == nil || s.feedRepo == nil {
		return
	}

	entry, err := s.entryRepo.GetByID(ctx, entryID)
	if err != nil {
		logger.Warn("ai analysis archive load entry failed", "module", "service", "action", "fetch", "resource", "ai_archive", "result", "failed", "entry_id", entryID, "error", err)
		return
	}

	feed, err := s.feedRepo.GetByID(ctx, entry.FeedID)
	if err != nil {
		logger.Warn("ai analysis archive load feed failed", "module", "service", "action", "fetch", "resource", "ai_archive", "result", "failed", "entry_id", entryID, "feed_id", entry.FeedID, "error", err)
		return
	}

	archiveDir, _ := s.resolveAnalysisArchiveLocation(ctx, feed)
	archiveDir = normalizeArchiveRoot(archiveDir)
	if archiveDir == "" {
		return
	}

	archiveDay := analysis.CreatedAt
	if archiveDay.IsZero() {
		archiveDay = time.Now()
	}
	archiveDay = archiveDay.In(time.Local)

	groupedItems, err := s.collectDailyArchiveItemsByRoot(ctx, archiveDay)
	if err != nil {
		logger.Warn("ai analysis archive collect failed", "module", "service", "action", "fetch", "resource", "ai_archive", "result", "failed", "entry_id", entryID, "error", err)
		return
	}

	reportBlock := extractDailyArchiveReportBlockFromFile(dailyArchiveFilePath(archiveDir, archiveDay))
	if err := s.writeDailyArchiveFile(archiveDay, archiveDir, groupedItems[archiveDir], reportBlock); err != nil {
		logger.Warn("ai analysis archive write failed", "module", "service", "action", "save", "resource", "ai_archive", "result", "failed", "entry_id", entryID, "path", dailyArchiveFilePath(archiveDir, archiveDay), "error", err)
		return
	}

	logger.Info("ai analysis archived", "module", "service", "action", "save", "resource", "ai_archive", "result", "ok", "entry_id", entryID, "path", dailyArchiveFilePath(archiveDir, archiveDay))
}

func (s *aiService) resolveAnalysisArchiveLocation(ctx context.Context, feed model.Feed) (string, []string) {
	defaultArchiveDir := s.analysisArchiveDir
	if s.settingsRepo != nil {
		if setting, err := s.settingsRepo.Get(ctx, keyAIAnalysisArchiveDir); err == nil && setting != nil && strings.TrimSpace(setting.Value) != "" {
			defaultArchiveDir = strings.TrimSpace(setting.Value)
		}
	}

	if s.folderRepo == nil || feed.FolderID == nil {
		return strings.TrimSpace(defaultArchiveDir), nil
	}

	folderChain := make([]model.Folder, 0, 4)
	visited := make(map[int64]struct{})
	currentID := *feed.FolderID

	for currentID != 0 {
		if _, exists := visited[currentID]; exists {
			break
		}
		visited[currentID] = struct{}{}

		folder, err := s.folderRepo.GetByID(ctx, currentID)
		if err != nil {
			logger.Warn("ai analysis archive load folder failed", "module", "service", "action", "fetch", "resource", "ai_archive", "result", "failed", "folder_id", currentID, "error", err)
			break
		}

		folderChain = append(folderChain, folder)
		if folder.ParentID == nil {
			break
		}
		currentID = *folder.ParentID
	}

	for idx, folder := range folderChain {
		if strings.TrimSpace(folder.AnalysisArchiveDir) == "" {
			continue
		}
		return strings.TrimSpace(folder.AnalysisArchiveDir), buildArchiveFolderSegments(folderChain[:idx])
	}

	return strings.TrimSpace(defaultArchiveDir), buildArchiveFolderSegments(folderChain)
}

func buildArchiveFolderSegments(folderChain []model.Folder) []string {
	segments := make([]string, 0, len(folderChain))
	for i := len(folderChain) - 1; i >= 0; i-- {
		folder := folderChain[i]
		name := strings.TrimSpace(folder.Name)
		if name == "" {
			name = fmt.Sprintf("folder-%d", folder.ID)
		}
		segments = append(segments, name)
	}
	return segments
}

func normalizeArchiveRoot(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func dailyArchiveFilePath(root string, day time.Time) string {
	return filepath.Join(root, day.In(time.Local).Format("20060102")+".md")
}

func (s *aiService) collectDailyArchiveItemsByRoot(ctx context.Context, day time.Time) (map[string][]dailyArchiveItem, error) {
	day = day.In(time.Local)
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)

	items, err := s.analysisRepo.ListByCreatedRange(ctx, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}

	feedCache := make(map[int64]model.Feed)
	entryCache := make(map[int64]model.Entry)
	grouped := make(map[string]map[int64]dailyArchiveItem)
	for _, item := range items {
		root, folderSegments, feedTitle, err := s.resolveStoredAnalysisArchiveLocation(ctx, item, feedCache)
		if err != nil {
			logger.Warn("ai archive resolve stored location failed", "module", "service", "action", "fetch", "resource", "ai_archive", "result", "failed", "entry_id", item.EntryID, "feed_id", item.FeedID, "error", err)
			continue
		}
		if root == "" {
			continue
		}

		entryTitle := fmt.Sprintf("Entry %d", item.EntryID)
		if item.EntryTitle != nil && strings.TrimSpace(*item.EntryTitle) != "" {
			entryTitle = strings.TrimSpace(*item.EntryTitle)
		}
		feedTitle = strings.TrimSpace(feedTitle)
		if feedTitle == "" {
			feedTitle = "Feed"
		}

		var originalTitle *string
		if s.entryRepo != nil {
			entry, ok := entryCache[item.EntryID]
			if !ok {
				entry, err = s.entryRepo.GetByID(ctx, item.EntryID)
				if err != nil {
					logger.Warn("ai archive load original title failed", "module", "service", "action", "fetch", "resource", "ai_archive", "result", "failed", "entry_id", item.EntryID, "error", err)
				} else {
					entryCache[item.EntryID] = entry
				}
			}
			if ok || entry.ID != 0 {
				originalTitle = entry.Title
			}
		}

		candidate := dailyArchiveItem{
			Root:           root,
			FolderSegments: folderSegments,
			FeedTitle:      feedTitle,
			EntryTitle:     entryTitle,
			OriginalTitle:  originalTitle,
			Analysis:       item,
		}

		if _, ok := grouped[root]; !ok {
			grouped[root] = make(map[int64]dailyArchiveItem)
		}

		current, ok := grouped[root][item.EntryID]
		if !ok || shouldReplaceDailyArchiveItem(current, candidate) {
			grouped[root][item.EntryID] = candidate
		}
	}

	result := make(map[string][]dailyArchiveItem, len(grouped))
	for root, deduped := range grouped {
		items := make([]dailyArchiveItem, 0, len(deduped))
		for _, item := range deduped {
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool {
			left := items[i]
			right := items[j]
			if cmp := compareStringSlices(left.FolderSegments, right.FolderSegments); cmp != 0 {
				return cmp < 0
			}
			if left.FeedTitle != right.FeedTitle {
				return left.FeedTitle < right.FeedTitle
			}
			leftPublished := time.Time{}
			if left.Analysis.PublishedAt != nil {
				leftPublished = left.Analysis.PublishedAt.In(time.Local)
			}
			rightPublished := time.Time{}
			if right.Analysis.PublishedAt != nil {
				rightPublished = right.Analysis.PublishedAt.In(time.Local)
			}
			if !leftPublished.Equal(rightPublished) {
				return leftPublished.After(rightPublished)
			}
			if !left.Analysis.CreatedAt.Equal(right.Analysis.CreatedAt) {
				return left.Analysis.CreatedAt.After(right.Analysis.CreatedAt)
			}
			if left.EntryTitle != right.EntryTitle {
				return left.EntryTitle < right.EntryTitle
			}
			return left.Analysis.EntryID < right.Analysis.EntryID
		})
		result[root] = items
	}

	return result, nil
}

func shouldReplaceDailyArchiveItem(current, candidate dailyArchiveItem) bool {
	if candidate.Analysis.CreatedAt.After(current.Analysis.CreatedAt) {
		return true
	}
	if candidate.Analysis.CreatedAt.Before(current.Analysis.CreatedAt) {
		return false
	}
	if candidate.Analysis.IsReadability != current.Analysis.IsReadability {
		return candidate.Analysis.IsReadability
	}
	return candidate.Analysis.ID > current.Analysis.ID
}

func compareStringSlices(left, right []string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if left[i] == right[i] {
			continue
		}
		if left[i] < right[i] {
			return -1
		}
		return 1
	}
	switch {
	case len(left) < len(right):
		return -1
	case len(left) > len(right):
		return 1
	default:
		return 0
	}
}

func (s *aiService) resolveStoredAnalysisArchiveLocation(ctx context.Context, item model.StoredAIAnalysis, feedCache map[int64]model.Feed) (string, []string, string, error) {
	feed, ok := feedCache[item.FeedID]
	if !ok {
		var err error
		feed, err = s.feedRepo.GetByID(ctx, item.FeedID)
		if err != nil {
			return "", nil, "", err
		}
		feedCache[item.FeedID] = feed
	}

	root, folderSegments := s.resolveAnalysisArchiveLocation(ctx, feed)
	root = normalizeArchiveRoot(root)
	return root, folderSegments, feed.Title, nil
}

func (s *aiService) writeDailyArchiveFile(day time.Time, root string, items []dailyArchiveItem, reportBlock string) error {
	root = normalizeArchiveRoot(root)
	if root == "" {
		return nil
	}
	if len(items) == 0 && strings.TrimSpace(reportBlock) == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	content := buildDailyArchiveDocument(day, items, reportBlock)
	return os.WriteFile(dailyArchiveFilePath(root, day), []byte(content), 0o644)
}

func buildDailyArchiveDocument(day time.Time, items []dailyArchiveItem, reportBlock string) string {
	day = day.In(time.Local)

	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(day.Format("2006-01-02"))
	builder.WriteString(" AI 分析归档\n\n")

	reportBlock = strings.TrimSpace(reportBlock)
	if reportBlock != "" {
		builder.WriteString(reportBlock)
		builder.WriteString("\n\n")
	}

	builder.WriteString("## 归档条目\n\n")
	if len(items) == 0 {
		builder.WriteString("暂无分析条目。\n")
		return builder.String()
	}

	lastFolderSegments := []string{}
	lastFeedTitle := ""
	for _, item := range items {
		commonDepth := 0
		for commonDepth < len(lastFolderSegments) && commonDepth < len(item.FolderSegments) && lastFolderSegments[commonDepth] == item.FolderSegments[commonDepth] {
			commonDepth++
		}

		for idx := commonDepth; idx < len(item.FolderSegments); idx++ {
			builder.WriteString(headingLine(3+idx, item.FolderSegments[idx]))
			builder.WriteString("\n\n")
		}

		if commonDepth != len(lastFolderSegments) || lastFeedTitle != item.FeedTitle {
			builder.WriteString(headingLine(3+len(item.FolderSegments), item.FeedTitle))
			builder.WriteString("\n\n")
		}

		builder.WriteString(headingLine(4+len(item.FolderSegments), item.EntryTitle))
		builder.WriteString("\n\n")
		builder.WriteString(buildDailyArchiveEntryMarkdown(item))
		builder.WriteString("\n\n")

		lastFolderSegments = append(lastFolderSegments[:0], item.FolderSegments...)
		lastFolderSegments = append([]string(nil), lastFolderSegments...)
		lastFeedTitle = item.FeedTitle
	}

	return strings.TrimRight(builder.String(), "\n") + "\n"
}

func headingLine(level int, title string) string {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	return strings.Repeat("#", level) + " " + strings.TrimSpace(title)
}

func buildDailyArchiveEntryMarkdown(item dailyArchiveItem) string {
	analysis := item.Analysis

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("- Entry ID: %d\n", analysis.EntryID))
	builder.WriteString(fmt.Sprintf("- Feed: %s\n", strings.TrimSpace(item.FeedTitle)))
	if item.OriginalTitle != nil && strings.TrimSpace(*item.OriginalTitle) != "" && strings.TrimSpace(*item.OriginalTitle) != strings.TrimSpace(item.EntryTitle) {
		builder.WriteString(fmt.Sprintf("- Original Title: %s\n", strings.TrimSpace(*item.OriginalTitle)))
	}
	if analysis.EntryURL != nil && strings.TrimSpace(*analysis.EntryURL) != "" {
		builder.WriteString(fmt.Sprintf("- URL: %s\n", strings.TrimSpace(*analysis.EntryURL)))
	}
	if analysis.Author != nil && strings.TrimSpace(*analysis.Author) != "" {
		builder.WriteString(fmt.Sprintf("- Author: %s\n", strings.TrimSpace(*analysis.Author)))
	}
	if analysis.PublishedAt != nil {
		builder.WriteString(fmt.Sprintf("- Published At: %s\n", analysis.PublishedAt.In(time.Local).Format(time.RFC3339)))
	}
	builder.WriteString(fmt.Sprintf("- AI Language: %s\n", strings.TrimSpace(analysis.Language)))
	builder.WriteString(fmt.Sprintf("- Analyzed At: %s\n", analysis.CreatedAt.In(time.Local).Format(time.RFC3339)))
	builder.WriteString(fmt.Sprintf("- Readability: %t\n\n", analysis.IsReadability))

	builder.WriteString("**摘要**\n\n")
	builder.WriteString(strings.TrimSpace(analysis.Summary))
	builder.WriteString("\n\n")

	builder.WriteString("**标签**\n\n")
	builder.WriteString(strings.TrimSpace(analysis.Tag))
	builder.WriteString("\n\n")

	builder.WriteString("**情绪与重要性**\n\n")
	builder.WriteString(fmt.Sprintf("- Sentiment: %s\n", strings.TrimSpace(analysis.Sentiment)))
	builder.WriteString(fmt.Sprintf("- Importance: %d/10\n\n", analysis.Importance))

	builder.WriteString("**实体**\n\n")
	builder.WriteString(formatDailyArchiveList(analysis.Entities, "- None"))
	builder.WriteString("\n")

	if analysis.Latitude != nil && analysis.Longitude != nil {
		builder.WriteString("\n**坐标**\n\n")
		builder.WriteString(fmt.Sprintf("- Latitude: %.6f\n", *analysis.Latitude))
		builder.WriteString(fmt.Sprintf("- Longitude: %.6f\n", *analysis.Longitude))
	}

	return strings.TrimRight(builder.String(), "\n")
}

func formatDailyArchiveList(items []string, emptyFallback string) string {
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		return emptyFallback
	}

	var builder strings.Builder
	for _, item := range filtered {
		builder.WriteString("- ")
		builder.WriteString(item)
		builder.WriteString("\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}

func sanitizeArchivePathSegment(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}

	replacer := strings.NewReplacer(
		"/", "／",
		"\\", "＼",
		":", "：",
		"*", "＊",
		"?", "？",
		"\"", "＂",
		"<", "＜",
		">", "＞",
		"|", "｜",
	)
	value = replacer.Replace(value)

	var builder strings.Builder
	for _, r := range value {
		if r < 32 {
			continue
		}
		builder.WriteRune(r)
	}

	cleaned := strings.TrimSpace(builder.String())
	cleaned = strings.Trim(cleaned, ".")
	if cleaned == "" {
		return fallback
	}
	return cleaned
}

func (s *aiService) ensureCachedAnalysisArchive(ctx context.Context, entryID int64, analysis *model.AIAnalysis) {
	if analysis == nil {
		return
	}
	s.archiveAnalysisMarkdown(ctx, entryID, *analysis)
}

func (s *aiService) resolveStoredAnalysisTitle(ctx context.Context, entryID int64) string {
	if entryID == 0 {
		return ""
	}
	if s.listTranslationRepo != nil {
		cached, err := s.listTranslationRepo.Get(ctx, entryID, storedAnalysisTitleLanguage)
		if err == nil && cached != nil && strings.TrimSpace(cached.Title) != "" {
			return strings.TrimSpace(cached.Title)
		}
	}
	if s.entryRepo == nil {
		return ""
	}

	entry, err := s.entryRepo.GetByID(ctx, entryID)
	if err != nil || entry.Title == nil {
		return ""
	}
	return s.ensureStoredAnalysisTitleTranslation(ctx, entryID, strings.TrimSpace(*entry.Title))
}

func extractDailyArchiveReportBlockFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return extractDailyArchiveReportBlock(string(data))
}

func extractDailyArchiveReportBlock(content string) string {
	start := strings.Index(content, dailyArchiveReportStartMarker)
	if start < 0 {
		return ""
	}
	end := strings.Index(content, dailyArchiveReportEndMarker)
	if end < 0 || end < start {
		return ""
	}
	end += len(dailyArchiveReportEndMarker)
	return strings.TrimSpace(content[start:end])
}

func buildDailyArchiveReportBlock(report *model.AIDailyReport) string {
	if report == nil {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(dailyArchiveReportStartMarker)
	builder.WriteString("\n## AI 日报\n\n")
	builder.WriteString(fmt.Sprintf("- 日期: %s\n", strings.TrimSpace(report.Date)))
	builder.WriteString(fmt.Sprintf("- 分析总数: %d\n", report.Total))
	builder.WriteString(fmt.Sprintf("- 重点关注数: %d\n", report.FocusedTotal))
	builder.WriteString(fmt.Sprintf("- 情绪分布: Positive %d / Neutral %d / Negative %d / Other %d\n\n", report.Sentiment.Positive, report.Sentiment.Neutral, report.Sentiment.Negative, report.Sentiment.Other))

	if strings.TrimSpace(report.Overview) != "" {
		builder.WriteString("### 今日热点综述\n\n")
		builder.WriteString(strings.TrimSpace(report.Overview))
		builder.WriteString("\n\n")
	}
	if strings.TrimSpace(report.RiskReview) != "" {
		builder.WriteString("### 风险点评\n\n")
		builder.WriteString(strings.TrimSpace(report.RiskReview))
		builder.WriteString("\n\n")
	}
	if strings.TrimSpace(report.TrendOutlook) != "" {
		builder.WriteString("### 趋势判断\n\n")
		builder.WriteString(strings.TrimSpace(report.TrendOutlook))
		builder.WriteString("\n\n")
	}
	if len(report.TopTags) > 0 {
		builder.WriteString("### 高频标签\n\n")
		for _, item := range report.TopTags {
			builder.WriteString(fmt.Sprintf("- %s (%d)\n", strings.TrimSpace(item.Name), item.Count))
		}
		builder.WriteString("\n")
	}
	if len(report.FocusedTags) > 0 {
		builder.WriteString("### 重点关注标签\n\n")
		for _, item := range report.FocusedTags {
			builder.WriteString(fmt.Sprintf("- %s (%d)\n", strings.TrimSpace(item.Name), item.Count))
		}
		builder.WriteString("\n")
	}
	if len(report.TopEntities) > 0 {
		builder.WriteString("### 高频实体\n\n")
		for _, item := range report.TopEntities {
			builder.WriteString(fmt.Sprintf("- %s (%d)\n", strings.TrimSpace(item.Name), item.Count))
		}
		builder.WriteString("\n")
	}
	if len(report.TopFeeds) > 0 {
		builder.WriteString("### 重点来源\n\n")
		for _, item := range report.TopFeeds {
			builder.WriteString(fmt.Sprintf("- %s (%d)\n", strings.TrimSpace(item.FeedTitle), item.Count))
		}
		builder.WriteString("\n")
	}

	builder.WriteString(dailyArchiveReportEndMarker)
	return strings.TrimRight(builder.String(), "\n")
}

func (s *aiService) BuildDailyAnalysisReport(ctx context.Context, day time.Time) (*model.AIDailyReport, error) {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	items, err := s.analysisRepo.ListByCreatedRange(ctx, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}
	items, err = s.attachFocusTags(ctx, items)
	if err != nil {
		return nil, err
	}

	focusedItems := filterFocusedAnalyses(items)

	report := &model.AIDailyReport{
		Date:         dayStart.Format("2006-01-02"),
		Total:        len(items),
		FocusedTotal: len(focusedItems),
		Sentiment:    model.AIDailyReportSentiment{},
		TopAnalyses:  topStoredAnalyses(items, 10),
		TopTags:      buildTopCountItems(items, 10, extractTagSegments),
		TopEntities:  buildTopCountItems(items, 10, extractEntities),
		TopFeeds:     buildTopFeedMetrics(items, 10),
		FocusedTags:  buildTopCountItems(focusedItems, 10, extractFocusTags),
		FocusedItems: topStoredAnalyses(focusedItems, 10),
	}

	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Sentiment)) {
		case "positive":
			report.Sentiment.Positive++
		case "negative":
			report.Sentiment.Negative++
		case "neutral":
			report.Sentiment.Neutral++
		default:
			report.Sentiment.Other++
		}
	}

	if report.Total > 0 {
		input, err := buildDailyReportNarrativeInput(report)
		if err != nil {
			logger.Warn("ai daily report narrative input failed", "module", "service", "action", "fetch", "resource", "ai_report", "result", "failed", "date", report.Date, "error", err)
		} else {
			signature := hashutil.SHA256Hex(input)
			narrative, cacheErr := s.getCachedDailyReportNarrative(ctx, report.Date, signature)
			if cacheErr != nil {
				logger.Warn("ai daily report narrative cache lookup failed", "module", "service", "action", "fetch", "resource", "ai_report_cache", "result", "failed", "date", report.Date, "error", cacheErr)
			}
			if narrative == nil {
				narrative, err = s.generateDailyReportNarrativeWithInput(ctx, report, input)
				if err != nil {
					logger.Warn("ai daily report narrative failed", "module", "service", "action", "fetch", "resource", "ai_report", "result", "failed", "date", report.Date, "error", err)
				} else if saveErr := s.saveDailyReportNarrativeCache(ctx, report.Date, signature, narrative); saveErr != nil {
					logger.Warn("ai daily report narrative cache save failed", "module", "service", "action", "save", "resource", "ai_report_cache", "result", "failed", "date", report.Date, "error", saveErr)
				}
			}
			if narrative != nil {
				report.Overview = narrative.Overview
				report.RiskReview = narrative.RiskReview
				report.TrendOutlook = narrative.TrendOutlook
			}
		}
	}

	return report, nil
}

func (s *aiService) generateDailyReportNarrative(ctx context.Context, report *model.AIDailyReport) (*DailyReportNarrativeResult, error) {
	if report == nil || report.Total == 0 {
		return nil, nil
	}

	input, err := buildDailyReportNarrativeInput(report)
	if err != nil {
		return nil, err
	}
	return s.generateDailyReportNarrativeWithInput(ctx, report, input)
}

func (s *aiService) generateDailyReportNarrativeWithInput(ctx context.Context, report *model.AIDailyReport, input string) (*DailyReportNarrativeResult, error) {
	if report == nil || report.Total == 0 {
		return nil, nil
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("daily report narrative input is empty")
	}

	cfg, err := s.getAIConfig(ctx, aiSceneReport)
	if err != nil {
		return nil, err
	}

	provider, err := ai.NewProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("create provider: %w", err)
	}

	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
	}

	language := s.GetSummaryLanguage(ctx)
	systemPrompt := s.prompts().DailyReportPrompt(report.Date, language)
	wrappedInput := ai.WrapInput(input)
	raw, err := provider.Complete(ctx, systemPrompt, wrappedInput)
	if err != nil {
		return nil, err
	}
	s.recordAIUsageEstimate(aiSceneReport, systemPrompt, wrappedInput, raw)

	narrative, err := parseDailyReportNarrative(raw)
	if err != nil {
		return nil, err
	}

	return &narrative, nil
}

func (s *aiService) getCachedDailyReportNarrative(ctx context.Context, date, signature string) (*DailyReportNarrativeResult, error) {
	if s.settingsRepo == nil {
		return nil, nil
	}
	date = strings.TrimSpace(date)
	signature = strings.TrimSpace(signature)
	if date == "" || signature == "" {
		return nil, nil
	}

	setting, err := s.settingsRepo.Get(ctx, dailyReportNarrativeCacheKey(date))
	if err != nil || setting == nil || strings.TrimSpace(setting.Value) == "" {
		return nil, err
	}

	var payload dailyReportNarrativeCachePayload
	if err := json.Unmarshal([]byte(setting.Value), &payload); err != nil {
		return nil, fmt.Errorf("decode daily report narrative cache: %w", err)
	}
	if strings.TrimSpace(payload.Signature) != signature {
		return nil, nil
	}

	return &DailyReportNarrativeResult{
		Overview:     payload.Overview,
		RiskReview:   payload.RiskReview,
		TrendOutlook: payload.TrendOutlook,
	}, nil
}

func (s *aiService) saveDailyReportNarrativeCache(ctx context.Context, date, signature string, narrative *DailyReportNarrativeResult) error {
	if s.settingsRepo == nil || narrative == nil {
		return nil
	}
	date = strings.TrimSpace(date)
	signature = strings.TrimSpace(signature)
	if date == "" || signature == "" {
		return nil
	}

	payload := dailyReportNarrativeCachePayload{
		Signature:    signature,
		Overview:     narrative.Overview,
		RiskReview:   narrative.RiskReview,
		TrendOutlook: narrative.TrendOutlook,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode daily report narrative cache: %w", err)
	}
	return s.settingsRepo.Set(ctx, dailyReportNarrativeCacheKey(date), string(data))
}

func dailyReportNarrativeCacheKey(date string) string {
	return dailyReportNarrativeCacheKeyPrefix + strings.TrimSpace(date)
}

func topStoredAnalyses(items []model.StoredAIAnalysis, limit int) []model.StoredAIAnalysis {
	if len(items) == 0 || limit <= 0 {
		return []model.StoredAIAnalysis{}
	}

	cloned := append([]model.StoredAIAnalysis(nil), items...)
	sort.SliceStable(cloned, func(i, j int) bool {
		if cloned[i].Importance != cloned[j].Importance {
			return cloned[i].Importance > cloned[j].Importance
		}
		if !cloned[i].CreatedAt.Equal(cloned[j].CreatedAt) {
			return cloned[i].CreatedAt.After(cloned[j].CreatedAt)
		}
		return cloned[i].ID > cloned[j].ID
	})

	if len(cloned) > limit {
		cloned = cloned[:limit]
	}
	return cloned
}

func (s *aiService) attachFocusTags(ctx context.Context, items []model.StoredAIAnalysis) ([]model.StoredAIAnalysis, error) {
	if len(items) == 0 || s.focusTagRepo == nil {
		return items, nil
	}

	entryIDs := make([]int64, 0, len(items))
	for _, item := range items {
		if item.EntryID != 0 {
			entryIDs = append(entryIDs, item.EntryID)
		}
	}

	tagMap, err := s.focusTagRepo.ListByEntryIDs(ctx, entryIDs)
	if err != nil {
		return nil, err
	}

	for index := range items {
		if tags, ok := tagMap[items[index].EntryID]; ok {
			items[index].FocusTags = append([]string(nil), tags...)
		} else if items[index].FocusTags == nil {
			items[index].FocusTags = []string{}
		}
	}

	return items, nil
}

func filterFocusedAnalyses(items []model.StoredAIAnalysis) []model.StoredAIAnalysis {
	if len(items) == 0 {
		return []model.StoredAIAnalysis{}
	}

	result := make([]model.StoredAIAnalysis, 0, len(items))
	for _, item := range items {
		if item.Focused {
			result = append(result, item)
		}
	}
	return result
}

func buildTopCountItems(
	items []model.StoredAIAnalysis,
	limit int,
	extractor func(model.StoredAIAnalysis) []string,
) []model.AIDailyReportCountItem {
	if len(items) == 0 || limit <= 0 {
		return []model.AIDailyReportCountItem{}
	}

	counts := make(map[string]int)
	for _, item := range items {
		for _, value := range extractor(item) {
			counts[value]++
		}
	}

	return sortCountItems(counts, limit)
}

func sortCountItems(counts map[string]int, limit int) []model.AIDailyReportCountItem {
	result := make([]model.AIDailyReportCountItem, 0, len(counts))
	for name, count := range counts {
		if name == "" || count <= 0 {
			continue
		}
		result = append(result, model.AIDailyReportCountItem{
			Name:  name,
			Count: count,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Name < result[j].Name
	})

	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func buildTopFeedMetrics(items []model.StoredAIAnalysis, limit int) []model.AIDailyReportFeedMetric {
	if len(items) == 0 || limit <= 0 {
		return []model.AIDailyReportFeedMetric{}
	}

	type feedMetric struct {
		id    int64
		title string
		count int
	}

	counts := make(map[int64]*feedMetric)
	for _, item := range items {
		metric, ok := counts[item.FeedID]
		if !ok {
			metric = &feedMetric{id: item.FeedID, title: item.FeedTitle}
			counts[item.FeedID] = metric
		}
		metric.count++
		if metric.title == "" {
			metric.title = item.FeedTitle
		}
	}

	result := make([]model.AIDailyReportFeedMetric, 0, len(counts))
	for _, metric := range counts {
		result = append(result, model.AIDailyReportFeedMetric{
			FeedID:    metric.id,
			FeedTitle: metric.title,
			Count:     metric.count,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		if result[i].FeedTitle != result[j].FeedTitle {
			return result[i].FeedTitle < result[j].FeedTitle
		}
		return result[i].FeedID < result[j].FeedID
	})

	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func extractTagSegments(item model.StoredAIAnalysis) []string {
	if item.Tag == "" {
		return nil
	}

	seen := make(map[string]struct{})
	result := make([]string, 0, 4)
	for _, part := range strings.Split(item.Tag, "/") {
		tag := strings.TrimSpace(strings.TrimPrefix(part, "#"))
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}

func extractEntities(item model.StoredAIAnalysis) []string {
	if len(item.Entities) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(item.Entities))
	result := make([]string, 0, len(item.Entities))
	for _, entity := range item.Entities {
		entity = strings.TrimSpace(entity)
		if entity == "" {
			continue
		}
		if _, exists := seen[entity]; exists {
			continue
		}
		seen[entity] = struct{}{}
		result = append(result, entity)
	}
	return result
}

func extractFocusTags(item model.StoredAIAnalysis) []string {
	if len(item.FocusTags) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(item.FocusTags))
	result := make([]string, 0, len(item.FocusTags))
	for _, tag := range item.FocusTags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}

// TranslateBlocks parses HTML into blocks and translates them in parallel.
// Returns block info, a channel of results, an error channel, and any initial error.
func (s *aiService) TranslateBlocks(ctx context.Context, entryID int64, content, title string, isReadability bool) ([]TranslateBlockInfo, <-chan TranslateBlockResult, <-chan error, error) {
	// Parse HTML into blocks
	blocks, err := ai.ParseHTMLBlocks(content)
	if err != nil {
		logger.Warn("ai translate parse blocks failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, nil, nil, fmt.Errorf("parse HTML blocks: %w", err)
	}

	if len(blocks) == 0 {
		logger.Warn("ai translate no blocks", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID)
		return nil, nil, nil, fmt.Errorf("no blocks to translate")
	}

	// Build block info for caller
	blockInfos := make([]TranslateBlockInfo, len(blocks))
	for i, b := range blocks {
		blockInfos[i] = TranslateBlockInfo{
			Index:         b.Index,
			HTML:          b.HTML,
			NeedTranslate: b.NeedTranslate,
		}
	}

	// Get AI configuration
	cfg, err := s.getAIConfig(ctx, aiSceneTranslation)
	if err != nil {
		logger.Warn("ai translate get config failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, nil, nil, err
	}

	// Get language setting
	language := s.GetSummaryLanguage(ctx)

	// Create channels
	resultCh := make(chan TranslateBlockResult)
	errCh := make(chan error, len(blocks))

	// Start parallel translation
	go func() {
		defer close(resultCh)
		defer close(errCh)

		var wg sync.WaitGroup
		sem := make(chan struct{}, 3) // Limit to 3 concurrent translations

		// Collect results for caching
		var results []TranslateBlockResult
		var resultsMu sync.Mutex
		var hasError atomic.Bool

	blockLoop:
		for _, block := range blocks {
			// Check if context is cancelled before processing each block
			if ctx.Err() != nil {
				break
			}

			if !block.NeedTranslate {
				// No translation needed, add to results for caching
				resultsMu.Lock()
				results = append(results, TranslateBlockResult{
					Index: block.Index,
					HTML:  block.HTML,
				})
				resultsMu.Unlock()
				// Don't send via channel - frontend already has original content
				continue
			}

			wg.Add(1)

			// Acquire semaphore with context cancellation support
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Done()
				break blockLoop
			}

			go func(b ai.Block) {
				defer wg.Done()
				defer func() { <-sem }() // Release semaphore

				// Wait for rate limiter
				if err := s.rateLimiter.Wait(ctx); err != nil {
					select {
					case errCh <- fmt.Errorf("rate limit: %w", err):
						hasError.Store(true)
					default:
					}
					return
				}

				// Create provider for this goroutine
				provider, err := ai.NewProvider(cfg)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("create provider: %w", err):
						hasError.Store(true)
					default:
					}
					return
				}

				// Replace media elements with placeholders to prevent AI from modifying them
				htmlForTranslation, mediaElements := ai.ReplaceMediaWithPlaceholders(b.HTML)

				// Wrap input with <input> tags
				wrappedInput := ai.WrapInput(htmlForTranslation)

				// Translate single block using non-streaming Complete
				systemPrompt := s.prompts().TranslateBlockPrompt(title, language)
				translatedHTML, err := provider.Complete(ctx, systemPrompt, wrappedInput)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("translate block %d: %w", b.Index, err):
						hasError.Store(true)
					default:
					}
					return
				}

				// Restore media elements from placeholders
				translatedHTML = ai.RestoreMediaFromPlaceholders(translatedHTML, mediaElements)
				s.recordAIUsageEstimate(aiSceneTranslation, systemPrompt, wrappedInput, translatedHTML)

				// Send result
				result := TranslateBlockResult{
					Index: b.Index,
					HTML:  translatedHTML,
				}
				resultsMu.Lock()
				results = append(results, result)
				resultsMu.Unlock()

				select {
				case resultCh <- result:
				case <-ctx.Done():
					return
				}
			}(block)
		}

		wg.Wait()

		// Cache complete result if no errors and not cancelled
		if !hasError.Load() && len(results) > 0 && ctx.Err() == nil {
			// Sort by index
			sort.Slice(results, func(i, j int) bool {
				return results[i].Index < results[j].Index
			})

			// Concatenate all blocks
			var fullHTML strings.Builder
			for _, r := range results {
				fullHTML.WriteString(r.HTML)
			}

			// Save to cache
			if err := s.SaveTranslation(ctx, entryID, isReadability, fullHTML.String()); err != nil {
				logger.Warn("ai translate cache save failed", "module", "service", "action", "save", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
			}

		}

	}()

	return blockInfos, resultCh, errCh, nil
}

// TranslateBatch translates multiple articles' titles and summaries concurrently.
// It first checks cache and only translates articles that don't have cached results.
func (s *aiService) TranslateBatch(ctx context.Context, articles []BatchArticleInput) (<-chan BatchTranslateResult, <-chan error, error) {
	if len(articles) == 0 {
		logger.Warn("ai batch translate empty input", "module", "service", "action", "fetch", "resource", "ai", "result", "failed")
		return nil, nil, fmt.Errorf("no articles to translate")
	}

	// Get language setting
	language := s.GetSummaryLanguage(ctx)

	// Collect entry IDs for batch cache lookup
	entryIDs := make([]int64, 0, len(articles))
	articleMap := make(map[int64]BatchArticleInput)
	for _, a := range articles {
		entryID, err := parseEntryID(a.ID)
		if err != nil {
			continue
		}
		entryIDs = append(entryIDs, entryID)
		articleMap[entryID] = a
	}

	// Batch fetch cached translations
	cachedMap, err := s.listTranslationRepo.GetBatch(ctx, entryIDs, language)
	if err != nil {
		logger.Warn("ai batch translate cache lookup failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "error", err)
		// Log error but continue without cache
		cachedMap = make(map[int64]*model.AIListTranslation)
	}

	// Get AI configuration (only needed if there are uncached articles)
	var cfg ai.Config
	needsTranslation := false
	for _, entryID := range entryIDs {
		if _, ok := cachedMap[entryID]; !ok {
			needsTranslation = true
			break
		}
	}

	if needsTranslation {
		cfg, err = s.getAIConfig(ctx, aiSceneTranslation)
		if err != nil {
			logger.Warn("ai batch translate get config failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "error", err)
			return nil, nil, err
		}
	}

	// Create channels
	resultCh := make(chan BatchTranslateResult)
	errCh := make(chan error, len(articles))

	go func() {
		defer close(resultCh)
		defer close(errCh)

		var wg sync.WaitGroup
		sem := make(chan struct{}, 5) // Limit to 5 concurrent translations

	articleLoop:
		for _, entryID := range entryIDs {
			if ctx.Err() != nil {
				break
			}

			article := articleMap[entryID]

			// Check cache first
			if cached, ok := cachedMap[entryID]; ok {
				result := BatchTranslateResult{
					ID:      article.ID,
					Title:   &cached.Title,
					Summary: &cached.Summary,
					Cached:  true,
				}
				select {
				case resultCh <- result:
				case <-ctx.Done():
					break articleLoop
				}
				logger.Debug("ai batch translate cache hit", "module", "service", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID, "cache", "hit")
				continue
			}

			wg.Add(1)

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Done()
				break articleLoop
			}

			go func(a BatchArticleInput, eID int64) {
				defer wg.Done()
				defer func() { <-sem }()

				// Create provider for this goroutine
				provider, err := ai.NewProvider(cfg)
				if err != nil {
					logger.Warn("ai batch translate provider create failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "provider", cfg.Provider, "model", cfg.Model, "error", err)
					select {
					case errCh <- fmt.Errorf("create provider: %w", err):
					default:
					}
					return
				}

				// Translate title
				var translatedTitle *string
				titleStr := ""
				if a.Title != "" {
					// Wait for rate limiter
					if err := s.rateLimiter.Wait(ctx); err != nil {
						logger.Warn("ai batch translate rate limit", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", eID, "error", err)
						select {
						case errCh <- fmt.Errorf("rate limit: %w", err):
						default:
						}
						return
					}
					titlePrompt := s.prompts().TranslateTextPrompt("title", language)
					wrappedTitle := ai.WrapInput(a.Title)
					translated, err := provider.Complete(ctx, titlePrompt, wrappedTitle)
					if err != nil {
						select {
						case errCh <- fmt.Errorf("translate title for %s: %w", a.ID, err):
						default:
						}
						return
					}
					s.recordAIUsageEstimate(aiSceneTranslation, titlePrompt, wrappedTitle, translated)
					translatedTitle = &translated
					titleStr = translated
				}

				// Translate summary
				var translatedSummary *string
				summaryStr := ""
				if a.Summary != "" {
					// Wait for rate limiter
					if err := s.rateLimiter.Wait(ctx); err != nil {
						logger.Warn("ai batch translate rate limit", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", eID, "error", err)
						select {
						case errCh <- fmt.Errorf("rate limit: %w", err):
						default:
						}
						return
					}
					summaryPrompt := s.prompts().TranslateTextPrompt("summary", language)
					wrappedSummary := ai.WrapInput(a.Summary)
					translated, err := provider.Complete(ctx, summaryPrompt, wrappedSummary)
					if err != nil {
						select {
						case errCh <- fmt.Errorf("translate summary for %s: %w", a.ID, err):
						default:
						}
						return
					}
					s.recordAIUsageEstimate(aiSceneTranslation, summaryPrompt, wrappedSummary, translated)
					translatedSummary = &translated
					summaryStr = translated
				}

				// Save to cache
				if titleStr != "" || summaryStr != "" {
					if err := s.listTranslationRepo.Save(ctx, eID, language, titleStr, summaryStr); err != nil {
						logger.Warn("ai batch translate cache save failed", "module", "service", "action", "save", "resource", "ai", "result", "failed", "entry_id", eID, "error", err)
					}
				}

				// Send result
				result := BatchTranslateResult{
					ID:      a.ID,
					Title:   translatedTitle,
					Summary: translatedSummary,
				}

				select {
				case resultCh <- result:
				case <-ctx.Done():
				}
			}(article, entryID)
		}

		wg.Wait()
	}()

	return resultCh, errCh, nil
}

func parseEntryID(id string) (int64, error) {
	var entryID int64
	_, err := fmt.Sscanf(id, "%d", &entryID)
	return entryID, err
}

func (s *aiService) ClearAllCache(ctx context.Context) (summaries, translations, listTranslations, analyses int64, err error) {
	summaries, err = s.summaryRepo.DeleteAll(ctx)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("clear summaries: %w", err)
	}

	translations, err = s.translationRepo.DeleteAll(ctx)
	if err != nil {
		return summaries, 0, 0, 0, fmt.Errorf("clear translations: %w", err)
	}

	listTranslations, err = s.listTranslationRepo.DeleteAll(ctx)
	if err != nil {
		return summaries, translations, 0, 0, fmt.Errorf("clear list translations: %w", err)
	}

	analyses, err = s.analysisRepo.DeleteAll(ctx)
	if err != nil {
		return summaries, translations, listTranslations, 0, fmt.Errorf("clear analyses: %w", err)
	}

	logger.Info("ai cache cleared", "module", "service", "action", "clear", "resource", "ai", "result", "ok", "summaries", summaries, "translations", translations, "list_translations", listTranslations, "analyses", analyses)
	return summaries, translations, listTranslations, analyses, nil
}

func buildDailyReportNarrativeInput(report *model.AIDailyReport) (string, error) {
	if report == nil {
		return "", nil
	}

	type narrativeAnalysisItem struct {
		FeedTitle   string   `json:"feedTitle"`
		Title       string   `json:"title"`
		Tag         string   `json:"tag"`
		FocusTags   []string `json:"focusTags,omitempty"`
		Summary     string   `json:"summary"`
		Entities    []string `json:"entities"`
		Sentiment   string   `json:"sentiment"`
		Importance  int      `json:"importance"`
		PublishedAt string   `json:"publishedAt,omitempty"`
	}

	analyses := make([]narrativeAnalysisItem, 0, len(report.TopAnalyses))
	for _, item := range report.TopAnalyses {
		narrativeItem := narrativeAnalysisItem{
			FeedTitle:  item.FeedTitle,
			Tag:        item.Tag,
			FocusTags:  append([]string(nil), item.FocusTags...),
			Summary:    item.Summary,
			Entities:   append([]string(nil), item.Entities...),
			Sentiment:  item.Sentiment,
			Importance: item.Importance,
		}
		if item.EntryTitle != nil {
			narrativeItem.Title = strings.TrimSpace(*item.EntryTitle)
		}
		if item.PublishedAt != nil {
			narrativeItem.PublishedAt = item.PublishedAt.Format(time.RFC3339)
		}
		analyses = append(analyses, narrativeItem)
	}

	focusedAnalyses := make([]narrativeAnalysisItem, 0, len(report.FocusedItems))
	for _, item := range report.FocusedItems {
		narrativeItem := narrativeAnalysisItem{
			FeedTitle:  item.FeedTitle,
			Tag:        item.Tag,
			FocusTags:  append([]string(nil), item.FocusTags...),
			Summary:    item.Summary,
			Entities:   append([]string(nil), item.Entities...),
			Sentiment:  item.Sentiment,
			Importance: item.Importance,
		}
		if item.EntryTitle != nil {
			narrativeItem.Title = strings.TrimSpace(*item.EntryTitle)
		}
		if item.PublishedAt != nil {
			narrativeItem.PublishedAt = item.PublishedAt.Format(time.RFC3339)
		}
		focusedAnalyses = append(focusedAnalyses, narrativeItem)
	}

	payload := map[string]any{
		"date":         report.Date,
		"total":        report.Total,
		"focusedTotal": report.FocusedTotal,
		"sentiment":    report.Sentiment,
		"topTags":      report.TopTags,
		"topEntities":  report.TopEntities,
		"topFeeds":     report.TopFeeds,
		"topAnalyses":  analyses,
		"focusedTags":  report.FocusedTags,
		"focusedItems": focusedAnalyses,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode daily report input: %w", err)
	}

	return string(encoded), nil
}

func parseArticleAnalysis(raw string) (ArticleAnalysisResult, error) {
	type articleAnalysisPayload struct {
		Tag        string          `json:"tag"`
		Summary    string          `json:"summary"`
		Entities   []string        `json:"entities"`
		Sentiment  string          `json:"sentiment"`
		Importance int             `json:"importance"`
		Latitude   json.RawMessage `json:"latitude"`
		Longitude  json.RawMessage `json:"longitude"`
	}

	var payload articleAnalysisPayload
	var result ArticleAnalysisResult

	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}

	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return result, fmt.Errorf("decode analysis json: %w", err)
	}

	latitude := parseCoordinate(payload.Latitude, -90, 90)
	longitude := parseCoordinate(payload.Longitude, -180, 180)
	if latitude == nil || longitude == nil {
		latitude = nil
		longitude = nil
	}

	result = ArticleAnalysisResult{
		Tag:        payload.Tag,
		Summary:    payload.Summary,
		Entities:   payload.Entities,
		Sentiment:  payload.Sentiment,
		Importance: payload.Importance,
		Latitude:   latitude,
		Longitude:  longitude,
	}

	result.Tag = strings.TrimSpace(result.Tag)
	if result.Tag != "" && !strings.HasPrefix(result.Tag, "#") {
		result.Tag = "#" + result.Tag
	}
	result.Summary = strings.TrimSpace(result.Summary)
	result.Sentiment = normalizeSentiment(result.Sentiment)
	result.Importance = clampImportance(result.Importance)
	result.Entities = normalizeEntities(result.Entities)

	return result, nil
}

func parseDailyReportNarrative(raw string) (DailyReportNarrativeResult, error) {
	type dailyReportPayload struct {
		Overview     string `json:"overview"`
		RiskReview   string `json:"riskReview"`
		TrendOutlook string `json:"trendOutlook"`
	}

	var payload dailyReportPayload
	var result DailyReportNarrativeResult

	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}

	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return result, fmt.Errorf("decode daily report json: %w", err)
	}

	result.Overview = strings.TrimSpace(payload.Overview)
	result.RiskReview = strings.TrimSpace(payload.RiskReview)
	result.TrendOutlook = strings.TrimSpace(payload.TrendOutlook)
	return result, nil
}

func parseLocationCoordinateResult(raw string) (ArticleLocationResult, error) {
	type locationPayload struct {
		Location  *string         `json:"location"`
		Latitude  json.RawMessage `json:"latitude"`
		Longitude json.RawMessage `json:"longitude"`
	}

	var payload locationPayload
	var result ArticleLocationResult

	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}

	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return result, fmt.Errorf("decode location json: %w", err)
	}

	latitude := parseCoordinate(payload.Latitude, -90, 90)
	longitude := parseCoordinate(payload.Longitude, -180, 180)
	if latitude == nil || longitude == nil {
		latitude = nil
		longitude = nil
	}

	result = ArticleLocationResult{
		Location:  normalizeOptionalString(payload.Location),
		Latitude:  latitude,
		Longitude: longitude,
	}

	return result, nil
}

func parseCoordinate(raw json.RawMessage, min, max float64) *float64 {
	if len(raw) == 0 {
		return nil
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}

	var value float64
	if err := json.Unmarshal(raw, &value); err == nil {
		return clampCoordinate(value, min, max)
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return parseCoordinateString(text, min, max)
	}

	return nil
}

func parseCoordinateString(value string, min, max float64) *float64 {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" || normalized == "NULL" {
		return nil
	}

	sign := 1.0
	if strings.ContainsAny(normalized, "SW") {
		sign = -1
	}

	replacer := strings.NewReplacer(
		"°", "",
		"N", "",
		"S", "",
		"E", "",
		"W", "",
		" ", "",
	)
	normalized = replacer.Replace(normalized)
	if strings.Count(normalized, ",") == 1 && !strings.Contains(normalized, ".") {
		normalized = strings.ReplaceAll(normalized, ",", ".")
	}

	parsed, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return nil
	}

	return clampCoordinate(parsed*sign, min, max)
}

func clampCoordinate(value, min, max float64) *float64 {
	if value < min || value > max {
		return nil
	}
	return &value
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *aiService) enrichAnalysisCoordinates(ctx context.Context, analysis *model.AIAnalysis, title, plainText string) error {
	if analysis == nil {
		return nil
	}
	if analysis.Latitude != nil && analysis.Longitude != nil {
		return nil
	}

	input := buildCoordinateLookupInput(title, analysis, plainText)
	if input == "" {
		return nil
	}

	cfg, err := s.getAIConfig(ctx, aiSceneAnalysis)
	if err != nil {
		return err
	}

	provider, err := ai.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	if err := s.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}

	raw, err := provider.Complete(ctx, s.prompts().CoordinateLookupPrompt(), ai.WrapInput(input))
	if err != nil {
		return err
	}

	resolved, err := parseLocationCoordinateResult(raw)
	if err != nil {
		return err
	}

	if resolved.Latitude != nil && resolved.Longitude != nil {
		analysis.Latitude = resolved.Latitude
		analysis.Longitude = resolved.Longitude
		return nil
	}

	if resolved.Location != nil {
		latitude, longitude, err := geocodeLocation(ctx, *resolved.Location)
		if err != nil {
			return err
		}
		if latitude != nil && longitude != nil {
			analysis.Latitude = latitude
			analysis.Longitude = longitude
		}
	}

	return nil
}

func buildCoordinateLookupInput(title string, analysis *model.AIAnalysis, plainText string) string {
	var builder strings.Builder

	appendField := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		builder.WriteString(label)
		builder.WriteString(": ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}

	appendField("title", title)
	if analysis != nil {
		appendField("tag", analysis.Tag)
		if len(analysis.Entities) > 0 {
			appendField("entities", strings.Join(analysis.Entities, ", "))
		}
		appendField("summary", analysis.Summary)
	}

	excerpt := strings.TrimSpace(plainText)
	if excerpt != "" {
		runes := []rune(excerpt)
		if len(runes) > 1200 {
			excerpt = string(runes[:1200])
		}
		appendField("content_excerpt", excerpt)
	}

	return strings.TrimSpace(builder.String())
}

func geocodeLocation(ctx context.Context, location string) (*float64, *float64, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return nil, nil, nil
	}

	requestCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
	}

	values := url.Values{}
	values.Set("format", "jsonv2")
	values.Set("limit", "1")
	values.Set("q", location)

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, geocodeSearchEndpoint+"?"+values.Encode(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build geocode request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", config.GistUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request geocode: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("geocode status: %s", resp.Status)
	}

	latitude, longitude, err := parseGeocodeSearchResponse(resp)
	if err != nil {
		return nil, nil, err
	}
	return latitude, longitude, nil
}

func parseGeocodeSearchResponse(resp *http.Response) (*float64, *float64, error) {
	var results []geocodeSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, nil, fmt.Errorf("decode geocode response: %w", err)
	}
	if len(results) == 0 {
		return nil, nil, nil
	}

	latitude := parseCoordinateString(results[0].Lat, -90, 90)
	longitude := parseCoordinateString(results[0].Lon, -180, 180)
	if latitude == nil || longitude == nil {
		return nil, nil, nil
	}
	return latitude, longitude, nil
}

func normalizeSentiment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "positive", "pos", "正面", "积极":
		return "positive"
	case "negative", "neg", "负面", "消极":
		return "negative"
	default:
		return "neutral"
	}
}

func clampImportance(value int) int {
	if value < 1 {
		return 1
	}
	if value > 10 {
		return 10
	}
	return value
}

func normalizeEntities(entities []string) []string {
	seen := make(map[string]struct{}, len(entities))
	normalized := make([]string, 0, len(entities))
	for _, entity := range entities {
		entity = strings.TrimSpace(entity)
		if entity == "" {
			continue
		}
		if _, ok := seen[entity]; ok {
			continue
		}
		seen[entity] = struct{}{}
		normalized = append(normalized, entity)
	}
	return normalized
}
