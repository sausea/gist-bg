package service_test

import (
	"context"
	"encoding/json"
	"gist/backend/internal/service"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gist/backend/internal/service/ai"

	"github.com/stretchr/testify/require"
)

func TestSettingsService_GetAISettings_Defaults(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	settings, err := svc.GetAISettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, ai.ProviderOpenAI, settings.Analysis.Provider)
	require.Equal(t, 10000, settings.Analysis.ThinkingBudget)
	require.Equal(t, "medium", settings.Analysis.ReasoningEffort)
	require.Equal(t, "zh-CN", settings.SummaryLanguage)
	require.False(t, settings.AutoAnalysis)
	require.Equal(t, ai.DefaultRateLimit, settings.RateLimit)
	require.Equal(t, 2, settings.WorkerCount)
}

func TestSettingsService_GetAISettings_MaskedKey(t *testing.T) {
	repo := newSettingsRepoStub()
	repo.data[service.KeyAIProvider] = ai.ProviderOpenAI
	repo.data[service.KeyAIAPIKey] = "sk-test-1234567890"
	repo.data[service.KeyAIBaseURL] = "https://api.example.com"
	repo.data[service.KeyAIModel] = "gpt-4"
	repo.data[service.KeyAIThinking] = "true"
	repo.data[service.KeyAIThinkingBudget] = "9000"
	repo.data[service.KeyAIReasoningEffort] = "high"
	repo.data[service.KeyAISummaryLanguage] = "en-US"
	repo.data[service.KeyAIAutoTranslate] = "true"
	repo.data[service.KeyAIAutoSummary] = "true"
	repo.data[service.KeyAIAutoAnalysis] = "true"
	repo.data[service.KeyAIRateLimit] = "5"
	repo.data[service.KeyAIWorkerCount] = "4"

	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))
	settings, err := svc.GetAISettings(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, "sk-test-1234567890", settings.Analysis.APIKey)
	require.NotEmpty(t, settings.Analysis.APIKey)
	require.Equal(t, ai.ProviderOpenAI, settings.Analysis.Provider)
	require.Equal(t, "gpt-4", settings.Analysis.Model)
	require.True(t, settings.Analysis.Thinking)
	require.Equal(t, 9000, settings.Analysis.ThinkingBudget)
	require.True(t, settings.AutoAnalysis)
	require.Equal(t, 5, settings.RateLimit)
	require.Equal(t, 4, settings.WorkerCount)
}

func TestSettingsService_SetAISettings_StoresAndUpdatesLimiter(t *testing.T) {
	repo := newSettingsRepoStub()
	limiter := ai.NewRateLimiter(1)
	svc := service.NewSettingsService(repo, limiter)

	settings := &service.AISettings{
		Analysis: service.AIModelSettings{
			Provider:        ai.ProviderOpenAI,
			APIKey:          "sk-realkey-123",
			BaseURL:         "https://api.example.com",
			Model:           "gpt-4",
			Thinking:        true,
			ThinkingBudget:  5000,
			ReasoningEffort: "high",
		},
		Translation: service.AIModelSettings{
			Provider: ai.ProviderCompatible,
			APIKey:   "translate-key",
			BaseURL:  "https://translate.example.com/v1",
			Model:    "translate-model",
		},
		Report: service.AIModelSettings{
			Provider: ai.ProviderAnthropic,
			APIKey:   "report-key",
			Model:    "claude-report",
		},
		SummaryLanguage: "en-US",
		AutoTranslate:   true,
		AutoAnalysis:    true,
		RateLimit:       20,
		WorkerCount:     6,
	}

	err := svc.SetAISettings(context.Background(), settings)
	require.NoError(t, err)
	require.Equal(t, "sk-realkey-123", repo.data[service.KeyAIAPIKey])
	require.Equal(t, "translate-key", repo.data["ai.translate.api_key"])
	require.Equal(t, "report-key", repo.data["ai.report.api_key"])
	require.Equal(t, "true", repo.data[service.KeyAIAutoAnalysis])
	require.Equal(t, 20, limiter.GetLimit())
	require.Equal(t, "6", repo.data[service.KeyAIWorkerCount])

	repo.data[service.KeyAIAPIKey] = "sk-existing"
	settings.Analysis.APIKey = "***"
	settings.RateLimit = 0
	settings.WorkerCount = 0
	err = svc.SetAISettings(context.Background(), settings)
	require.NoError(t, err)
	require.Equal(t, "sk-existing", repo.data[service.KeyAIAPIKey])
	require.Equal(t, ai.DefaultRateLimit, limiter.GetLimit())
	require.Equal(t, "2", repo.data[service.KeyAIWorkerCount])
}

