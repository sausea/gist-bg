package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"gist/backend/internal/model"
	"gist/backend/internal/service"
	aiservice "gist/backend/internal/service/ai"
	"gist/backend/pkg/logger"
)

type AIHandler struct {
	service        service.AIService
	statusProvider aiProcessingStatusProvider
	authValidator  dailyReportTokenValidator
	keyProvider    dailyReportAPIKeyProvider
}

type aiProcessingStatusProvider interface {
	GetProcessingStatus(entryID int64) service.AIEntryProcessingStatus
	GetQueueStats() service.AIQueueStats
	ListQueue(limit int) ([]model.AIAnalysisQueueItem, error)
}

type dailyReportTokenValidator interface {
	ValidateToken(token string) (bool, error)
}

type dailyReportAPIKeyProvider interface {
	GetAIDailyReportAccessKey(ctx context.Context) string
}

// Request/Response types

type summarizeRequest struct {
	EntryID       string `json:"entryId"`
	Content       string `json:"content"`
	Title         string `json:"title"`
	IsReadability bool   `json:"isReadability"`
}

type summarizeResponse struct {
	Summary string `json:"summary"`
	Cached  bool   `json:"cached"`
}

type analyzeRequest struct {
	EntryID       string `json:"entryId"`
	Content       string `json:"content"`
	Title         string `json:"title"`
	IsReadability bool   `json:"isReadability"`
}

type analyzeResponse struct {
	Tag        string   `json:"tag"`
	Summary    string   `json:"summary"`
	Entities   []string `json:"entities"`
	Sentiment  string   `json:"sentiment"`
	Importance int      `json:"importance"`
	Latitude   *float64 `json:"latitude,omitempty"`
	Longitude  *float64 `json:"longitude,omitempty"`
	Cached     bool     `json:"cached"`
}

type translateRequest struct {
	EntryID       string `json:"entryId"`
	Content       string `json:"content"`
	Title         string `json:"title"`
	IsReadability bool   `json:"isReadability"`
	ReturnBlocks  bool   `json:"returnBlocks"`
}

type translateResponse struct {
	Content string `json:"content"`
	Cached  bool   `json:"cached"`
}

type processingStatusResponse struct {
	Queued     bool `json:"queued"`
	Running    bool `json:"running"`
	Processing bool `json:"processing"`
}

type storedAnalysisResponse struct {
	ID            int64    `json:"id"`
	EntryID       int64    `json:"entryId"`
	FeedID        int64    `json:"feedId"`
	FeedType      string   `json:"feedType"`
	EntryTitle    *string  `json:"entryTitle,omitempty"`
	EntryURL      *string  `json:"entryUrl,omitempty"`
	FeedTitle     string   `json:"feedTitle"`
	Author        *string  `json:"author,omitempty"`
	PublishedAt   *string  `json:"publishedAt,omitempty"`
	Focused       bool     `json:"focused"`
	FocusTags     []string `json:"focusTags"`
	IsReadability bool     `json:"isReadability"`
	Language      string   `json:"language"`
	Tag           string   `json:"tag"`
	Summary       string   `json:"summary"`
	Entities      []string `json:"entities"`
	Sentiment     string   `json:"sentiment"`
	Importance    int      `json:"importance"`
	Latitude      *float64 `json:"latitude,omitempty"`
	Longitude     *float64 `json:"longitude,omitempty"`
	CreatedAt     string   `json:"createdAt"`
}

type listStoredAnalysesResponse struct {
	Items []storedAnalysisResponse `json:"items"`
}

type analysisQueueItemResponse struct {
	ID           int64   `json:"id"`
	EntryID      int64   `json:"entryId"`
	FeedID       int64   `json:"feedId"`
	FeedType     string  `json:"feedType"`
	EntryTitle   *string `json:"entryTitle,omitempty"`
	EntryURL     *string `json:"entryUrl,omitempty"`
	FeedTitle    string  `json:"feedTitle"`
	Author       *string `json:"author,omitempty"`
	PublishedAt  *string `json:"publishedAt,omitempty"`
	Status       string  `json:"status"`
	Source       string  `json:"source"`
	ContentMode  string  `json:"contentMode"`
	Language     string  `json:"language"`
	RetryCount   int     `json:"retryCount"`
	ErrorMessage *string `json:"errorMessage,omitempty"`
	CreatedAt    string  `json:"createdAt"`
	StartedAt    *string `json:"startedAt,omitempty"`
	FinishedAt   *string `json:"finishedAt,omitempty"`
	UpdatedAt    string  `json:"updatedAt"`
}

