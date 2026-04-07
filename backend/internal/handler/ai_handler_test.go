package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"gist/backend/internal/handler"
	"gist/backend/internal/model"
	"gist/backend/internal/service"
	"gist/backend/internal/service/mock"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type aiStatusProviderStub struct {
	status service.AIEntryProcessingStatus
	stats  service.AIQueueStats
}

func (s aiStatusProviderStub) EnsureQueued(entryID int64) service.AIEntryProcessingStatus {
	return s.status
}

func (s aiStatusProviderStub) GetQueueStats() service.AIQueueStats {
	return s.stats
}

type authValidatorStub struct {
	valid bool
	err   error
}

func (s authValidatorStub) ValidateToken(token string) (bool, error) {
	return s.valid, s.err
}

type dailyReportKeyProviderStub struct {
	key string
}

func (s dailyReportKeyProviderStub) GetAIDailyReportAccessKey(ctx context.Context) string {
	return s.key
}

func TestAIHandler_GetProcessingStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)
	handler.AttachAIStatusProviderHelper(h, aiStatusProviderStub{
		status: service.AIEntryProcessingStatus{
			Queued:     true,
			Running:    false,
			Processing: true,
		},
	})

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/ai/status/123", nil)
	c, rec := newTestContext(e, req)
	c.SetParamNames("entryId")
	c.SetParamValues("123")

	err := h.GetProcessingStatus(c)
	require.NoError(t, err)

	var resp handler.ProcessingStatusResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.True(t, resp.Queued)
	require.True(t, resp.Processing)
	require.False(t, resp.Running)
}

func TestAIHandler_ListStoredAnalyses(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)
	handler.AttachAIDailyReportAccessHelper(h, authValidatorStub{}, dailyReportKeyProviderStub{
		key: "daily-report-secret",
	})
	handler.AttachAIStatusProviderHelper(h, aiStatusProviderStub{
		stats: service.AIQueueStats{
			PendingCount: 5,
			QueuedCount:  3,
			RunningCount: 2,
			Processing:   true,
		},
	})

	now := time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC)
	title := "Stored article"
	feedTitle := "World Feed"
	url := "https://example.com/article"

	mockService.EXPECT().
		ListStoredAnalyses(gomock.Any(), 100, 0).
		Return([]model.StoredAIAnalysis{
			{
				ID:         1,
				EntryID:    101,
				FeedID:     202,
				FeedType:   "article",
				EntryTitle: &title,
				EntryURL:   &url,
				FeedTitle:  feedTitle,
				Language:   "zh-CN",
				Tag:        "#全球/2026/欧盟/关税/美国",
				Summary:    "summary",
				Entities:   []string{"欧盟", "美国"},
				Sentiment:  "neutral",
				Importance: 7,
				CreatedAt:  now,
			},
		}, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/ai/analyses", nil)
	req.Header.Set("X-Gist-API-Key", "daily-report-secret")
	c, rec := newTestContext(e, req)

	err := h.ListStoredAnalyses(c)
	require.NoError(t, err)

	var resp handler.ListStoredAnalysesResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Len(t, resp.Items, 1)
	require.Equal(t, int64(101), resp.Items[0].EntryID)
	require.Equal(t, feedTitle, resp.Items[0].FeedTitle)
	require.Equal(t, now.Format(time.RFC3339), resp.Items[0].CreatedAt)
}

