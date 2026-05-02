package repository_test

import (
	"context"
	"testing"
	"time"

	"gist/backend/internal/model"
	"gist/backend/internal/repository"
	"gist/backend/internal/repository/testutil"

	"github.com/stretchr/testify/require"
)

func TestAIAnalysisJobRepository_UpsertAndTransitions(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewAIAnalysisJobRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID:  feedID,
		Title:   stringPtr("Entry"),
		Content: stringPtr("<p>Body</p>"),
	})

	require.NoError(t, repo.UpsertQueued(ctx, entryID, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))

	job, err := repo.GetByEntryID(ctx, entryID)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, model.AIAnalysisJobStatusQueued, job.Status)
	require.Equal(t, model.AIAnalysisJobSourceAuto, job.Source)
	require.Equal(t, "zh-CN", job.Language)

	require.NoError(t, repo.MarkRunning(ctx, entryID))
	job, err = repo.GetByEntryID(ctx, entryID)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, model.AIAnalysisJobStatusRunning, job.Status)
	require.Equal(t, 1, job.RetryCount)
	require.NotNil(t, job.StartedAt)

	require.NoError(t, repo.MarkFailed(ctx, entryID, "network error"))
	job, err = repo.GetByEntryID(ctx, entryID)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, model.AIAnalysisJobStatusFailed, job.Status)
	require.NotNil(t, job.ErrorMessage)
	require.Equal(t, "network error", *job.ErrorMessage)

	require.NoError(t, repo.UpsertQueued(ctx, entryID, feedID, model.AIAnalysisJobSourceManual, model.AIAnalysisContentModeReadability, "zh-CN"))
	require.NoError(t, repo.MarkRunning(ctx, entryID))
	require.NoError(t, repo.MarkSucceeded(ctx, entryID, model.AIAnalysisContentModeReadability, "zh-CN"))

	job, err = repo.GetByEntryID(ctx, entryID)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, model.AIAnalysisJobStatusSucceeded, job.Status)
	require.Equal(t, model.AIAnalysisJobSourceManual, job.Source)
	require.Equal(t, model.AIAnalysisContentModeReadability, job.ContentMode)
	require.Equal(t, 2, job.RetryCount)
	require.NotNil(t, job.FinishedAt)
	require.Nil(t, job.ErrorMessage)
}