type listAnalysisQueueResponse struct {
	PendingCount int                         `json:"pendingCount"`
	QueuedCount  int                         `json:"queuedCount"`
	RunningCount int                         `json:"runningCount"`
	FailedCount  int                         `json:"failedCount"`
	Processing   bool                        `json:"processing"`
	Items        []analysisQueueItemResponse `json:"items"`
}

type dailyReportSentimentResponse struct {
	Positive int `json:"positive"`
	Neutral  int `json:"neutral"`
	Negative int `json:"negative"`
	Other    int `json:"other"`
}

type dailyReportCountItemResponse struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type dailyReportFeedMetricResponse struct {
	FeedID    int64  `json:"feedId"`
	FeedTitle string `json:"feedTitle"`
	Count     int    `json:"count"`
}

type dailyAnalysisReportResponse struct {
	Date         string                          `json:"date"`
	Total        int                             `json:"total"`
	PendingCount int                             `json:"pendingCount"`
	FocusedTotal int                             `json:"focusedTotal"`
	Sentiment    dailyReportSentimentResponse    `json:"sentiment"`
	TopAnalyses  []storedAnalysisResponse        `json:"topAnalyses"`
	TopTags      []dailyReportCountItemResponse  `json:"topTags"`
	TopEntities  []dailyReportCountItemResponse  `json:"topEntities"`
	TopFeeds     []dailyReportFeedMetricResponse `json:"topFeeds"`
	FocusedTags  []dailyReportCountItemResponse  `json:"focusedTags"`
	FocusedItems []storedAnalysisResponse        `json:"focusedItems"`
	Overview     string                          `json:"overview,omitempty"`
	RiskReview   string                          `json:"riskReview,omitempty"`
	TrendOutlook string                          `json:"trendOutlook,omitempty"`
}

func NewAIHandler(service service.AIService) *AIHandler {
	return &AIHandler{service: service}
}

func AttachAIStatusProvider(h *AIHandler, provider aiProcessingStatusProvider) {
	if h == nil {
		return
	}
	h.statusProvider = provider
}

func AttachAIDailyReportAccess(h *AIHandler, validator dailyReportTokenValidator, provider dailyReportAPIKeyProvider) {
	if h == nil {
		return
	}
	h.authValidator = validator
	h.keyProvider = provider
}

func (h *AIHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/ai/status/:entryId", h.GetProcessingStatus)
	g.POST("/ai/summarize", h.Summarize)
	g.POST("/ai/analyze", h.Analyze)
	g.POST("/ai/translate", h.Translate)
	g.POST("/ai/translate/batch", h.TranslateBatch)
	g.DELETE("/ai/cache", h.ClearCache)
}

func (h *AIHandler) RegisterPublicRoutes(g *echo.Group) {
	g.GET("/ai/analyses", h.ListStoredAnalyses)
	g.GET("/ai/queue", h.ListAnalysisQueue)
	g.GET("/ai/reports/daily", h.GetDailyAnalysisReport)
}

func (h *AIHandler) GetProcessingStatus(c echo.Context) error {
	entryID, err := strconv.ParseInt(c.Param("entryId"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid entry ID"})
	}

	if h.statusProvider == nil {
		return c.JSON(http.StatusOK, processingStatusResponse{})
	}

	status := h.statusProvider.GetProcessingStatus(entryID)
	return c.JSON(http.StatusOK, processingStatusResponse{
		Queued:     status.Queued,
		Running:    status.Running,
		Processing: status.Processing,
	})
}

func (h *AIHandler) ListStoredAnalyses(c echo.Context) error {
	authorized, err := h.authorizeExternalAIRead(c)
	if err != nil {
		return err
	}
	if !authorized {
		return nil
	}

	limit := 100
	if value := strings.TrimSpace(c.QueryParam("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid limit"})
		}
		if parsed > 200 {
			parsed = 200
		}
		limit = parsed
	}

	offset := 0
	if value := strings.TrimSpace(c.QueryParam("offset")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid offset"})
		}
		offset = parsed
	}

	items, err := h.service.ListStoredAnalyses(c.Request().Context(), limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
	}

	resp := make([]storedAnalysisResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toStoredAnalysisResponse(item))
	}

	return c.JSON(http.StatusOK, listStoredAnalysesResponse{Items: resp})
}

