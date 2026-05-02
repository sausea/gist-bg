package service_test

import (
	"context"
	"testing"
	"time"

	"gist/backend/internal/model"
	"gist/backend/internal/repository"
	"gist/backend/internal/repository/testutil"
	"gist/backend/internal/service"
	servicemock "gist/backend/internal/service/mock"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestAIStreamProcessor_ProcessAnalysis(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	db := testutil.NewTestDB(t)
	entryRepo := repository.NewEntryRepository(db)
	jobRepo := repository.NewAIAnalysisJobRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	aiService := servicemock.NewMockAIService(ctrl)

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID:  feedID,
		Title:   service.OptionalString("Test title"),
		Content: service.OptionalString("<p>Test content</p>"),
	})
	testutil.SeedSetting(t, db, service.KeyAIAutoAnalysis, "true")

	aiService.EXPECT().GetSummaryLanguage(gomock.Any()).Return("zh-CN").AnyTimes()
	cached := false
	aiService.EXPECT().GetCachedAnalysis(gomock.Any(), entryID, false).DoAndReturn(func(context.Context, int64, bool) (*model.AIAnalysis, error) {
		if cached {
			return &model.AIAnalysis{Summary: "ready"}, nil
		}
		return nil, nil
	}).AnyTimes()
	aiService.EXPECT().Analyze(gomock.Any(), entryID, "<p>Test content</p>", "Test title", false).DoAndReturn(func(context.Context, int64, string, string, bool) (*model.AIAnalysis, error) {
		cached = true
		return &model.AIAnalysis{}, nil
	}).Times(1)

	processor := service.NewAIStreamProcessor(entryRepo, jobRepo, settingsRepo, aiService, 1, 4)
	processor.Enqueue(entryID)

	job := waitForJobStatus(t, jobRepo, entryID, model.AIAnalysisJobStatusSucceeded)
	processor.Close()

	require.Equal(t, model.AIAnalysisJobStatusSucceeded, job.Status)
	require.Equal(t, 1, job.RetryCount)
	require.Equal(t, model.AIAnalysisContentModeOriginal, job.ContentMode)
	require.Equal(t, "zh-CN", job.Language)
}

func TestAIStreamProcessor_SkipsWhenDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	db := testutil.NewTestDB(t)
	entryRepo := repository.NewEntryRepository(db)
	jobRepo := repository.NewAIAnalysisJobRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	aiService := servicemock.NewMockAIService(ctrl)

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID:  feedID,
		Content: service.OptionalString("<p>Disabled</p>"),
	})
	testutil.SeedSetting(t, db, service.KeyAIAutoSummary, "false")
	testutil.SeedSetting(t, db, service.KeyAIAutoAnalysis, "false")

	processor := service.NewAIStreamProcessor(entryRepo, jobRepo, settingsRepo, aiService, 1, 4)
	processor.Enqueue(entryID)
	processor.Close()

	job, err := jobRepo.GetByEntryID(context.Background(), entryID)
	require.NoError(t, err)
	require.Nil(t, job)
}

func TestAIStreamProcessor_CloseIsIdempotent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	db := testutil.NewTestDB(t)
	entryRepo := repository.NewEntryRepository(db)
	jobRepo := repository.NewAIAnalysisJobRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	aiService := servicemock.NewMockAIService(ctrl)

	processor := service.NewAIStreamProcessor(entryRepo, jobRepo, settingsRepo, aiService, 1, 1)
	processor.Close()
	processor.Close()
}

func TestAIStreamProcessor_EnsureQueued_SkipsWhenCached(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	db := testutil.NewTestDB(t)
	entryRepo := repository.NewEntryRepository(db)
	jobRepo := repository.NewAIAnalysisJobRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	aiService := servicemock.NewMockAIService(ctrl)

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID:  feedID,
		Content: service.OptionalString("<p>Cached</p>"),
	})
	testutil.SeedSetting(t, db, service.KeyAIAutoSummary, "true")
	testutil.SeedSetting(t, db, service.KeyAIAutoAnalysis, "false")

	aiService.EXPECT().GetSummaryLanguage(gomock.Any()).Return("zh-CN").AnyTimes()
	aiService.EXPECT().GetCachedAnalysis(gomock.Any(), entryID, false).Return(&model.AIAnalysis{Summary: "ready"}, nil).AnyTimes()

	processor := service.NewAIStreamProcessor(entryRepo, jobRepo, settingsRepo, aiService, 1, 4)
	status := processor.EnsureQueued(entryID)
	processor.Close()

	require.False(t, status.Processing)
}

