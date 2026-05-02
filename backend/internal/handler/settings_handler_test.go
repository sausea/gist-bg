package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gist/backend/internal/handler"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"gist/backend/internal/service"
	"gist/backend/internal/service/mock"
)

func TestSettingsHandler_GetAISettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/settings/ai", nil)
	c, rec := newTestContext(e, req)

	settings := &service.AISettings{
		Analysis: service.AIModelSettings{
			Provider: "openai",
			Model:    "gpt-4",
		},
		WorkerCount: 3,
	}

	mockService.EXPECT().
		GetAISettings(gomock.Any()).
		Return(settings, nil)

	err := h.GetAISettings(c)
	require.NoError(t, err)

	var resp handler.AISettingsResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "openai", resp.Analysis.Provider)
	require.Equal(t, 3, resp.WorkerCount)
}

func TestSettingsHandler_GetAIUsageStats_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/settings/ai/usage?days=14", nil)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		GetAIUsageStats(gomock.Any(), 14).
		Return(&service.AIUsageStats{
			Today: service.AIUsagePeriodStats{
				AIUsageCounter: service.AIUsageCounter{
					RequestCount:     2,
					PromptTokens:     120,
					CompletionTokens: 80,
					TotalTokens:      200,
				},
			},
		}, nil)

	err := h.GetAIUsageStats(c)
	require.NoError(t, err)

	var resp service.AIUsageStats
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, 2, resp.Today.RequestCount)
	require.Equal(t, 200, resp.Today.TotalTokens)
}

func TestSettingsHandler_GetAIPromptSettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/settings/ai/prompts", nil)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		GetAIPromptSettings(gomock.Any()).
		Return(&service.AIPromptSettings{
			Dir: "/app/data/prompts",
			Templates: []service.AIPromptTemplate{
				{Key: "analysis", FileName: "analysis.tmpl", Variables: []string{".TargetLanguage"}, Content: "analysis", DefaultContent: "default"},
			},
		}, nil)

	err := h.GetAIPromptSettings(c)
	require.NoError(t, err)

	var resp handler.AIPromptSettingsResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "/app/data/prompts", resp.Dir)
	require.Len(t, resp.Templates, 1)
	require.Equal(t, "analysis", resp.Templates[0].Key)
	require.Equal(t, "analysis.tmpl", resp.Templates[0].FileName)
}

func TestSettingsHandler_UpdateAISettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"analysis": map[string]interface{}{
			"provider": "openai",
			"model":    "gpt-4",
		},
		"translation": map[string]interface{}{
			"provider": "openai",
			"model":    "gpt-4o-mini",
		},
		"report": map[string]interface{}{
			"provider": "anthropic",
			"model":    "claude-sonnet",
		},
		"workerCount": 5,
	}
	req := newJSONRequest(http.MethodPut, "/settings/ai", reqBody)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		SetAISettings(gomock.Any(), gomock.Any()).
		Return(nil)

	mockService.EXPECT().
		GetAISettings(gomock.Any()).
		Return(&service.AISettings{Analysis: service.AIModelSettings{Provider: "openai", Model: "gpt-4"}, WorkerCount: 5}, nil)

	err := h.UpdateAISettings(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSettingsHandler_UpdateAIPromptSettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	reqBody := map[string]any{
		"templates": []map[string]any{
			{
				"key":     "analysis",
				"content": "updated analysis prompt",
			},
		},
	}
	req := newJSONRequest(http.MethodPut, "/settings/ai/prompts", reqBody)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		SetAIPromptSettings(gomock.Any(), gomock.Any()).
		Return(nil)

	mockService.EXPECT().
		GetAIPromptSettings(gomock.Any()).
		Return(&service.AIPromptSettings{
			Dir: "/app/data/prompts",
			Templates: []service.AIPromptTemplate{
				{Key: "analysis", FileName: "analysis.tmpl", Content: "updated analysis prompt", DefaultContent: "default"},
			},
		}, nil)

	err := h.UpdateAIPromptSettings(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSettingsHandler_TestNetworkProxy_Disabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"enabled": false,
	}
	req := newJSONRequest(http.MethodPost, "/settings/network/test", reqBody)
	c, rec := newTestContext(e, req)

	err := h.TestNetworkProxy(c)
	require.NoError(t, err)

	var resp handler.NetworkTestResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.True(t, resp.Success)
}

func TestSettingsHandler_TestNetworkProxy_InvalidParams(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"enabled": true,
		"host":    "",
	}
	req := newJSONRequest(http.MethodPost, "/settings/network/test", reqBody)
	c, rec := newTestContext(e, req)

	err := h.TestNetworkProxy(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsHandler_GetGeneralSettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/settings/general", nil)
	c, rec := newTestContext(e, req)

	settings := &service.GeneralSettings{
		AutoReadability:      true,
		AIDailyReportAPIKey:  "***123",
		AIAnalysisArchiveDir: "/tmp/gist-ai",
	}

	mockService.EXPECT().
		GetGeneralSettings(gomock.Any()).
		Return(settings, nil)

	err := h.GetGeneralSettings(c)
	require.NoError(t, err)

	var resp handler.GeneralSettingsResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.True(t, resp.AutoReadability)
	require.Equal(t, "***123", resp.AIDailyReportAPIKey)
	require.Equal(t, "/tmp/gist-ai", resp.AIAnalysisArchiveDir)
}