func (h *AIHandler) ListAnalysisQueue(c echo.Context) error {
	authorized, err := h.authorizeExternalAIRead(c)
	if err != nil {
		return err
	}
	if !authorized {
		return nil
	}

	limit := 50
	if value := strings.TrimSpace(c.QueryParam("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid limit"})
		}
		if parsed > 200 {
			parsed = 200
		}
		limit = parsed
	}

	stats := h.queueStats()
	if h.statusProvider == nil {
		return c.JSON(http.StatusOK, listAnalysisQueueResponse{
			PendingCount: stats.PendingCount,
			QueuedCount:  stats.QueuedCount,
			RunningCount: stats.RunningCount,
			FailedCount:  stats.FailedCount,
			Processing:   stats.Processing,
			Items:        []analysisQueueItemResponse{},
		})
	}

	items, err := h.statusProvider.ListQueue(limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
	}

	resp := make([]analysisQueueItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toAnalysisQueueItemResponse(item))
	}

	return c.JSON(http.StatusOK, listAnalysisQueueResponse{
		PendingCount: stats.PendingCount,
		QueuedCount:  stats.QueuedCount,
		RunningCount: stats.RunningCount,
		FailedCount:  stats.FailedCount,
		Processing:   stats.Processing,
		Items:        resp,
	})
}

func (h *AIHandler) GetDailyAnalysisReport(c echo.Context) error {
	authorized, err := h.authorizeExternalAIRead(c)
	if err != nil {
		return err
	}
	if !authorized {
		return nil
	}

	dateValue := strings.TrimSpace(c.QueryParam("date"))
	reportDay := time.Now()
	if dateValue != "" {
		parsed, err := time.ParseInLocation("2006-01-02", dateValue, time.Local)
		if err != nil {
			return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid date"})
		}
		reportDay = parsed
	}

	report, err := h.service.BuildDailyAnalysisReport(c.Request().Context(), reportDay)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
	}

	return c.JSON(http.StatusOK, toDailyAnalysisReportResponse(report, h.queuePendingCount()))
}

func (h *AIHandler) authorizeExternalAIRead(c echo.Context) (bool, error) {
	authHeader := strings.TrimSpace(c.Request().Header.Get("Authorization"))
	if authHeader != "" && h.authValidator != nil {
		if token, ok := extractBearerToken(authHeader); ok {
			valid, err := h.authValidator.ValidateToken(token)
			if err == nil && valid {
				return true, nil
			}
		}
	}

	if h.keyProvider != nil {
		expectedKey := strings.TrimSpace(h.keyProvider.GetAIDailyReportAccessKey(c.Request().Context()))
		providedKey := strings.TrimSpace(c.Request().Header.Get("X-Gist-API-Key"))
		if providedKey == "" {
			providedKey = strings.TrimSpace(c.Request().Header.Get("X-API-Key"))
		}
		if expectedKey != "" && providedKey != "" && subtle.ConstantTimeCompare([]byte(expectedKey), []byte(providedKey)) == 1 {
			return true, nil
		}
	}

	return false, c.JSON(http.StatusUnauthorized, errorResponse{Error: "missing authentication"})
}

func extractBearerToken(value string) (string, bool) {
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	return token, token != ""
}

func toStoredAnalysisResponse(item model.StoredAIAnalysis) storedAnalysisResponse {
	resp := storedAnalysisResponse{
		ID:            item.ID,
		EntryID:       item.EntryID,
		FeedID:        item.FeedID,
		FeedType:      item.FeedType,
		EntryTitle:    item.EntryTitle,
		EntryURL:      item.EntryURL,
		FeedTitle:     item.FeedTitle,
		Author:        item.Author,
		Focused:       item.Focused,
		FocusTags:     append([]string{}, item.FocusTags...),
		IsReadability: item.IsReadability,
		Language:      item.Language,
		Tag:           item.Tag,
		Summary:       item.Summary,
		Entities:      item.Entities,
		Sentiment:     item.Sentiment,
		Importance:    item.Importance,
		Latitude:      item.Latitude,
		Longitude:     item.Longitude,
		CreatedAt:     item.CreatedAt.Format(time.RFC3339),
	}
	if item.PublishedAt != nil {
		formatted := item.PublishedAt.Format(time.RFC3339)
		resp.PublishedAt = &formatted
	}
	return resp
}

