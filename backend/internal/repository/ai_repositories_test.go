package repository_test

import (
	"context"
	"gist/backend/internal/repository"
	"testing"
	"time"

	"gist/backend/internal/model"
	"gist/backend/internal/repository/testutil"

	"github.com/stretchr/testify/require"
)

func TestAISummaryRepository(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewAISummaryRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "F", URL: "u"})
	entryID := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID})

	// Save
	err := repo.Save(ctx, entryID, false, "zh-CN", "summary content")
	require.NoError(t, err)

	// Get
	summary, err := repo.Get(ctx, entryID, false, "zh-CN")
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, "summary content", summary.Summary)

	// Update (Conflict)
	err = repo.Save(ctx, entryID, false, "zh-CN", "updated summary")
	require.NoError(t, err)
	summary, _ = repo.Get(ctx, entryID, false, "zh-CN")
	require.Equal(t, "updated summary", summary.Summary)

	// Delete All
	count, err := repo.DeleteAll(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	// Delete By Entry
	err = repo.DeleteByEntryID(ctx, entryID)
	require.NoError(t, err)
}

func TestAITranslationRepository(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewAITranslationRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "F", URL: "u"})
	entryID := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID})

	// Save
	err := repo.Save(ctx, entryID, false, "en-US", "translated html")
	require.NoError(t, err)

	// Get
	trans, err := repo.Get(ctx, entryID, false, "en-US")
	require.NoError(t, err)
	require.NotNil(t, trans)
	require.Equal(t, "translated html", trans.Content)

	// Delete By Entry
	err = repo.DeleteByEntryID(ctx, entryID)
	require.NoError(t, err)
	trans, _ = repo.Get(ctx, entryID, false, "en-US")
	require.Nil(t, trans)

	// Delete All
	count, err := repo.DeleteAll(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)
}

func TestAIListTranslationRepository(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewAIListTranslationRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "F", URL: "u"})
	entryID := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID})

	// Save
	err := repo.Save(ctx, entryID, "en-US", "Trans Title", "Trans Summary")
	require.NoError(t, err)

	// Get
	trans, err := repo.Get(ctx, entryID, "en-US")
	require.NoError(t, err)
	require.NotNil(t, trans)
	require.Equal(t, "Trans Title", trans.Title)

	// Delete All
	count, err := repo.DeleteAll(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func TestAIListTranslationRepository_GetBatchAndDelete(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewAIListTranslationRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "F", URL: "u"})
	entryID1 := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID})
	entryID2 := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID})

	err := repo.Save(ctx, entryID1, "en-US", "Title 1", "Summary 1")
	require.NoError(t, err)
	err = repo.Save(ctx, entryID2, "en-US", "Title 2", "Summary 2")
	require.NoError(t, err)

	batch, err := repo.GetBatch(ctx, []int64{entryID1, entryID2}, "en-US")
	require.NoError(t, err)
	require.Len(t, batch, 2)

	err = repo.DeleteByEntryID(ctx, entryID1)
	require.NoError(t, err)
	remaining, err := repo.GetBatch(ctx, []int64{entryID1, entryID2}, "en-US")
	require.NoError(t, err)
	require.Len(t, remaining, 1)
}

func TestAIAnalysisRepository(t *testing.T) {
	db := testutil.NewTestDB(t)
	repo := repository.NewAIAnalysisRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "F", URL: "u"})
	entryID := testutil.SeedEntry(t, db, model.Entry{FeedID: feedID})

	lat := 35.6895
	lng := 139.6917
	err := repo.Save(ctx, entryID, false, "zh-CN", model.AIAnalysis{
		Tag:        "#东亚/2026/日本/政策调整/美国",
		Summary:    "测试摘要",
		Entities:   []string{"日本", "美国"},
		Sentiment:  "neutral",
		Importance: 7,
		Latitude:   &lat,
		Longitude:  &lng,
	})
	require.NoError(t, err)

	analysis, err := repo.Get(ctx, entryID, false, "zh-CN")
	require.NoError(t, err)
	require.NotNil(t, analysis)
	require.Equal(t, "#东亚/2026/日本/政策调整/美国", analysis.Tag)
	require.Equal(t, []string{"日本", "美国"}, analysis.Entities)
	require.Equal(t, "neutral", analysis.Sentiment)
	require.Equal(t, 7, analysis.Importance)
	require.NotNil(t, analysis.Latitude)
	require.NotNil(t, analysis.Longitude)

	count, err := repo.DeleteAll(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func TestAIAnalysisRepository_ListPrefersChineseTranslatedTitle(t *testing.T) {
	db := testutil.NewTestDB(t)
	analysisRepo := repository.NewAIAnalysisRepository(db)
	listRepo := repository.NewAIListTranslationRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "F", URL: "u"})
	originalTitle := "Markets rally on Fed signals"
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID: feedID,
		Title:  &originalTitle,
	})

	err := analysisRepo.Save(ctx, entryID, false, "en-US", model.AIAnalysis{
		Tag:        "#全球/美国/美联储",
		Summary:    "summary",
		Entities:   []string{"Fed"},
		Sentiment:  "positive",
		Importance: 8,
	})
	require.NoError(t, err)

	translatedTitle := "美联储信号带动市场上涨"
	err = listRepo.Save(ctx, entryID, "zh-CN", translatedTitle, "")
	require.NoError(t, err)

	items, err := analysisRepo.List(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].EntryTitle)
	require.Equal(t, translatedTitle, *items[0].EntryTitle)
}

func TestAIAnalysisRepository_ListByCreatedRangePrefersChineseTranslatedTitle(t *testing.T) {
	db := testutil.NewTestDB(t)
	analysisRepo := repository.NewAIAnalysisRepository(db)
	listRepo := repository.NewAIListTranslationRepository(db)
	ctx := context.Background()

	feedID := testutil.SeedFeed(t, db, model.Feed{Title: "F", URL: "u"})
	originalTitle := "Chipmakers expand in Asia"
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID: feedID,
		Title:  &originalTitle,
	})

	err := analysisRepo.Save(ctx, entryID, false, "en-US", model.AIAnalysis{
		Tag:        "#亚洲/芯片/扩产",
		Summary:    "summary",
		Entities:   []string{"Asia"},
		Sentiment:  "neutral",
		Importance: 6,
	})
	require.NoError(t, err)

	translatedTitle := "芯片厂商在亚洲扩产"
	err = listRepo.Save(ctx, entryID, "zh-CN", translatedTitle, "")
	require.NoError(t, err)

	end := time.Now().Add(time.Minute)
	start := end.Add(-time.Hour)
	items, err := analysisRepo.ListByCreatedRange(ctx, start, end)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].EntryTitle)
	require.Equal(t, translatedTitle, *items[0].EntryTitle)
}