func TestSettingsService_GetAIUsageStats(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	today := time.Now().In(time.Local).Format("2006-01-02")
	yesterday := time.Now().In(time.Local).AddDate(0, 0, -1).Format("2006-01-02")

	todayPayload, err := json.Marshal(map[string]any{
		"date": today,
		"totals": map[string]any{
			"requestCount":     3,
			"promptTokens":     120,
			"completionTokens": 80,
			"totalTokens":      200,
		},
		"scenes": map[string]any{
			"analysis": map[string]any{
				"requestCount":     2,
				"promptTokens":     90,
				"completionTokens": 50,
				"totalTokens":      140,
			},
			"translation": map[string]any{
				"requestCount":     1,
				"promptTokens":     30,
				"completionTokens": 30,
				"totalTokens":      60,
			},
		},
	})
	require.NoError(t, err)

	yesterdayPayload, err := json.Marshal(map[string]any{
		"date": yesterday,
		"totals": map[string]any{
			"requestCount":     1,
			"promptTokens":     40,
			"completionTokens": 20,
			"totalTokens":      60,
		},
		"scenes": map[string]any{
			"report": map[string]any{
				"requestCount":     1,
				"promptTokens":     40,
				"completionTokens": 20,
				"totalTokens":      60,
			},
		},
	})
	require.NoError(t, err)

	repo.data["stats.ai_usage.daily."+today] = string(todayPayload)
	repo.data["stats.ai_usage.daily."+yesterday] = string(yesterdayPayload)

	stats, err := svc.GetAIUsageStats(context.Background(), 30)
	require.NoError(t, err)
	require.Equal(t, 3, stats.Today.RequestCount)
	require.Equal(t, 200, stats.Today.TotalTokens)
	require.Equal(t, 4, stats.Last7Days.RequestCount)
	require.Equal(t, 260, stats.AllTime.TotalTokens)
	require.Len(t, stats.Daily, 2)
	require.Equal(t, today, stats.Daily[0].Date)
	require.Equal(t, "analysis", stats.Daily[0].Scenes[0].Scene)
}

func TestSettingsService_AIPromptSettings(t *testing.T) {
	repo := newSettingsRepoStub()
	dir := t.TempDir()
	manager := ai.NewPromptManager(dir)
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0), service.WithSettingsPromptManager(manager))

	settings, err := svc.GetAIPromptSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, dir, settings.Dir)
	require.NotEmpty(t, settings.Templates)

	err = svc.SetAIPromptSettings(context.Background(), &service.AIPromptSettings{
		Templates: []service.AIPromptTemplate{
			{
				Key:     "analysis",
				Content: "custom {{ .TargetLanguage }} {{ .ArticleTitle }}",
			},
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "analysis.tmpl"))
	require.NoError(t, err)
	require.Equal(t, "custom {{ .TargetLanguage }} {{ .ArticleTitle }}", string(data))
}

func TestSettingsService_GeneralSettings(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	err := svc.SetGeneralSettings(context.Background(), &service.GeneralSettings{
		FallbackUserAgent:    "UA-Test",
		AutoReadability:      true,
		AIDailyReportAPIKey:  "report-secret-123",
		AIAnalysisArchiveDir: "/tmp/gist-ai",
	})
	require.NoError(t, err)

	settings, err := svc.GetGeneralSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, "UA-Test", settings.FallbackUserAgent)
	require.True(t, settings.AutoReadability)
	require.NotEqual(t, "report-secret-123", settings.AIDailyReportAPIKey)
	require.NotEmpty(t, settings.AIDailyReportAPIKey)
	require.Equal(t, "/tmp/gist-ai", settings.AIAnalysisArchiveDir)

	ua := svc.GetFallbackUserAgent(context.Background())
	require.Equal(t, "UA-Test", ua)
	require.Equal(t, "report-secret-123", svc.GetAIDailyReportAccessKey(context.Background()))

	err = svc.SetGeneralSettings(context.Background(), &service.GeneralSettings{
		FallbackUserAgent:    "UA-Test",
		AutoReadability:      false,
		AIDailyReportAPIKey:  "",
		AIAnalysisArchiveDir: "",
	})
	require.NoError(t, err)
	require.Empty(t, svc.GetAIDailyReportAccessKey(context.Background()))
}