func TestAIHandler_ListStoredAnalyses_Unauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)
	handler.AttachAIDailyReportAccessHelper(h, authValidatorStub{}, dailyReportKeyProviderStub{
		key: "daily-report-secret",
	})

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/ai/analyses", nil)
	c, rec := newTestContext(e, req)

	err := h.ListStoredAnalyses(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAIHandler_GetDailyAnalysisReport(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)
	handler.AttachAIDailyReportAccessHelper(h, authValidatorStub{}, dailyReportKeyProviderStub{
		key: "daily-report-secret",
	})
	handler.AttachAIStatusProviderHelper(h, aiStatusProviderStub{
		stats: service.AIQueueStats{
			PendingCount: 5,
			QueuedCount:  3,
			RunningCount: 2,
			Processing:   true,
		},
	})

	reportDay := time.Date(2026, 3, 30, 0, 0, 0, 0, time.Local)
	title := "Top article"

	mockService.EXPECT().
		BuildDailyAnalysisReport(gomock.Any(), reportDay).
		Return(&model.AIDailyReport{
			Date:  "2026-03-30",
			Total: 1,
			Sentiment: model.AIDailyReportSentiment{
				Positive: 1,
			},
			TopAnalyses: []model.StoredAIAnalysis{
				{
					ID:         1,
					EntryID:    101,
					FeedID:     201,
					FeedType:   "article",
					EntryTitle: &title,
					FeedTitle:  "World Feed",
					Language:   "zh-CN",
					Tag:        "#全球/美国",
					Summary:    "summary",
					Entities:   []string{"美国"},
					Sentiment:  "positive",
					Importance: 9,
					CreatedAt:  reportDay.Add(10 * time.Hour),
				},
			},
			TopTags: []model.AIDailyReportCountItem{
				{Name: "美国", Count: 1},
			},
			TopEntities: []model.AIDailyReportCountItem{
				{Name: "美国", Count: 1},
			},
			TopFeeds: []model.AIDailyReportFeedMetric{
				{FeedID: 201, FeedTitle: "World Feed", Count: 1},
			},
		}, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/ai/reports/daily?date=2026-03-30", nil)
	req.Header.Set("X-Gist-API-Key", "daily-report-secret")
	c, rec := newTestContext(e, req)

	err := h.GetDailyAnalysisReport(c)
	require.NoError(t, err)

	var resp handler.DailyAnalysisReportResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "2026-03-30", resp.Date)
	require.Equal(t, 1, resp.Total)
	require.Equal(t, 5, resp.PendingCount)
	require.Equal(t, 1, resp.Sentiment.Positive)
	require.Len(t, resp.TopAnalyses, 1)
	require.Equal(t, int64(101), resp.TopAnalyses[0].EntryID)
	require.Len(t, resp.TopTags, 1)
	require.Equal(t, "美国", resp.TopTags[0].Name)
	require.Len(t, resp.TopFeeds, 1)
	require.Equal(t, int64(201), resp.TopFeeds[0].FeedID)
}

func TestAIHandler_GetDailyAnalysisReport_Unauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)
	handler.AttachAIDailyReportAccessHelper(h, authValidatorStub{}, dailyReportKeyProviderStub{
		key: "daily-report-secret",
	})

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/ai/reports/daily?date=2026-03-30", nil)
	c, rec := newTestContext(e, req)

	err := h.GetDailyAnalysisReport(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAIHandler_Summarize_CacheHit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		"content": "test content",
		"title":   "test title",
	}
	req := newJSONRequest(http.MethodPost, "/ai/summarize", reqBody)
	c, rec := newTestContext(e, req)

	cached := &model.AISummary{
		Summary: "cached summary",
	}

	mockService.EXPECT().
		GetCachedSummary(gomock.Any(), int64(123), false).
		Return(cached, nil)

	err := h.Summarize(c)
	require.NoError(t, err)

	var resp handler.SummarizeResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "cached summary", resp.Summary)
	require.True(t, resp.Cached)
}

func TestAIHandler_Summarize_InvalidRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
	}
	req := newJSONRequest(http.MethodPost, "/ai/summarize", reqBody)
	c, rec := newTestContext(e, req)

	err := h.Summarize(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAIHandler_Analyze_CacheHit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		"content": "test content",
		"title":   "test title",
	}
	req := newJSONRequest(http.MethodPost, "/ai/analyze", reqBody)
	c, rec := newTestContext(e, req)

	lat := 35.6895
	lng := 139.6917
	cached := &model.AIAnalysis{
		Tag:        "#东亚/2026/日本/政策调整/美国",
		Summary:    "cached analysis",
		Entities:   []string{"日本", "美国"},
		Sentiment:  "neutral",
		Importance: 8,
		Latitude:   &lat,
		Longitude:  &lng,
	}

	mockService.EXPECT().
		GetCachedAnalysis(gomock.Any(), int64(123), false).
		Return(cached, nil)

	err := h.Analyze(c)
	require.NoError(t, err)

	var resp handler.AnalyzeResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, cached.Tag, resp.Tag)
	require.True(t, resp.Cached)
}

func TestAIHandler_Translate_CacheHit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		"content": "test content",
		"title":   "test title",
	}
	req := newJSONRequest(http.MethodPost, "/ai/translate", reqBody)
	c, rec := newTestContext(e, req)

	cached := &model.AITranslation{
		Content: "translated content",
	}

	mockService.EXPECT().
		GetCachedTranslation(gomock.Any(), int64(123), false).
		Return(cached, nil)

	err := h.Translate(c)
	require.NoError(t, err)

	var resp handler.TranslateResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "translated content", resp.Content)
	require.True(t, resp.Cached)
}