func toAnalysisQueueItemResponse(item model.AIAnalysisQueueItem) analysisQueueItemResponse {
	resp := analysisQueueItemResponse{
		ID:           item.ID,
		EntryID:      item.EntryID,
		FeedID:       item.FeedID,
		FeedType:     item.FeedType,
		EntryTitle:   item.EntryTitle,
		EntryURL:     item.EntryURL,
		FeedTitle:    item.FeedTitle,
		Author:       item.Author,
		Status:       item.Status,
		Source:       item.Source,
		ContentMode:  item.ContentMode,
		Language:     item.Language,
		RetryCount:   item.RetryCount,
		ErrorMessage: item.ErrorMessage,
		CreatedAt:    item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    item.UpdatedAt.Format(time.RFC3339),
	}
	if item.PublishedAt != nil {
		formatted := item.PublishedAt.Format(time.RFC3339)
		resp.PublishedAt = &formatted
	}
	if item.StartedAt != nil {
		formatted := item.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &formatted
	}
	if item.FinishedAt != nil {
		formatted := item.FinishedAt.Format(time.RFC3339)
		resp.FinishedAt = &formatted
	}
	return resp
}

func toDailyAnalysisReportResponse(report *model.AIDailyReport, pendingCount int) dailyAnalysisReportResponse {
	if report == nil {
		return dailyAnalysisReportResponse{
			PendingCount: pendingCount,
			TopAnalyses:  []storedAnalysisResponse{},
			TopTags:      []dailyReportCountItemResponse{},
			TopEntities:  []dailyReportCountItemResponse{},
			TopFeeds:     []dailyReportFeedMetricResponse{},
			FocusedTags:  []dailyReportCountItemResponse{},
			FocusedItems: []storedAnalysisResponse{},
		}
	}

	resp := dailyAnalysisReportResponse{
		Date:         report.Date,
		Total:        report.Total,
		PendingCount: pendingCount,
		FocusedTotal: report.FocusedTotal,
		Overview:     report.Overview,
		RiskReview:   report.RiskReview,
		TrendOutlook: report.TrendOutlook,
		Sentiment: dailyReportSentimentResponse{
			Positive: report.Sentiment.Positive,
			Neutral:  report.Sentiment.Neutral,
			Negative: report.Sentiment.Negative,
			Other:    report.Sentiment.Other,
		},
		TopAnalyses:  make([]storedAnalysisResponse, 0, len(report.TopAnalyses)),
		TopTags:      make([]dailyReportCountItemResponse, 0, len(report.TopTags)),
		TopEntities:  make([]dailyReportCountItemResponse, 0, len(report.TopEntities)),
		TopFeeds:     make([]dailyReportFeedMetricResponse, 0, len(report.TopFeeds)),
		FocusedTags:  make([]dailyReportCountItemResponse, 0, len(report.FocusedTags)),
		FocusedItems: make([]storedAnalysisResponse, 0, len(report.FocusedItems)),
	}

	for _, item := range report.TopAnalyses {
		resp.TopAnalyses = append(resp.TopAnalyses, toStoredAnalysisResponse(item))
	}
	for _, item := range report.TopTags {
		resp.TopTags = append(resp.TopTags, dailyReportCountItemResponse{
			Name:  item.Name,
			Count: item.Count,
		})
	}
	for _, item := range report.TopEntities {
		resp.TopEntities = append(resp.TopEntities, dailyReportCountItemResponse{
			Name:  item.Name,
			Count: item.Count,
		})
	}
	for _, item := range report.TopFeeds {
		resp.TopFeeds = append(resp.TopFeeds, dailyReportFeedMetricResponse{
			FeedID:    item.FeedID,
			FeedTitle: item.FeedTitle,
			Count:     item.Count,
		})
	}
	for _, item := range report.FocusedTags {
		resp.FocusedTags = append(resp.FocusedTags, dailyReportCountItemResponse{
			Name:  item.Name,
			Count: item.Count,
		})
	}
	for _, item := range report.FocusedItems {
		resp.FocusedItems = append(resp.FocusedItems, toStoredAnalysisResponse(item))
	}

	return resp
}

func (h *AIHandler) queuePendingCount() int {
	return h.queueStats().PendingCount
}

func (h *AIHandler) queueStats() service.AIQueueStats {
	if h.statusProvider == nil {
		return service.AIQueueStats{}
	}
	return h.statusProvider.GetQueueStats()
}