func TestSettingsService_ClearAnubisCookies(t *testing.T) {
	repo := newSettingsRepoStub()
	repo.data["anubis.cookie.example.com"] = "cookie"
	repo.data["anubis.cookie.test.com"] = "cookie"
	repo.data["other.key"] = "value"

	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	deleted, err := svc.ClearAnubisCookies(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	require.Contains(t, repo.data, "other.key")
}

func TestSettingsService_TestAI_InvalidConfig(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	_, err := svc.TestAI(context.Background(), ai.ProviderOpenAI, "", "", "", "responses", false, 0, "")
	require.Error(t, err)

	repo.data[service.KeyAIAPIKey] = ""
	_, err = svc.TestAI(context.Background(), ai.ProviderOpenAI, "***", "", "gpt-4", "responses", false, 0, "")
	require.ErrorIs(t, err, ai.ErrMissingAPIKey)
}

func TestMaskAPIKey(t *testing.T) {
	require.Empty(t, service.MaskAPIKey(""))
	require.Equal(t, "***", service.MaskAPIKey("short"))
	masked := service.MaskAPIKey("sk-test-1234567890")
	require.NotEqual(t, "sk-test-1234567890", masked)
	require.NotEmpty(t, masked)
	require.True(t, service.IsMaskedKey(masked))
}

func TestSettingsService_GetNetworkSettings_Defaults(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	settings, err := svc.GetNetworkSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, "http", settings.Type)
	require.Empty(t, settings.Host)
	require.Zero(t, settings.Port)
}

func TestSettingsService_AppearanceSettings_Defaults(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	settings, err := svc.GetAppearanceSettings(context.Background())
	require.NoError(t, err)
	require.Len(t, settings.ContentTypes, len(service.DefaultAppearanceContentTypes))
	require.Equal(t, service.DefaultAppearanceContentTypes[0], settings.ContentTypes[0])
}

func TestSettingsService_AppearanceSettings_Validate(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	err := svc.SetAppearanceSettings(context.Background(), &service.AppearanceSettings{ContentTypes: []string{}})
	require.Error(t, err)

	err = svc.SetAppearanceSettings(context.Background(), &service.AppearanceSettings{ContentTypes: []string{"picture", "picture", "invalid", "article"}})
	require.NoError(t, err)

	settings, err := svc.GetAppearanceSettings(context.Background())
	require.NoError(t, err)
	require.Len(t, settings.ContentTypes, 2)
	require.Equal(t, "picture", settings.ContentTypes[0])
	require.Equal(t, "article", settings.ContentTypes[1])
}

func TestSettingsService_GetNetworkSettings_StoredValues(t *testing.T) {
	repo := newSettingsRepoStub()
	repo.data[service.KeyNetworkEnabled] = "true"
	repo.data[service.KeyNetworkType] = "socks5"
	repo.data[service.KeyNetworkHost] = "127.0.0.1"
	repo.data[service.KeyNetworkPort] = "7890"
	repo.data[service.KeyNetworkUsername] = "user"
	repo.data[service.KeyNetworkPassword] = "secret123"

	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))
	settings, err := svc.GetNetworkSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.Equal(t, "socks5", settings.Type)
	require.Equal(t, "127.0.0.1", settings.Host)
	require.Equal(t, 7890, settings.Port)
	require.Equal(t, "user", settings.Username)
	require.NotEqual(t, "secret123", settings.Password)
	require.NotEmpty(t, settings.Password)
}