func TestAIHandler_TranslateBatch_InvalidRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"articles": []interface{}{},
	}
	req := newJSONRequest(http.MethodPost, "/ai/translate/batch", reqBody)
	c, rec := newTestContext(e, req)

	err := h.TranslateBatch(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAIHandler_TranslateBatch_TooManyArticles(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	// Create 101 articles to exceed the limit
	articles := make([]map[string]string, 101)
	for i := range articles {
		articles[i] = map[string]string{"id": "1", "title": "test", "summary": "test"}
	}

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"articles": articles,
	}
	req := newJSONRequest(http.MethodPost, "/ai/translate/batch", reqBody)
	c, rec := newTestContext(e, req)

	err := h.TranslateBatch(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAIHandler_TranslateBatch_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	req := newJSONRequestRaw(http.MethodPost, "/ai/translate/batch", "{invalid json")
	c, rec := newTestContext(e, req)

	err := h.TranslateBatch(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAIHandler_TranslateBatch_ServiceError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"articles": []map[string]string{
			{"id": "1", "title": "Test", "summary": "Summary"},
		},
	}
	req := newJSONRequest(http.MethodPost, "/ai/translate/batch", reqBody)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		TranslateBatch(gomock.Any(), gomock.Any()).
		Return(nil, nil, errors.New("service error"))

	err := h.TranslateBatch(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAIHandler_TranslateBatch_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"articles": []map[string]string{
			{"id": "1", "title": "Test", "summary": "Summary"},
		},
	}
	req := newJSONRequest(http.MethodPost, "/ai/translate/batch", reqBody)
	c, rec := newTestContext(e, req)

	resultChan := make(chan service.BatchTranslateResult, 1)
	title := "Translated Title"
	summary := "Translated Summary"
	resultChan <- service.BatchTranslateResult{
		ID:      "1",
		Title:   &title,
		Summary: &summary,
		Cached:  true,
	}
	close(resultChan)

	errChan := make(chan error)
	close(errChan)

	mockService.EXPECT().
		TranslateBatch(gomock.Any(), gomock.Any()).
		Return((<-chan service.BatchTranslateResult)(resultChan), (<-chan error)(errChan), nil)

	err := h.TranslateBatch(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/x-ndjson")
	require.Contains(t, rec.Body.String(), "Translated Title")
}

func TestAIHandler_ClearCache_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	req := newJSONRequest(http.MethodDelete, "/ai/cache", nil)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		ClearAllCache(gomock.Any()).
		Return(int64(10), int64(5), int64(3), int64(2), nil)

	err := h.ClearCache(c)
	require.NoError(t, err)

	var resp handler.ClearCacheResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, int64(10), resp.Summaries)
	require.Equal(t, int64(5), resp.Translations)
	require.Equal(t, int64(3), resp.ListTranslations)
	require.Equal(t, int64(2), resp.Analyses)
}

func TestAIHandler_Summarize_StreamResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	// Mock service return nil (cache miss)
	mockService.EXPECT().
		GetCachedSummary(gomock.Any(), int64(123), false).
		Return(nil, nil)

	// Mock service return channel
	resultChan := make(chan string, 3)
	resultChan <- "First chunk"
	resultChan <- "Second chunk"
	resultChan <- "Final chunk"
	close(resultChan)

	mockService.EXPECT().
		Summarize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(resultChan, make(<-chan error), nil)

	mockService.EXPECT().
		SaveSummary(gomock.Any(), int64(123), false, "First chunkSecond chunkFinal chunk").
		Return(nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		"content": "test content",
		"title":   "test title",
	}
	req := newJSONRequest(http.MethodPost, "/ai/summarize", reqBody)
	c, rec := newTestContext(e, req)

	err := h.Summarize(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")

	body := rec.Body.String()
	require.Contains(t, body, "First chunk")
	require.Contains(t, body, "Second chunk")
	require.Contains(t, body, "Final chunk")
}