func TestAIStreamProcessor_EnsureQueued_QueuesWhenMissingCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	db := testutil.NewTestDB(t)
	entryRepo := repository.NewEntryRepository(db)
	jobRepo := repository.NewAIAnalysisJobRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	aiService := servicemock.NewMockAIService(ctrl)

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID:  feedID,
		Title:   service.OptionalString("Queued title"),
		Content: service.OptionalString("<p>Queued content</p>"),
	})
	testutil.SeedSetting(t, db, service.KeyAIAutoSummary, "true")
	testutil.SeedSetting(t, db, service.KeyAIAutoAnalysis, "false")

	aiService.EXPECT().GetSummaryLanguage(gomock.Any()).Return("zh-CN").AnyTimes()
	cached := false
	aiService.EXPECT().GetCachedAnalysis(gomock.Any(), entryID, false).DoAndReturn(func(context.Context, int64, bool) (*model.AIAnalysis, error) {
		if cached {
			return &model.AIAnalysis{Summary: "ready"}, nil
		}
		return nil, nil
	}).AnyTimes()
	aiService.EXPECT().Analyze(gomock.Any(), entryID, "<p>Queued content</p>", "Queued title", false).DoAndReturn(func(context.Context, int64, string, string, bool) (*model.AIAnalysis, error) {
		cached = true
		return &model.AIAnalysis{}, nil
	}).Times(1)

	processor := service.NewAIStreamProcessor(entryRepo, jobRepo, settingsRepo, aiService, 1, 4)
	status := processor.EnsureQueued(entryID)
	require.True(t, status.Processing)

	waitForJobStatus(t, jobRepo, entryID, model.AIAnalysisJobStatusSucceeded)
	processor.Close()
}

func TestAIStreamProcessor_Enqueue_SkipsHistoricalAutoEntry(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	db := testutil.NewTestDB(t)
	entryRepo := repository.NewEntryRepository(db)
	jobRepo := repository.NewAIAnalysisJobRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	aiService := servicemock.NewMockAIService(ctrl)

	dayStart, _ := currentLocalDayBoundsForTest()
	yesterday := dayStart.Add(-2 * time.Hour)

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID:      feedID,
		Title:       service.OptionalString("Historical"),
		Content:     service.OptionalString("<p>Historical content</p>"),
		PublishedAt: &yesterday,
	})
	testutil.SeedSetting(t, db, service.KeyAIAutoAnalysis, "true")

	processor := service.NewAIStreamProcessor(entryRepo, jobRepo, settingsRepo, aiService, 1, 4)
	processor.Enqueue(entryID)
	processor.Close()

	job, err := jobRepo.GetByEntryID(context.Background(), entryID)
	require.NoError(t, err)
	require.Nil(t, job)
}

func TestAIStreamProcessor_EnsureQueued_AllowsHistoricalManualEntry(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	db := testutil.NewTestDB(t)
	entryRepo := repository.NewEntryRepository(db)
	jobRepo := repository.NewAIAnalysisJobRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	aiService := servicemock.NewMockAIService(ctrl)

	dayStart, _ := currentLocalDayBoundsForTest()
	yesterday := dayStart.Add(-2 * time.Hour)

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID:      feedID,
		Title:       service.OptionalString("Manual historical"),
		Content:     service.OptionalString("<p>Historical content</p>"),
		PublishedAt: &yesterday,
	})
	testutil.SeedSetting(t, db, service.KeyAIAutoSummary, "true")

	aiService.EXPECT().GetSummaryLanguage(gomock.Any()).Return("zh-CN").AnyTimes()
	cached := false
	aiService.EXPECT().GetCachedAnalysis(gomock.Any(), entryID, false).DoAndReturn(func(context.Context, int64, bool) (*model.AIAnalysis, error) {
		if cached {
			return &model.AIAnalysis{Summary: "ready"}, nil
		}
		return nil, nil
	}).AnyTimes()
	aiService.EXPECT().Analyze(gomock.Any(), entryID, "<p>Historical content</p>", "Manual historical", false).DoAndReturn(func(context.Context, int64, string, string, bool) (*model.AIAnalysis, error) {
		cached = true
		return &model.AIAnalysis{}, nil
	}).Times(1)

	processor := service.NewAIStreamProcessor(entryRepo, jobRepo, settingsRepo, aiService, 1, 4)
	status := processor.EnsureQueued(entryID)
	require.True(t, status.Processing)

	job := waitForJobStatus(t, jobRepo, entryID, model.AIAnalysisJobStatusSucceeded)
	processor.Close()

	require.Equal(t, model.AIAnalysisJobSourceManual, job.Source)
}

func waitForJobStatus(t *testing.T, repo repository.AIAnalysisJobRepository, entryID int64, status string) *model.AIAnalysisJob {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByEntryID(context.Background(), entryID)
		require.NoError(t, err)
		if job != nil && job.Status == status {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}

	job, err := repo.GetByEntryID(context.Background(), entryID)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, status, job.Status)
	return job
}

func currentLocalDayBoundsForTest() (time.Time, time.Time) {
	now := time.Now().In(time.Local)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	return dayStart, dayStart.Add(24 * time.Hour)
}
