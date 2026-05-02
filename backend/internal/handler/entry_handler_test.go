package handler_test

import (
	"gist/backend/internal/handler"
	"gist/backend/internal/service"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"gist/backend/internal/model"
	"gist/backend/internal/service/mock"
)

func TestEntryHandler_List_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/entries?limit=10", nil)
	c, rec := newTestContext(e, req)

	title1 := "Entry 1"
	title2 := "Entry 2"
	entries := []model.Entry{
		{ID: 1, Title: &title1, HasAnalysis: true},
		{ID: 2, Title: &title2, HasAnalysis: false},
	}

	mockService.EXPECT().
		List(gomock.Any(), gomock.Any()).
		Return(entries, nil)

	err := h.List(c)
	require.NoError(t, err)

	var resp handler.EntryListResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Len(t, resp.Entries, 2)
	require.True(t, resp.Entries[0].HasAnalysis)
	require.False(t, resp.Entries[1].HasAnalysis)
	require.False(t, resp.HasMore, "should not have more with 2 entries when limit is 10")
}

func TestEntryHandler_List_HasMore(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/entries?limit=2", nil)
	c, rec := newTestContext(e, req)

	// Return 3 entries when limit is 2 (limit+1 pattern)
	title1 := "Entry 1"
	title2 := "Entry 2"
	title3 := "Entry 3"
	entries := []model.Entry{
		{ID: 1, Title: &title1},
		{ID: 2, Title: &title2},
		{ID: 3, Title: &title3},
	}

	mockService.EXPECT().
		List(gomock.Any(), gomock.Any()).
		Return(entries, nil)

	err := h.List(c)
	require.NoError(t, err)

	var resp handler.EntryListResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Len(t, resp.Entries, 2, "should return only limit entries")
	require.True(t, resp.HasMore, "should have more when returned entries > limit")
}

func TestEntryHandler_GetByID_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/entries/123", nil)
	c, rec := newTestContext(e, req)
	setPathParams(c, map[string]string{"id": "123"})

	title := "Test Entry"
	entry := model.Entry{
		ID:          123,
		Title:       &title,
		HasAnalysis: true,
	}

	mockService.EXPECT().
		GetByID(gomock.Any(), int64(123)).
		Return(entry, nil)

	err := h.GetByID(c)
	require.NoError(t, err)

	var resp handler.EntryResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "123", resp.ID)
	require.NotNil(t, resp.Title)
	require.Equal(t, "Test Entry", *resp.Title)
	require.True(t, resp.HasAnalysis)
}

func TestEntryHandler_GetFocus_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/entries/123/focus", nil)
	c, rec := newTestContext(e, req)
	setPathParams(c, map[string]string{"id": "123"})

	mockService.EXPECT().
		GetFocus(gomock.Any(), int64(123)).
		Return(model.EntryFocus{EntryID: 123, Focused: true, Tags: []string{"军工", "欧洲"}}, nil)

	err := h.GetFocus(c)
	require.NoError(t, err)

	var resp handler.EntryFocusResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "123", resp.EntryID)
	require.True(t, resp.Focused)
	require.Equal(t, []string{"军工", "欧洲"}, resp.Tags)
}

func TestEntryHandler_UpdateRead_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"read": true,
	}
	req := newJSONRequest(http.MethodPatch, "/entries/123/read", reqBody)
	c, rec := newTestContext(e, req)
	setPathParams(c, map[string]string{"id": "123"})

	mockService.EXPECT().
		MarkAsRead(gomock.Any(), int64(123), true).
		Return(nil)

	err := h.UpdateReadStatus(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestEntryHandler_UpdateStarred_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"starred": true,
	}
	req := newJSONRequest(http.MethodPatch, "/entries/123/starred", reqBody)
	c, rec := newTestContext(e, req)
	setPathParams(c, map[string]string{"id": "123"})

	mockService.EXPECT().
		MarkAsStarred(gomock.Any(), int64(123), true).
		Return(nil)

	err := h.UpdateStarredStatus(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestEntryHandler_UpdateFocus_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"focused": true,
		"tags":    []string{"能源", "制裁"},
	}
	req := newJSONRequest(http.MethodPut, "/entries/123/focus", reqBody)
	c, rec := newTestContext(e, req)
	setPathParams(c, map[string]string{"id": "123"})

	mockService.EXPECT().
		UpdateFocus(gomock.Any(), int64(123), true, []string{"能源", "制裁"}).
		Return(model.EntryFocus{EntryID: 123, Focused: true, Tags: []string{"制裁", "能源"}}, nil)

	err := h.UpdateFocus(c)
	require.NoError(t, err)

	var resp handler.EntryFocusResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "123", resp.EntryID)
	require.True(t, resp.Focused)
	require.Equal(t, []string{"制裁", "能源"}, resp.Tags)
}

