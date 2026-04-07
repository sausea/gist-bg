package service_test

import (
	"testing"

	"gist/backend/internal/model"
	repomock "gist/backend/internal/repository/mock"
	"gist/backend/internal/service"
	servicemock "gist/backend/internal/service/mock"

	"go.uber.org/mock/gomock"
)

func TestAIStreamProcessor_ProcessSummaryAndAnalysis(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	entryRepo := repomock.NewMockEntryRepository(ctrl)
	settingsRepo := repomock.NewMockSettingsRepository(ctrl)
	aiService := servicemock.NewMockAIService(ctrl)

	entry := model.Entry{
		ID:      42,
		Title:   service.OptionalString("Test title"),
		Content: service.OptionalString("<p>Test content</p>"),
	}

	entryRepo.EXPECT().GetByID(gomock.Any(), int64(42)).Return(entry, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoSummary).Return(&model.Setting{Key: service.KeyAIAutoSummary, Value: "true"}, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoAnalysis).Return(&model.Setting{Key: service.KeyAIAutoAnalysis, Value: "true"}, nil)
	aiService.EXPECT().GetCachedSummary(gomock.Any(), int64(42), false).Return(nil, nil)

	textCh := make(chan string, 1)
	textCh <- "streamed summary"
	close(textCh)
	errCh := make(chan error)
	close(errCh)

	aiService.EXPECT().Summarize(gomock.Any(), int64(42), "<p>Test content</p>", "Test title", false).Return(textCh, errCh, nil)
	aiService.EXPECT().SaveSummary(gomock.Any(), int64(42), false, "streamed summary").Return(nil)
	aiService.EXPECT().GetCachedAnalysis(gomock.Any(), int64(42), false).Return(nil, nil)
	aiService.EXPECT().Analyze(gomock.Any(), int64(42), "<p>Test content</p>", "Test title", false).Return(&model.AIAnalysis{}, nil)

	processor := service.NewAIStreamProcessor(entryRepo, settingsRepo, aiService, 1, 4)
	processor.Enqueue(42)
	processor.Close()
}

func TestAIStreamProcessor_SkipsWhenDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	entryRepo := repomock.NewMockEntryRepository(ctrl)
	settingsRepo := repomock.NewMockSettingsRepository(ctrl)
	aiService := servicemock.NewMockAIService(ctrl)

	entry := model.Entry{
		ID:      7,
		Content: service.OptionalString("<p>Disabled</p>"),
	}

	entryRepo.EXPECT().GetByID(gomock.Any(), int64(7)).Return(entry, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoSummary).Return(&model.Setting{Key: service.KeyAIAutoSummary, Value: "false"}, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoAnalysis).Return(&model.Setting{Key: service.KeyAIAutoAnalysis, Value: "false"}, nil)

	processor := service.NewAIStreamProcessor(entryRepo, settingsRepo, aiService, 1, 4)
	processor.Enqueue(7)
	processor.Close()
}

func TestAIStreamProcessor_CloseIsIdempotent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	entryRepo := repomock.NewMockEntryRepository(ctrl)
	settingsRepo := repomock.NewMockSettingsRepository(ctrl)
	aiService := servicemock.NewMockAIService(ctrl)

	processor := service.NewAIStreamProcessor(entryRepo, settingsRepo, aiService, 1, 1)
	processor.Close()
	processor.Close()
}

func TestAIStreamProcessor_EnsureQueued_SkipsWhenCached(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	entryRepo := repomock.NewMockEntryRepository(ctrl)
	settingsRepo := repomock.NewMockSettingsRepository(ctrl)
	aiService := servicemock.NewMockAIService(ctrl)

	entry := model.Entry{
		ID:      9,
		Content: service.OptionalString("<p>Cached</p>"),
	}

	entryRepo.EXPECT().GetByID(gomock.Any(), int64(9)).Return(entry, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoSummary).Return(&model.Setting{Key: service.KeyAIAutoSummary, Value: "true"}, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoAnalysis).Return(&model.Setting{Key: service.KeyAIAutoAnalysis, Value: "false"}, nil)
	aiService.EXPECT().GetCachedSummary(gomock.Any(), int64(9), false).Return(&model.AISummary{Summary: "ready"}, nil)

	processor := service.NewAIStreamProcessor(entryRepo, settingsRepo, aiService, 1, 4)
	status := processor.EnsureQueued(9)
	processor.Close()

	if status.Processing {
		t.Fatalf("expected cached entry not to be queued")
	}
}

func TestAIStreamProcessor_EnsureQueued_QueuesWhenMissingCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	entryRepo := repomock.NewMockEntryRepository(ctrl)
	settingsRepo := repomock.NewMockSettingsRepository(ctrl)
	aiService := servicemock.NewMockAIService(ctrl)

	entry := model.Entry{
		ID:      42,
		Title:   service.OptionalString("Queued title"),
		Content: service.OptionalString("<p>Queued content</p>"),
	}

	entryRepo.EXPECT().GetByID(gomock.Any(), int64(42)).Return(entry, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoSummary).Return(&model.Setting{Key: service.KeyAIAutoSummary, Value: "true"}, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoAnalysis).Return(&model.Setting{Key: service.KeyAIAutoAnalysis, Value: "false"}, nil)
	aiService.EXPECT().GetCachedSummary(gomock.Any(), int64(42), false).Return(nil, nil)

	entryRepo.EXPECT().GetByID(gomock.Any(), int64(42)).Return(entry, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoSummary).Return(&model.Setting{Key: service.KeyAIAutoSummary, Value: "true"}, nil)
	settingsRepo.EXPECT().Get(gomock.Any(), service.KeyAIAutoAnalysis).Return(&model.Setting{Key: service.KeyAIAutoAnalysis, Value: "false"}, nil)
	aiService.EXPECT().GetCachedSummary(gomock.Any(), int64(42), false).Return(nil, nil)

	textCh := make(chan string)
	errCh := make(chan error)
	aiService.EXPECT().Summarize(gomock.Any(), int64(42), "<p>Queued content</p>", "Queued title", false).Return(textCh, errCh, nil)

	processor := service.NewAIStreamProcessor(entryRepo, settingsRepo, aiService, 1, 4)
	status := processor.EnsureQueued(42)
	if !status.Processing {
		t.Fatalf("expected missing cache entry to be queued")
	}

	close(textCh)
	close(errCh)
	processor.Close()
}