func TestSettingsService_SetNetworkSettings(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	settings := &service.NetworkSettings{
		Enabled:  true,
		Type:     "socks5",
		Host:     "proxy.example.com",
		Port:     1080,
		Username: "admin",
		Password: "password123",
	}

	err := svc.SetNetworkSettings(context.Background(), settings)
	require.NoError(t, err)
	require.Equal(t, "true", repo.data[service.KeyNetworkEnabled])
	require.Equal(t, "socks5", repo.data[service.KeyNetworkType])
	require.Equal(t, "proxy.example.com", repo.data[service.KeyNetworkHost])
	require.Equal(t, "1080", repo.data[service.KeyNetworkPort])
	require.Equal(t, "password123", repo.data[service.KeyNetworkPassword])
}

func TestSettingsService_GetIPStack(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	require.Equal(t, "default", svc.GetIPStack(context.Background()))

	repo.data[service.KeyNetworkIPStack] = "ipv6"
	require.Equal(t, "ipv6", svc.GetIPStack(context.Background()))
}

func TestSettingsService_SetNetworkSettings_MaskedPassword(t *testing.T) {
	repo := newSettingsRepoStub()
	repo.data[service.KeyNetworkPassword] = "existing-password"

	svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))

	settings := &service.NetworkSettings{
		Enabled:  true,
		Type:     "http",
		Host:     "proxy.example.com",
		Port:     8080,
		Password: "***", // masked password
	}

	err := svc.SetNetworkSettings(context.Background(), settings)
	require.NoError(t, err)
	require.Equal(t, "existing-password", repo.data[service.KeyNetworkPassword])
}

func TestSettingsService_GetProxyURL(t *testing.T) {
	tests := []struct {
		name      string
		enabled   string
		proxyType string
		host      string
		port      string
		username  string
		password  string
		expected  string
	}{
		{
			name:     "disabled proxy",
			enabled:  "false",
			expected: "",
		},
		{
			name:     "empty host",
			enabled:  "true",
			host:     "",
			expected: "",
		},
		{
			name:      "http proxy without auth",
			enabled:   "true",
			proxyType: "http",
			host:      "127.0.0.1",
			port:      "8080",
			expected:  "http://127.0.0.1:8080",
		},
		{
			name:      "socks5 proxy without auth",
			enabled:   "true",
			proxyType: "socks5",
			host:      "localhost",
			port:      "1080",
			expected:  "socks5://localhost:1080",
		},
		{
			name:      "http proxy with auth",
			enabled:   "true",
			proxyType: "http",
			host:      "proxy.example.com",
			port:      "3128",
			username:  "user",
			password:  "pass",
			expected:  "http://user:pass@proxy.example.com:3128",
		},
		{
			name:      "socks5 proxy with username only",
			enabled:   "true",
			proxyType: "socks5",
			host:      "socks.example.com",
			port:      "1080",
			username:  "user",
			expected:  "socks5://user@socks.example.com:1080",
		},
		{
			name:      "default type is http",
			enabled:   "true",
			proxyType: "",
			host:      "localhost",
			port:      "8080",
			expected:  "http://localhost:8080",
		},
		{
			name:      "port 0 returns empty",
			enabled:   "true",
			proxyType: "http",
			host:      "localhost",
			port:      "",
			expected:  "", // port <= 0 is invalid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newSettingsRepoStub()
			if tt.enabled != "" {
				repo.data[service.KeyNetworkEnabled] = tt.enabled
			}
			if tt.proxyType != "" {
				repo.data[service.KeyNetworkType] = tt.proxyType
			}
			if tt.host != "" {
				repo.data[service.KeyNetworkHost] = tt.host
			}
			if tt.port != "" {
				repo.data[service.KeyNetworkPort] = tt.port
			}
			if tt.username != "" {
				repo.data[service.KeyNetworkUsername] = tt.username
			}
			if tt.password != "" {
				repo.data[service.KeyNetworkPassword] = tt.password
			}

			svc := service.NewSettingsService(repo, ai.NewRateLimiter(0))
			result := svc.GetProxyURL(context.Background())
			require.Equal(t, tt.expected, result)
		})
	}
}