// Summarize generates an AI summary of the content.
// @Summary Generate AI summary
// @Description Generate an AI summary of the article content. Returns cached result if available, otherwise streams the response.
// @Tags ai
// @Accept json
// @Produce json
// @Produce text/event-stream
// @Param request body summarizeRequest true "Summarize request"
// @Success 200 {object} summarizeResponse "Cached summary"
// @Failure 400 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /ai/summarize [post]
func (h *AIHandler) Summarize(c echo.Context) error {
	var req summarizeRequest
	if err := c.Bind(&req); err != nil {
		logger.Debug("ai summarize invalid request", "module", "handler", "action", "request", "resource", "ai", "result", "failed", "error", err)
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid request"})
	}

	if req.Content == "" {
		logger.Debug("ai summarize missing content", "module", "handler", "action", "request", "resource", "ai", "result", "failed")
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "content is required"})
	}

	// Parse entry ID
	entryID, err := strconv.ParseInt(req.EntryID, 10, 64)
	if err != nil {
		logger.Debug("ai summarize invalid entry id", "module", "handler", "action", "request", "resource", "ai", "result", "failed", "entry_id", req.EntryID)
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid entry ID"})
	}

	ctx := c.Request().Context()

	// Check cache first
	cached, err := h.service.GetCachedSummary(ctx, entryID, req.IsReadability)
	if err != nil {
		logger.Warn("ai summarize cache lookup failed", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
	}
	if cached != nil {
		logger.Info("ai summarize cache hit", "module", "handler", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID, "cache", "hit")
		return c.JSON(http.StatusOK, summarizeResponse{
			Summary: cached.Summary,
			Cached:  true,
		})
	}

	// Generate summary with streaming
	textCh, errCh, err := h.service.Summarize(ctx, entryID, req.Content, req.Title, req.IsReadability)
	if err != nil {
		logger.Error("ai summarize start failed", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
	}

	logger.Info("ai summarize started", "module", "handler", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID)

	// Set headers for SSE
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	var fullText strings.Builder

	// Stream the response
	for {
		select {
		case text, ok := <-textCh:
			if !ok {
				// Channel closed, check for errors
				select {
				case err := <-errCh:
					if err != nil {
						logger.Error("ai summarize stream error", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
						// Write error to stream
						fmt.Fprintf(c.Response(), "event: error\ndata: %s\n\n", err.Error())
						c.Response().Flush()
						return nil
					}

				default:
				}

				// Save to cache if we got content
				if fullText.Len() > 0 {
					if err := h.service.SaveSummary(ctx, entryID, req.IsReadability, fullText.String()); err != nil {
						logger.Warn("ai summarize cache save failed", "module", "handler", "action", "save", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
					} else {
						logger.Info("ai summarize cached", "module", "handler", "action", "save", "resource", "ai", "result", "ok", "entry_id", entryID)
					}
				}

				logger.Info("ai summarize completed", "module", "handler", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID)
				return nil
			}

			fullText.WriteString(text)

			// Write chunk to stream (plain text, not SSE format for simpler client handling)
			if _, err := c.Response().Write([]byte(text)); err != nil {
				return nil
			}
			c.Response().Flush()

		case <-ctx.Done():
			logger.Warn("ai summarize cancelled", "module", "handler", "action", "fetch", "resource", "ai", "result", "cancelled", "entry_id", entryID)
			return nil
		}
	}
}

// Analyze generates structured AI metadata for the article.
// @Summary Generate AI analysis
// @Description Generate structured tags, summary, entities, sentiment, importance and coordinates for the article.
// @Tags ai
// @Accept json
// @Produce json
// @Param request body analyzeRequest true "Analyze request"
// @Success 200 {object} analyzeResponse
// @Failure 400 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /ai/analyze [post]
func (h *AIHandler) Analyze(c echo.Context) error {
	var req analyzeRequest
	if err := c.Bind(&req); err != nil {
		logger.Debug("ai analyze invalid request", "module", "handler", "action", "request", "resource", "ai", "result", "failed", "error", err)
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid request"})
	}

	if req.Content == "" {
		logger.Debug("ai analyze missing content", "module", "handler", "action", "request", "resource", "ai", "result", "failed")
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "content is required"})
	}

	entryID, err := strconv.ParseInt(req.EntryID, 10, 64)
	if err != nil {
		logger.Debug("ai analyze invalid entry id", "module", "handler", "action", "request", "resource", "ai", "result", "failed", "entry_id", req.EntryID)
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid entry ID"})
	}

	ctx := c.Request().Context()

	cached, err := h.service.GetCachedAnalysis(ctx, entryID, req.IsReadability)
	if err != nil {
		logger.Warn("ai analyze cache lookup failed", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
	}
	if cached != nil {
		logger.Info("ai analyze cache hit", "module", "handler", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID, "cache", "hit")
		return c.JSON(http.StatusOK, analyzeResponse{
			Tag:        cached.Tag,
			Summary:    cached.Summary,
			Entities:   cached.Entities,
			Sentiment:  cached.Sentiment,
			Importance: cached.Importance,
			Latitude:   cached.Latitude,
			Longitude:  cached.Longitude,
			Cached:     true,
		})
	}

	analysis, err := h.service.Analyze(ctx, entryID, req.Content, req.Title, req.IsReadability)
	if err != nil {
		logger.Error("ai analyze failed", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
	}

	return c.JSON(http.StatusOK, analyzeResponse{
		Tag:        analysis.Tag,
		Summary:    analysis.Summary,
		Entities:   analysis.Entities,
		Sentiment:  analysis.Sentiment,
		Importance: analysis.Importance,
		Latitude:   analysis.Latitude,
		Longitude:  analysis.Longitude,
		Cached:     false,
	})
}

// translateInitEvent represents the initial event with all original blocks.
type translateInitEvent struct {
	Blocks []translateBlockData `json:"blocks"`
}

type translateBlockData struct {
	Index         int    `json:"index"`
	HTML          string `json:"html"`
	NeedTranslate bool   `json:"needTranslate"`
}

// translateBlockEvent represents an SSE event for translated block.
type translateBlockEvent struct {
	Index int    `json:"index"`
	HTML  string `json:"html"`
}

// translateDoneEvent represents the completion of translation.
type translateDoneEvent struct {
	Done bool `json:"done"`
}

// translateErrorEvent represents an error during translation.
type translateErrorEvent struct {
	Error string `json:"error"`
}

// Translate generates an AI translation of the content.
// @Summary Generate AI translation
// @Description Translate article content. Returns cached result if available, otherwise streams block translations via SSE.
// @Tags ai
// @Accept json
// @Produce json
// @Produce text/event-stream
// @Param request body translateRequest true "Translate request"
// @Success 200 {object} translateResponse
// @Failure 400 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /ai/translate [post]
func (h *AIHandler) Translate(c echo.Context) error {
	var req translateRequest
	if err := c.Bind(&req); err != nil {
		logger.Debug("ai translate invalid request", "module", "handler", "action", "request", "resource", "ai", "result", "failed", "error", err)
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid request"})
	}

	if req.Content == "" {
		logger.Debug("ai translate missing content", "module", "handler", "action", "request", "resource", "ai", "result", "failed")
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "content is required"})
	}

	// Parse entry ID
	entryID, err := strconv.ParseInt(req.EntryID, 10, 64)
	if err != nil {
		logger.Debug("ai translate invalid entry id", "module", "handler", "action", "request", "resource", "ai", "result", "failed", "entry_id", req.EntryID)
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid entry ID"})
	}

	ctx := c.Request().Context()

	// Check cache first
	cached, err := h.service.GetCachedTranslation(ctx, entryID, req.IsReadability)
	if err != nil {
		logger.Warn("ai translate cache lookup failed", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
	}
	if cached != nil {
		logger.Info("ai translate cache hit", "module", "handler", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID, "cache", "hit")
		// Default behavior: return full cached content as JSON.
		if !req.ReturnBlocks {
			return c.JSON(http.StatusOK, translateResponse{
				Content: cached.Content,
				Cached:  true,
			})
		}

		// Block mode: stream original blocks + cached translated blocks via SSE so frontend
		// can render bilingual (original + translated) view without re-calling the LLM.
		originalBlocks, parseErr := aiservice.ParseHTMLBlocks(req.Content)
		if parseErr != nil || len(originalBlocks) == 0 {
			// Fallback to JSON if block parsing fails.
			logger.Warn("ai translate cached blocks parse failed", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", parseErr)
			return c.JSON(http.StatusOK, translateResponse{
				Content: cached.Content,
				Cached:  true,
			})
		}

		translatedBlocks, parseTranslatedErr := aiservice.ParseHTMLBlocks(cached.Content)
		if parseTranslatedErr != nil || len(translatedBlocks) == 0 {
			logger.Warn("ai translate cached translated blocks parse failed", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", parseTranslatedErr)
			return c.JSON(http.StatusOK, translateResponse{
				Content: cached.Content,
				Cached:  true,
			})
		}

		translatedByIndex := make(map[int]string, len(translatedBlocks))
		for _, b := range translatedBlocks {
			translatedByIndex[b.Index] = b.HTML
		}

		// Set headers for SSE
		c.Response().Header().Set("Content-Type", "text/event-stream")
		c.Response().Header().Set("Cache-Control", "no-cache")
		c.Response().Header().Set("Connection", "keep-alive")
		c.Response().WriteHeader(http.StatusOK)

		// Send init event with all original blocks
		initBlocks := make([]translateBlockData, len(originalBlocks))
		for i, b := range originalBlocks {
			initBlocks[i] = translateBlockData{
				Index:         b.Index,
				HTML:          b.HTML,
				NeedTranslate: b.NeedTranslate,
			}
		}
		initEvent := translateInitEvent{Blocks: initBlocks}
		initData, _ := json.Marshal(initEvent)
		fmt.Fprintf(c.Response(), "data: %s\n\n", initData)
		c.Response().Flush()

		// Send translated blocks for those that need translation
		for _, b := range originalBlocks {
			if ctx.Err() != nil {
				logger.Warn("ai translate cached blocks cancelled", "module", "handler", "action", "fetch", "resource", "ai", "result", "cancelled", "entry_id", entryID)
				return nil
			}
			if !b.NeedTranslate {
				continue
			}
			if translatedHTML, ok := translatedByIndex[b.Index]; ok && strings.TrimSpace(translatedHTML) != "" {
				event := translateBlockEvent{Index: b.Index, HTML: translatedHTML}
				data, _ := json.Marshal(event)
				fmt.Fprintf(c.Response(), "data: %s\n\n", data)
				c.Response().Flush()
			}
		}

		// Done
		doneEvent := translateDoneEvent{Done: true}
		doneData, _ := json.Marshal(doneEvent)
		fmt.Fprintf(c.Response(), "data: %s\n\n", doneData)
		c.Response().Flush()

		return nil
	}

	// Start block translation
	blockInfos, resultCh, errCh, err := h.service.TranslateBlocks(ctx, entryID, req.Content, req.Title, req.IsReadability)
	if err != nil {
		logger.Error("ai translate start failed", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
	}

	logger.Info("ai translate started", "module", "handler", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID)

	// Set headers for SSE
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	// Send init event with all original blocks
	initBlocks := make([]translateBlockData, len(blockInfos))
	for i, b := range blockInfos {
		initBlocks[i] = translateBlockData{
			Index:         b.Index,
			HTML:          b.HTML,
			NeedTranslate: b.NeedTranslate,
		}
	}
	initEvent := translateInitEvent{Blocks: initBlocks}
	initData, _ := json.Marshal(initEvent)
	fmt.Fprintf(c.Response(), "data: %s\n\n", initData)
	c.Response().Flush()

	// Stream the translation results
	for {
		select {
		case result, ok := <-resultCh:
			if !ok {
				// Channel closed, send done event
				doneEvent := translateDoneEvent{Done: true}
				data, _ := json.Marshal(doneEvent)
				fmt.Fprintf(c.Response(), "data: %s\n\n", data)
				c.Response().Flush()
				logger.Info("ai translate completed", "module", "handler", "action", "fetch", "resource", "ai", "result", "ok", "entry_id", entryID)
				return nil
			}

			// Send translated block result
			event := translateBlockEvent{Index: result.Index, HTML: result.HTML}
			data, _ := json.Marshal(event)
			fmt.Fprintf(c.Response(), "data: %s\n\n", data)
			c.Response().Flush()

		case err := <-errCh:
			if err != nil {
				logger.Error("ai translate stream error", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "entry_id", entryID, "error", err)
				errorEvent := translateErrorEvent{Error: err.Error()}
				data, _ := json.Marshal(errorEvent)
				fmt.Fprintf(c.Response(), "data: %s\n\n", data)
				c.Response().Flush()
				// Continue to receive remaining results
			}

		case <-ctx.Done():
			logger.Warn("ai translate cancelled", "module", "handler", "action", "fetch", "resource", "ai", "result", "cancelled", "entry_id", entryID)
			return nil
		}
	}
}

// batchTranslateRequest represents the request body for batch translation.
type batchTranslateRequest struct {
	Articles []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"articles"`
}

// TranslateBatch translates multiple articles' titles and summaries.
// @Summary Batch translate articles
// @Description Translate multiple articles' titles and summaries. Returns NDJSON stream.
// @Tags ai
// @Accept json
// @Produce application/x-ndjson
// @Param request body batchTranslateRequest true "Batch translate request"
// @Success 200 {object} service.BatchTranslateResult
// @Failure 400 {object} errorResponse
// @Failure 500 {object} errorResponse
// @Router /ai/translate/batch [post]
func (h *AIHandler) TranslateBatch(c echo.Context) error {
	var req batchTranslateRequest
	if err := c.Bind(&req); err != nil {
		logger.Debug("ai batch translate invalid request", "module", "handler", "action", "request", "resource", "ai", "result", "failed", "error", err)
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid request"})
	}

	if len(req.Articles) == 0 {
		logger.Debug("ai batch translate missing articles", "module", "handler", "action", "request", "resource", "ai", "result", "failed")
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "articles is required"})
	}

	// Limit batch size
	if len(req.Articles) > 100 {
		logger.Debug("ai batch translate too many articles", "module", "handler", "action", "request", "resource", "ai", "result", "failed", "count", len(req.Articles))
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "maximum 100 articles per batch"})
	}

	ctx := c.Request().Context()

	// Convert to service input
	articles := make([]service.BatchArticleInput, len(req.Articles))
	for i, a := range req.Articles {
		articles[i] = service.BatchArticleInput{
			ID:      a.ID,
			Title:   a.Title,
			Summary: a.Summary,
		}
	}

	// Start batch translation
	resultCh, errCh, err := h.service.TranslateBatch(ctx, articles)
	if err != nil {
		logger.Error("ai batch translate start failed", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "count", len(articles), "error", err)
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
	}

	logger.Info("ai batch translate started", "module", "handler", "action", "fetch", "resource", "ai", "result", "ok", "count", len(articles))

	// Set headers for NDJSON streaming
	c.Response().Header().Set("Content-Type", "application/x-ndjson")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	// Stream the results
	for {
		select {
		case result, ok := <-resultCh:
			if !ok {
				// Channel closed, done
				logger.Info("ai batch translate completed", "module", "handler", "action", "fetch", "resource", "ai", "result", "ok", "count", len(articles))
				return nil
			}

			// Send result as NDJSON
			data, _ := json.Marshal(result)
			c.Response().Write(data)
			c.Response().Write([]byte("\n"))
			c.Response().Flush()

		case err := <-errCh:
			if err != nil {
				logger.Error("ai batch translate stream error", "module", "handler", "action", "fetch", "resource", "ai", "result", "failed", "error", err)
				// Continue to receive remaining results
			}

		case <-ctx.Done():
			logger.Warn("ai batch translate cancelled", "module", "handler", "action", "fetch", "resource", "ai", "result", "cancelled", "count", len(articles))
			return nil
		}
	}
}

type clearCacheResponse struct {
	Summaries        int64 `json:"summaries"`
	Translations     int64 `json:"translations"`
	ListTranslations int64 `json:"listTranslations"`
	Analyses         int64 `json:"analyses"`
}

// ClearCache deletes all AI cache data.
// @Summary Clear AI cache
// @Description Delete all AI-generated summaries and translations cache.
// @Tags ai
// @Produce json
// @Success 200 {object} clearCacheResponse
// @Failure 500 {object} errorResponse
// @Router /ai/cache [delete]
func (h *AIHandler) ClearCache(c echo.Context) error {
	ctx := c.Request().Context()

	summaries, translations, listTranslations, analyses, err := h.service.ClearAllCache(ctx)
	if err != nil {
		logger.Error("ai cache clear failed", "module", "handler", "action", "clear", "resource", "ai", "result", "failed", "error", err)
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
	}

	logger.Info("ai cache cleared", "module", "handler", "action", "clear", "resource", "ai", "result", "ok", "summaries", summaries, "translations", translations, "list_translations", listTranslations, "analyses", analyses)
	return c.JSON(http.StatusOK, clearCacheResponse{
		Summaries:        summaries,
		Translations:     translations,
		ListTranslations: listTranslations,
		Analyses:         analyses,
	})
}
