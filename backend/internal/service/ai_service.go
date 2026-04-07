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

type geocodeSearchResult struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
}

var geocodeSearchEndpoint = "https://nominatim.openstreetmap.org/search"

const storedAnalysisTitleLanguage = "zh-CN"

type AIServiceOption func(*aiService)

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
	settingsRepo        repository.SettingsRepository
	rateLimiter         *ai.RateLimiter
	entryRepo           repository.EntryRepository
	feedRepo            repository.FeedRepository
	folderRepo          repository.FolderRepository
	analysisArchiveDir  string
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

func (s *aiService) GetCachedSummary(ctx context.Context, entryID int64, isReadability bool) (*model.AISummary, error) {
	language := s.GetSummaryLanguage(ctx)
	return s.summaryRepo.Get(ctx, entryID, isReadability, language)
}

func (s *aiService) Summarize(ctx context.Context, entryID int64, content, title string, isReadability bool) (<-chan string, <-chan error, error) {
	// Get AI configuration
	cfg, err := s.getAIConfig(ctx)
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
	systemPrompt := ai.GetSummarizePrompt(title, language)

	// Convert HTML to plain text to save tokens
	plainText := ai.HTMLToText(content)

	// Wrap input with <input> tags
	wrappedInput := ai.WrapInput(plainText)

	// Start streaming
	textCh, errCh := provider.SummarizeStream(ctx, systemPrompt, wrappedInput)
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

func (s *aiService) getAIConfig(ctx context.Context) (ai.Config, error) {
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

	// Get provider
	cfg.Provider = settingsMap["ai.provider"]
	if cfg.Provider == "" {
		cfg.Provider = ai.ProviderOpenAI
	}

	// Get API key
	cfg.APIKey = settingsMap["ai.api_key"]
	if cfg.APIKey == "" {
		return cfg, fmt.Errorf("AI API key is not configured")
	}

	// Get base URL
	cfg.BaseURL = settingsMap["ai.base_url"]

	// Get model
	cfg.Model = settingsMap["ai.model"]
	if cfg.Model == "" {
		return cfg, fmt.Errorf("AI model is not configured")
	}

	// Get thinking settings
	if settingsMap["ai.thinking"] == "true" {
		cfg.Thinking = true
	}

	if val := settingsMap["ai.thinking_budget"]; val != "" {
		var budget int
		fmt.Sscanf(val, "%d", &budget)
		cfg.ThinkingBudget = budget
	}

	cfg.ReasoningEffort = settingsMap["ai.reasoning_effort"]

	return cfg, nil
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
	cfg, err := s.getAIConfig(ctx)
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
	systemPrompt := ai.GetArticleAnalysisPrompt(title, language)
	plainText := ai.HTMLToText(content)
	wrappedInput := ai.WrapInput(plainText)

	raw, err := provider.Complete(ctx, systemPrompt, wrappedInput)
	if err != nil {
		logger.Warn("ai analysis complete failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return nil, err
	}

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

	storedTitle := s.ensureStoredAnalysisTitleTranslation(ctx, entryID, title)
	s.archiveAnalysisMarkdown(ctx, entryID, storedTitle, *analysis)

	logger.Info("ai analysis completed", "module", "service", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID, "readability", isReadability)
	return analysis, nil
}

func (s *aiService) ListStoredAnalyses(ctx context.Context, limit, offset int) ([]model.StoredAIAnalysis, error) {
	return s.analysisRepo.List(ctx, limit, offset)
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

	cfg, err := s.getAIConfig(ctx)
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

	translatedTitle, err := provider.Complete(ctx, ai.GetTranslateTextPrompt("title", storedAnalysisTitleLanguage), ai.WrapInput(title))
	if err != nil {
		logger.Warn("ai analysis title translate failed", "module", "service", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return title
	}

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

func (s *aiService) archiveAnalysisMarkdown(ctx context.Context, entryID int64, translatedTitle string, analysis model.AIAnalysis) {
	if strings.TrimSpace(s.analysisArchiveDir) == "" || s.entryRepo == nil || s.feedRepo == nil {
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

	now := time.Now().In(time.Local)
	pathParts := []string{s.analysisArchiveDir, now.Format("20060102")}
	if folders := s.analysisArchiveFolders(ctx, feed); len(folders) > 0 {
		pathParts = append(pathParts, folders...)
	}
	pathParts = append(pathParts, sanitizeArchivePathSegment(feed.Title, "Feed"))

	dirPath := filepath.Join(pathParts...)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		logger.Warn("ai analysis archive mkdir failed", "module", "service", "action", "save", "resource", "ai_archive", "result", "failed", "entry_id", entryID, "path", dirPath, "error", err)
		return
	}

	fileTitle := strings.TrimSpace(translatedTitle)
	if fileTitle == "" && entry.Title != nil {
		fileTitle = strings.TrimSpace(*entry.Title)
	}
	fileTitle = sanitizeArchivePathSegment(fileTitle, fmt.Sprintf("entry-%d", entryID))

	filePath := resolveArchiveFilePath(dirPath, fileTitle, entryID)

	content := buildAIAnalysisMarkdown(entryID, entry, feed, analysis, translatedTitle, now)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		logger.Warn("ai analysis archive write failed", "module", "service", "action", "save", "resource", "ai_archive", "result", "failed", "entry_id", entryID, "path", filePath, "error", err)
		return
	}

	logger.Info("ai analysis archived", "module", "service", "action", "save", "resource", "ai_archive", "result", "ok", "entry_id", entryID, "path", filePath)
}

func (s *aiService) analysisArchiveFolders(ctx context.Context, feed model.Feed) []string {
	if s.folderRepo == nil || feed.FolderID == nil {
		return nil
	}

	segments := make([]string, 0, 4)
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

		segments = append(segments, sanitizeArchivePathSegment(folder.Name, fmt.Sprintf("folder-%d", folder.ID)))
		if folder.ParentID == nil {
			break
		}
		currentID = *folder.ParentID
	}

	for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
		segments[i], segments[j] = segments[j], segments[i]
	}
	return segments
}

func buildAIAnalysisMarkdown(
	entryID int64,
	entry model.Entry,
	feed model.Feed,
	analysis model.AIAnalysis,
	translatedTitle string,
	analyzedAt time.Time,
) string {
	title := strings.TrimSpace(translatedTitle)
	if title == "" && entry.Title != nil {
		title = strings.TrimSpace(*entry.Title)
	}
	if title == "" {
		title = fmt.Sprintf("Entry %d", entryID)
	}

	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	builder.WriteString(fmt.Sprintf("- Entry ID: %d\n", entryID))
	builder.WriteString(fmt.Sprintf("- Feed: %s\n", strings.TrimSpace(feed.Title)))
	if entry.Title != nil && strings.TrimSpace(*entry.Title) != "" && strings.TrimSpace(*entry.Title) != title {
		builder.WriteString(fmt.Sprintf("- Original Title: %s\n", strings.TrimSpace(*entry.Title)))
	}
	if entry.URL != nil && strings.TrimSpace(*entry.URL) != "" {
		builder.WriteString(fmt.Sprintf("- URL: %s\n", strings.TrimSpace(*entry.URL)))
	}
	if entry.Author != nil && strings.TrimSpace(*entry.Author) != "" {
		builder.WriteString(fmt.Sprintf("- Author: %s\n", strings.TrimSpace(*entry.Author)))
	}
	if entry.PublishedAt != nil {
		builder.WriteString(fmt.Sprintf("- Published At: %s\n", entry.PublishedAt.In(time.Local).Format(time.RFC3339)))
	}
	builder.WriteString(fmt.Sprintf("- AI Language: %s\n", strings.TrimSpace(analysis.Language)))
	builder.WriteString(fmt.Sprintf("- Analyzed At: %s\n\n", analyzedAt.Format(time.RFC3339)))

	builder.WriteString("## 摘要\n\n")
	builder.WriteString(strings.TrimSpace(analysis.Summary))
	builder.WriteString("\n\n")

	builder.WriteString("## 标签\n\n")
	builder.WriteString(strings.TrimSpace(analysis.Tag))
	builder.WriteString("\n\n")

	builder.WriteString("## 情绪与重要性\n\n")
	builder.WriteString(fmt.Sprintf("- Sentiment: %s\n", strings.TrimSpace(analysis.Sentiment)))
	builder.WriteString(fmt.Sprintf("- Importance: %d/10\n\n", analysis.Importance))

	builder.WriteString("## 实体\n\n")
	if len(analysis.Entities) == 0 {
		builder.WriteString("- None\n\n")
	} else {
		for _, entity := range analysis.Entities {
			entity = strings.TrimSpace(entity)
			if entity == "" {
				continue
			}
			builder.WriteString("- ")
			builder.WriteString(entity)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	if analysis.Latitude != nil && analysis.Longitude != nil {
		builder.WriteString("## 坐标\n\n")
		builder.WriteString(fmt.Sprintf("- Latitude: %.6f\n", *analysis.Latitude))
		builder.WriteString(fmt.Sprintf("- Longitude: %.6f\n\n", *analysis.Longitude))
	}

	return builder.String()
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
	storedTitle := s.resolveStoredAnalysisTitle(ctx, entryID)
	s.archiveAnalysisMarkdown(ctx, entryID, storedTitle, *analysis)
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

func resolveArchiveFilePath(dirPath, fileTitle string, entryID int64) string {
	preferredPath := filepath.Join(dirPath, fileTitle+".md")
	suffixedPath := filepath.Join(dirPath, fmt.Sprintf("%s-%d.md", fileTitle, entryID))

	switch {
	case archiveFileBelongsToEntry(preferredPath, entryID):
		return preferredPath
	case archiveFileBelongsToEntry(suffixedPath, entryID):
		return suffixedPath
	}

	if _, err := os.Stat(preferredPath); err == nil {
		return suffixedPath
	}
	return preferredPath
}

func archiveFileBelongsToEntry(path string, entryID int64) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), fmt.Sprintf("- Entry ID: %d\n", entryID))
}

func (s *aiService) BuildDailyAnalysisReport(ctx context.Context, day time.Time) (*model.AIDailyReport, error) {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	items, err := s.analysisRepo.ListByCreatedRange(ctx, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}

	report := &model.AIDailyReport{
		Date:        dayStart.Format("2006-01-02"),
		Total:       len(items),
		Sentiment:   model.AIDailyReportSentiment{},
		TopAnalyses: topStoredAnalyses(items, 10),
		TopTags:     buildTopCountItems(items, 10, extractTagSegments),
		TopEntities: buildTopCountItems(items, 10, extractEntities),
		TopFeeds:    buildTopFeedMetrics(items, 10),
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

	return report, nil
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
	cfg, err := s.getAIConfig(ctx)
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
				systemPrompt := ai.GetTranslateBlockPrompt(title, language)
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
		cfg, err = s.getAIConfig(ctx)
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
					titlePrompt := ai.GetTranslateTextPrompt("title", language)
					wrappedTitle := ai.WrapInput(a.Title)
					translated, err := provider.Complete(ctx, titlePrompt, wrappedTitle)
					if err != nil {
						select {
						case errCh <- fmt.Errorf("translate title for %s: %w", a.ID, err):
						default:
						}
						return
					}
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
					summaryPrompt := ai.GetTranslateTextPrompt("summary", language)
					wrappedSummary := ai.WrapInput(a.Summary)
					translated, err := provider.Complete(ctx, summaryPrompt, wrappedSummary)
					if err != nil {
						select {
						case errCh <- fmt.Errorf("translate summary for %s: %w", a.ID, err):
						default:
						}
						return
					}
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

	cfg, err := s.getAIConfig(ctx)
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

	raw, err := provider.Complete(ctx, ai.GetCoordinateLookupPrompt(), ai.WrapInput(input))
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