func TestEntryHandler_FetchReadable_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	mockReadability := mock.NewMockReadabilityService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, mockReadability, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodPost, "/entries/123/fetch-readable", nil)
	c, rec := newTestContext(e, req)
	setPathParams(c, map[string]string{"id": "123"})

	mockReadability.EXPECT().
		FetchReadableContent(gomock.Any(), int64(123)).
		Return("readable content", nil)

	err := h.FetchReadable(c)
	require.NoError(t, err)

	var resp handler.ReadableContentResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "readable content", resp.ReadableContent)
}

func TestEntryHandler_MarkAllAsRead_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"feedId": "123",
	}
	req := newJSONRequest(http.MethodPost, "/entries/mark-read", reqBody)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		MarkAllAsRead(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	err := h.MarkAllAsRead(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestEntryHandler_GetUnreadCounts_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/unread-counts", nil)
	c, rec := newTestContext(e, req)

	counts := map[int64]int{
		1: 10,
		2: 5,
	}
	mockService.EXPECT().
		GetUnreadCounts(gomock.Any()).
		Return(counts, nil)

	err := h.GetUnreadCounts(c)
	require.NoError(t, err)

	var resp handler.UnreadCountsResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, 10, resp.Counts["1"])
	require.Equal(t, 5, resp.Counts["2"])
}

func TestEntryHandler_GetFeedAIStats_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/feed-ai-stats", nil)
	c, rec := newTestContext(e, req)

	stats := map[int64]service.FeedAIStats{
		1: {UnreadCount: 10, AnalyzedCount: 7, PendingCount: 3},
		2: {UnreadCount: 5, AnalyzedCount: 5, PendingCount: 0},
	}
	mockService.EXPECT().
		GetFeedAIStats(gomock.Any()).
		Return(stats, nil)

	err := h.GetFeedAIStats(c)
	require.NoError(t, err)

	var resp handler.FeedAIStatsResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, 10, resp.Stats["1"].UnreadCount)
	require.Equal(t, 7, resp.Stats["1"].AnalyzedCount)
	require.Equal(t, 3, resp.Stats["1"].PendingCount)
	require.Equal(t, 5, resp.Stats["2"].UnreadCount)
	require.Equal(t, 5, resp.Stats["2"].AnalyzedCount)
	require.Equal(t, 0, resp.Stats["2"].PendingCount)
}

func TestEntryHandler_GetStarredCount_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/starred-count", nil)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		GetStarredCount(gomock.Any()).
		Return(42, nil)

	err := h.GetStarredCount(c)
	require.NoError(t, err)

	var resp handler.StarredCountResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, 42, resp.Count)
}

func TestEntryHandler_ClearCaches_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockEntryService(ctrl)
	h := handler.NewEntryHandlerHelper(mockService, nil, nil)

	// Test ClearReadabilityCache
	e := newTestEcho()
	req := newJSONRequest(http.MethodDelete, "/entries/readability-cache", nil)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		ClearReadabilityCache(gomock.Any()).
		Return(int64(5), nil)

	err := h.ClearReadabilityCache(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp1 handler.EntryClearResponse
	parseJSONResponse(t, rec, &resp1)
	require.Equal(t, int64(5), resp1.Deleted)

	// Test ClearEntryCache
	req2 := newJSONRequest(http.MethodDelete, "/entries/cache", nil)
	c2, rec2 := newTestContext(e, req2)

	mockService.EXPECT().
		ClearEntryCache(gomock.Any()).
		Return(int64(10), nil)

	err = h.ClearEntryCache(c2)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp2 handler.EntryClearResponse
	parseJSONResponse(t, rec2, &resp2)
	require.Equal(t, int64(10), resp2.Deleted)
}

func TestEntryHandler_List_Errors(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		mockSetup  func(*mock.MockEntryService)
		wantStatus int
	}{
		{
			name:  "invalid feedId",
			query: "?feedId=abc",
			mockSetup: func(m *mock.MockEntryService) {
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "invalid folderId",
			query: "?folderId=abc",
			mockSetup: func(m *mock.MockEntryService) {
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "invalid contentType",
			query: "?contentType=invalid",
			mockSetup: func(m *mock.MockEntryService) {
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "service error",
			query: "?limit=10",
			mockSetup: func(m *mock.MockEntryService) {
				m.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, fmtErrorf("db error"))
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockService := mock.NewMockEntryService(ctrl)
			h := handler.NewEntryHandlerHelper(mockService, nil, nil)

			e := newTestEcho()
			req := newJSONRequest(http.MethodGet, "/entries"+tt.query, nil)
			c, rec := newTestContext(e, req)

			tt.mockSetup(mockService)

			err := h.List(c)
			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func fmtErrorf(s string) error {
	return &errorString{s}
}

type errorString struct {
	s string
}

func (e *errorString) Error() string { return e.s }