func TestAIHandler_Translate_StreamResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	// Mock service return nil (cache miss)
	mockService.EXPECT().
		GetCachedTranslation(gomock.Any(), int64(123), false).
		Return(nil, nil)

	// Mock service return channel
	resultChan := make(chan service.TranslateBlockResult, 2)
	resultChan <- service.TranslateBlockResult{Index: 0, HTML: "Translated chunk 1"}
	resultChan <- service.TranslateBlockResult{Index: 1, HTML: "Translated chunk 2"}
	close(resultChan)

	mockService.EXPECT().
		TranslateBlocks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return([]service.TranslateBlockInfo{{Index: 0}, {Index: 1}}, resultChan, make(<-chan error), nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		"content": "test content",
		"title":   "test title",
	}
	req := newJSONRequest(http.MethodPost, "/ai/translate", reqBody)
	c, rec := newTestContext(e, req)

	err := h.Translate(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")

	body := rec.Body.String()
	require.Contains(t, body, "data: {\"index\":0,\"html\":\"Translated chunk 1\"}")
	require.Contains(t, body, "data: {\"index\":1,\"html\":\"Translated chunk 2\"}")
}

func TestAIHandler_Summarize_ServiceError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	mockService.EXPECT().
		GetCachedSummary(gomock.Any(), int64(123), false).
		Return(nil, nil)

	mockService.EXPECT().
		Summarize(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil, errors.New("AI service error"))

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		"content": "test content",
		"title":   "test title",
	}
	req := newJSONRequest(http.MethodPost, "/ai/summarize", reqBody)
	c, rec := newTestContext(e, req)

	err := h.Summarize(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAIHandler_Translate_InvalidRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		// missing content and title
	}
	req := newJSONRequest(http.MethodPost, "/ai/translate", reqBody)
	c, rec := newTestContext(e, req)

	err := h.Translate(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAIHandler_Translate_ServiceError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	mockService.EXPECT().
		GetCachedTranslation(gomock.Any(), int64(123), false).
		Return(nil, nil)

	mockService.EXPECT().
		TranslateBlocks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil, nil, errors.New("AI service error"))

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		"content": "test content",
		"title":   "test title",
	}
	req := newJSONRequest(http.MethodPost, "/ai/translate", reqBody)
	c, rec := newTestContext(e, req)

	err := h.Translate(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAIHandler_ClearCache_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	req := newJSONRequest(http.MethodDelete, "/ai/cache", nil)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		ClearAllCache(gomock.Any()).
		Return(int64(0), int64(0), int64(0), int64(0), errors.New("cache clear error"))

	err := h.ClearCache(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAIHandler_Summarize_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	req := newJSONRequestRaw(http.MethodPost, "/ai/summarize", "{invalid json")
	c, rec := newTestContext(e, req)

	err := h.Summarize(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAIHandler_Translate_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	e := newTestEcho()
	req := newJSONRequestRaw(http.MethodPost, "/ai/translate", "{invalid json")
	c, rec := newTestContext(e, req)

	err := h.Translate(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAIHandler_Summarize_CacheLookupError_ContinuesWithService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	// Cache lookup fails, but handler continues with service call
	mockService.EXPECT().
		GetCachedSummary(gomock.Any(), int64(123), false).
		Return(nil, errors.New("cache lookup error"))

	// Handler continues to call Summarize after cache error
	resultChan := make(chan string, 1)
	resultChan <- "Summary content"
	close(resultChan)

	mockService.EXPECT().
		Summarize(gomock.Any(), int64(123), "test content", "test title", false).
		Return(resultChan, make(<-chan error), nil)

	mockService.EXPECT().
		SaveSummary(gomock.Any(), int64(123), false, "Summary content").
		Return(nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		"content": "test content",
		"title":   "test title",
	}
	req := newJSONRequest(http.MethodPost, "/ai/summarize", reqBody)
	c, rec := newTestContext(e, req)

	err := h.Summarize(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAIHandler_Translate_CacheLookupError_ContinuesWithService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockAIService(ctrl)
	h := handler.NewAIHandlerHelper(mockService)

	// Cache lookup fails, but handler continues with service call
	mockService.EXPECT().
		GetCachedTranslation(gomock.Any(), int64(123), false).
		Return(nil, errors.New("cache lookup error"))

	// Handler continues to call TranslateBlocks after cache error
	resultChan := make(chan service.TranslateBlockResult, 1)
	resultChan <- service.TranslateBlockResult{Index: 0, HTML: "Translated"}
	close(resultChan)

	mockService.EXPECT().
		TranslateBlocks(gomock.Any(), int64(123), "test content", "test title", false).
		Return([]service.TranslateBlockInfo{{Index: 0}}, resultChan, make(<-chan error), nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"entryId": "123",
		"content": "test content",
		"title":   "test title",
	}
	req := newJSONRequest(http.MethodPost, "/ai/translate", reqBody)
	c, rec := newTestContext(e, req)

	err := h.Translate(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
}