func TestSettingsHandler_UpdateGeneralSettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"autoReadability":      true,
		"aiDailyReportApiKey":  "report-secret",
		"aiAnalysisArchiveDir": "/tmp/gist-ai",
	}
	req := newJSONRequest(http.MethodPut, "/settings/general", reqBody)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		SetGeneralSettings(gomock.Any(), gomock.Any()).
		Return(nil)

	mockService.EXPECT().
		GetGeneralSettings(gomock.Any()).
		Return(&service.GeneralSettings{AutoReadability: true, AIAnalysisArchiveDir: "/tmp/gist-ai"}, nil)

	err := h.UpdateGeneralSettings(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSettingsHandler_GetAppearanceSettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/settings/appearance", nil)
	c, rec := newTestContext(e, req)

	settings := &service.AppearanceSettings{
		ContentTypes: []string{"article"},
	}

	mockService.EXPECT().
		GetAppearanceSettings(gomock.Any()).
		Return(settings, nil)

	err := h.GetAppearanceSettings(c)
	require.NoError(t, err)

	var resp handler.AppearanceSettingsResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, []string{"article"}, resp.ContentTypes)
}

func TestSettingsHandler_UpdateAppearanceSettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"contentTypes": []string{"article", "picture"},
	}
	req := newJSONRequest(http.MethodPut, "/settings/appearance", reqBody)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		SetAppearanceSettings(gomock.Any(), gomock.Any()).
		Return(nil)

	mockService.EXPECT().
		GetAppearanceSettings(gomock.Any()).
		Return(&service.AppearanceSettings{ContentTypes: []string{"article", "picture"}}, nil)

	err := h.UpdateAppearanceSettings(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSettingsHandler_TestAI_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"provider": "openai",
		"apiKey":   "sk-test",
		"model":    "gpt-4",
	}
	req := newJSONRequest(http.MethodPost, "/settings/ai/test", reqBody)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		TestAI(gomock.Any(), "openai", "sk-test", "", "gpt-4", "responses", false, 0, "").
		Return("OK", nil)

	err := h.TestAI(c)
	require.NoError(t, err)

	var resp handler.AITestResponse
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, "OK", resp.Message)
}

func TestSettingsHandler_ClearAnubisCookies_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodDelete, "/settings/anubis-cookies", nil)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		ClearAnubisCookies(gomock.Any()).
		Return(int64(5), nil)

	err := h.ClearAnubisCookies(c)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSettingsHandler_GetNetworkSettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	req := newJSONRequest(http.MethodGet, "/settings/network", nil)
	c, rec := newTestContext(e, req)

	settings := &service.NetworkSettings{
		Enabled:  true,
		Type:     "socks5",
		Host:     "127.0.0.1",
		Port:     7890,
		Username: "user",
		Password: "***",
		IPStack:  "ipv4",
	}

	mockService.EXPECT().
		GetNetworkSettings(gomock.Any()).
		Return(settings, nil)

	err := h.GetNetworkSettings(c)
	require.NoError(t, err)

	var resp map[string]interface{}
	assertJSONResponse(t, rec, http.StatusOK, &resp)
	require.Equal(t, true, resp["enabled"])
	require.Equal(t, "socks5", resp["type"])
	require.Equal(t, "127.0.0.1", resp["host"])
	require.Equal(t, "ipv4", resp["ipStack"])
}

func TestSettingsHandler_UpdateNetworkSettings_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	reqBody := map[string]interface{}{
		"enabled":  true,
		"type":     "http",
		"host":     "proxy.local",
		"port":     8080,
		"username": "user",
		"password": "secret",
		"ipStack":  "default",
	}
	req := newJSONRequest(http.MethodPut, "/settings/network", reqBody)
	c, rec := newTestContext(e, req)

	mockService.EXPECT().
		SetNetworkSettings(gomock.Any(), gomock.Any()).
		Return(nil)

	mockService.EXPECT().
		GetNetworkSettings(gomock.Any()).
		Return(&service.NetworkSettings{
			Enabled:  true,
			Type:     "http",
			Host:     "proxy.local",
			Port:     8080,
			Username: "user",
			Password: "***",
			IPStack:  "default",
		}, nil)

	err := h.UpdateNetworkSettings(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSettingsHandler_UpdateNetworkSettings_InvalidRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := mock.NewMockSettingsService(ctrl)
	h := handler.NewSettingsHandlerHelper(mockService, nil)

	e := newTestEcho()
	req := httptest.NewRequest(http.MethodPut, "/settings/network", newBody(`{"enabled":`))
	req.Header.Set("Content-Type", "application/json")
	c, rec := newTestContext(e, req)

	err := h.UpdateNetworkSettings(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