func TestAIAnalysisJobRepository_ListAndQueueStats(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewAIAnalysisJobRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryQueued := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID, Title: stringPtr("Queued"), Content: stringPtr("<p>1</p>")})
	entryRunning := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID, Title: stringPtr("Running"), Content: stringPtr("<p>2</p>")})
	entrySucceeded := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID, Title: stringPtr("Done"), Content: stringPtr("<p>3</p>")})
	entryFailed := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID, Title: stringPtr("Failed"), Content: stringPtr("<p>4</p>")})

	require.NoError(t, repo.UpsertQueued(ctx, entryQueued, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, repo.UpsertQueued(ctx, entryRunning, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, repo.MarkRunning(ctx, entryRunning))
	require.NoError(t, repo.UpsertQueued(ctx, entrySucceeded, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, repo.MarkRunning(ctx, entrySucceeded))
	require.NoError(t, repo.MarkSucceeded(ctx, entrySucceeded, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, repo.UpsertQueued(ctx, entryFailed, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, repo.MarkRunning(ctx, entryFailed))
	require.NoError(t, repo.MarkFailed(ctx, entryFailed, "network error"))

	dayStart, dayEnd := currentLocalDayBounds()
	queuedIDs, err := repo.ListQueuedEntryIDs(ctx, dayStart, dayEnd, true, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{entryQueued}, queuedIDs)

	stats, err := repo.GetQueueStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, stats.QueuedCount)
	require.Equal(t, 1, stats.RunningCount)
	require.Equal(t, 1, stats.FailedCount)

	affected, err := repo.RequeueRunning(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)

	stats, err = repo.GetQueueStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, stats.QueuedCount)
	require.Equal(t, 0, stats.RunningCount)
	require.Equal(t, 1, stats.FailedCount)
}

func TestAIAnalysisJobRepository_ListQueuedEntryIDs_CurrentDayPriority(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewAIAnalysisJobRepository(db)
	ctx := context.Background()

	dayStart, dayEnd := currentLocalDayBounds()
	todayTime := dayStart.Add(10 * time.Hour)
	yesterdayTime := dayStart.Add(-2 * time.Hour)

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryManualOld := testutil.SeedEntry(t, db, model.Entry{
		FeedID:      feedID,
		Title:       stringPtr("Manual old"),
		Content:     stringPtr("<p>manual</p>"),
		PublishedAt: &yesterdayTime,
	})
	entryToday := testutil.SeedEntry(t, db, model.Entry{
		FeedID:      feedID,
		Title:       stringPtr("Today"),
		Content:     stringPtr("<p>today</p>"),
		PublishedAt: &todayTime,
	})
	entryAutoOld := testutil.SeedEntry(t, db, model.Entry{
		FeedID:      feedID,
		Title:       stringPtr("Auto old"),
		Content:     stringPtr("<p>old</p>"),
		PublishedAt: &yesterdayTime,
	})

	require.NoError(t, repo.UpsertQueued(ctx, entryAutoOld, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, repo.UpsertQueued(ctx, entryToday, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, repo.UpsertQueued(ctx, entryManualOld, feedID, model.AIAnalysisJobSourceManual, model.AIAnalysisContentModeOriginal, "zh-CN"))

	ids, err := repo.ListQueuedEntryIDs(ctx, dayStart, dayEnd, false, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{entryManualOld, entryToday}, ids)

	ids, err = repo.ListQueuedEntryIDs(ctx, dayStart, dayEnd, true, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{entryManualOld, entryToday, entryAutoOld}, ids)

	hasPending, err := repo.HasPendingForDay(ctx, dayStart, dayEnd)
	require.NoError(t, err)
	require.True(t, hasPending)
}

func TestAIAnalysisJobRepository_ListQueue(t *testing.T) {
	db := testutil.NewTestDB(t)
	jobRepo := repository.NewAIAnalysisJobRepository(db)
	listRepo := repository.NewAIListTranslationRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	queuedTitle := "Queued title"
	runningTitle := "Running title"
	failedTitle := "Failed title"
	entryQueued := testutil.SeedEntry(t, db, model.Entry{
		FeedID:  feedID,
		Title:   &queuedTitle,
		URL:     stringPtr("https://example.com/queued"),
		Content: stringPtr("<p>queued</p>"),
	})
	entryRunning := testutil.SeedEntry(t, db, model.Entry{
		FeedID:  feedID,
		Title:   &runningTitle,
		Content: stringPtr("<p>running</p>"),
	})
	entryFailed := testutil.SeedEntry(t, db, model.Entry{
		FeedID:  feedID,
		Title:   &failedTitle,
		Content: stringPtr("<p>failed</p>"),
	})
	entrySucceeded := testutil.SeedEntry(t, db, model.Entry{
		FeedID:  feedID,
		Title:   stringPtr("Succeeded"),
		Content: stringPtr("<p>succeeded</p>"),
	})

	require.NoError(t, jobRepo.UpsertQueued(ctx, entryQueued, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, jobRepo.UpsertQueued(ctx, entryRunning, feedID, model.AIAnalysisJobSourceManual, model.AIAnalysisContentModeReadability, "zh-CN"))
	require.NoError(t, jobRepo.MarkRunning(ctx, entryRunning))
	require.NoError(t, jobRepo.UpsertQueued(ctx, entryFailed, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, jobRepo.MarkRunning(ctx, entryFailed))
	require.NoError(t, jobRepo.MarkFailed(ctx, entryFailed, "fetch failed"))
	require.NoError(t, jobRepo.UpsertQueued(ctx, entrySucceeded, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, jobRepo.MarkRunning(ctx, entrySucceeded))
	require.NoError(t, jobRepo.MarkSucceeded(ctx, entrySucceeded, model.AIAnalysisContentModeOriginal, "zh-CN"))

	translatedTitle := "失败文章中文标题"
	require.NoError(t, listRepo.Save(ctx, entryFailed, "zh-CN", translatedTitle, ""))

	items, err := jobRepo.ListQueue(ctx, 10)
	require.NoError(t, err)
	require.Len(t, items, 3)
	require.Equal(t, model.AIAnalysisJobStatusRunning, items[0].Status)
	require.Equal(t, entryRunning, items[0].EntryID)
	require.Equal(t, model.AIAnalysisContentModeReadability, items[0].ContentMode)
	require.Equal(t, model.AIAnalysisJobStatusQueued, items[1].Status)
	require.Equal(t, entryQueued, items[1].EntryID)
	require.Equal(t, model.AIAnalysisJobStatusFailed, items[2].Status)
	require.Equal(t, entryFailed, items[2].EntryID)
	require.NotNil(t, items[2].EntryTitle)
	require.Equal(t, translatedTitle, *items[2].EntryTitle)
	require.NotNil(t, items[2].ErrorMessage)
	require.Equal(t, "fetch failed", *items[2].ErrorMessage)
}

func TestAIAnalysisJobRepository_ListBackfillEntryIDs(t *testing.T) {
	db := testutil.NewTestDB(t)
	jobRepo := repository.NewAIAnalysisJobRepository(db)
	analysisRepo := repository.NewAIAnalysisRepository(db)
	ctx := context.Background()
	dayStart, dayEnd := currentLocalDayBounds()
	todayTime := dayStart.Add(9 * time.Hour)
	yesterdayTime := dayStart.Add(-3 * time.Hour)

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "Feed", URL: "https://example.com/feed"})
	entryBackfill := testutil.SeedEntry(t, db, model.Entry{
		FeedID:      feedID,
		Title:       stringPtr("Backfill"),
		Content:     stringPtr("<p>1</p>"),
		PublishedAt: &todayTime,
	})
	entryQueued := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID, Title: stringPtr("Queued"), Content: stringPtr("<p>2</p>")})
	entryDone := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID, Title: stringPtr("Done"), Content: stringPtr("<p>3</p>")})
	_ = testutil.SeedEntry(t, db, model.Entry{
		FeedID:      feedID,
		Title:       stringPtr("Old article"),
		Content:     stringPtr("<p>old</p>"),
		PublishedAt: &yesterdayTime,
	})
	_ = testutil.SeedEntry(t, db, model.Entry{FeedID: feedID, Title: stringPtr("Empty"), Content: stringPtr("")})

	require.NoError(t, jobRepo.UpsertQueued(ctx, entryQueued, feedID, model.AIAnalysisJobSourceAuto, model.AIAnalysisContentModeOriginal, "zh-CN"))
	require.NoError(t, analysisRepo.Save(ctx, entryDone, false, "zh-CN", model.AIAnalysis{
		Tag: "#Tag", Summary: "done", Entities: []string{"A"}, Sentiment: "neutral", Importance: 1,
	}))

	ids, err := jobRepo.ListBackfillEntryIDs(ctx, dayStart, dayEnd, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{entryBackfill}, ids)
}

func currentLocalDayBounds() (time.Time, time.Time) {
	now := time.Now().In(time.Local)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	return dayStart, dayStart.Add(24 * time.Hour)
}
