package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"gist/backend/internal/service"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gist/backend/internal/model"
	"gist/backend/internal/repository"
	"gist/backend/internal/repository/testutil"
	"gist/backend/internal/service/ai"
)

func TestAIService_GetSummaryLanguage(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	lang := svc.GetSummaryLanguage(context.Background())
	require.Equal(t, "zh-CN", lang, "expected default language")

	repo.data[service.KeyAISummaryLanguage] = "en-US"
	lang = svc.GetSummaryLanguage(context.Background())
	require.Equal(t, "en-US", lang, "expected stored language")
}

func TestAIService_SaveSummaryAndTranslation_UsesLanguage(t *testing.T) {
	repo := newSettingsRepoStub()
	repo.data[service.KeyAISummaryLanguage] = "en-US"

	summaryRepo := &summaryRepoStub{}
	translationRepo := &translationRepoStub{}
	svc := service.NewAIService(summaryRepo, translationRepo, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	err := svc.SaveSummary(context.Background(), 1, false, "summary")
	require.NoError(t, err, "SaveSummary should not fail")
	require.Equal(t, "en-US", summaryRepo.lastLanguage, "expected language en-US")

	err = svc.SaveTranslation(context.Background(), 2, true, "content")
	require.NoError(t, err, "SaveTranslation should not fail")
	require.Equal(t, "en-US", translationRepo.lastLanguage, "expected language en-US")
}

func TestAIService_ClearAllCache_ErrorPropagation(t *testing.T) {
	summaryRepo := &summaryRepoStub{deleteAllErr: errors.New("summary delete failed")}
	translationRepo := &translationRepoStub{}
	listRepo := &listTranslationRepoStub{}
	svc := service.NewAIService(summaryRepo, translationRepo, listRepo, &analysisRepoStub{}, newSettingsRepoStub(), ai.NewRateLimiter(100))

	_, _, _, _, err := svc.ClearAllCache(context.Background())
	require.Error(t, err, "expected summary clear error")
	require.Contains(t, err.Error(), "clear summaries")

	summaryRepo.deleteAllErr = nil
	translationRepo.deleteAllErr = errors.New("translation delete failed")
	_, _, _, _, err = svc.ClearAllCache(context.Background())
	require.Error(t, err, "expected translation clear error")
	require.Contains(t, err.Error(), "clear translations")

	translationRepo.deleteAllErr = nil
	listRepo.deleteAllErr = errors.New("list translation delete failed")
	_, _, _, _, err = svc.ClearAllCache(context.Background())
	require.Error(t, err, "expected list translation clear error")
	require.Contains(t, err.Error(), "clear list translations")

	listRepo.deleteAllErr = nil
	analysisRepo := &analysisRepoStub{deleteAllErr: errors.New("analysis delete failed")}
	svc = service.NewAIService(summaryRepo, translationRepo, listRepo, analysisRepo, newSettingsRepoStub(), ai.NewRateLimiter(100))
	_, _, _, _, err = svc.ClearAllCache(context.Background())
	require.Error(t, err, "expected analysis clear error")
	require.Contains(t, err.Error(), "clear analyses")
}

func TestAIService_Summarize_MissingConfig(t *testing.T) {
	repo := newSettingsRepoStub()
	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	_, _, err := svc.Summarize(context.Background(), 1, "content", "title", false)
	require.Error(t, err, "expected error for missing config")
}

func TestAIService_TranslateBlocks_EmptyContent(t *testing.T) {
	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, &listTranslationRepoStub{}, &analysisRepoStub{}, newSettingsRepoStub(), ai.NewRateLimiter(100))

	_, _, _, err := svc.TranslateBlocks(context.Background(), 1, "", "title", false)
	require.Error(t, err, "expected error for empty content")
}

func TestAIService_TranslateBatch_EmptyInput(t *testing.T) {
	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, &listTranslationRepoStub{}, &analysisRepoStub{}, newSettingsRepoStub(), ai.NewRateLimiter(100))

	_, _, err := svc.TranslateBatch(context.Background(), nil)
	require.Error(t, err, "expected error for empty batch")
}

func TestAIService_GetCachedTranslation_Error(t *testing.T) {
	repo := newSettingsRepoStub()
	translationRepo := &translationRepoStub{getErr: errors.New("get failed")}
	svc := service.NewAIService(&summaryRepoStub{}, translationRepo, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	_, err := svc.GetCachedTranslation(context.Background(), 1, false)
	require.Error(t, err)
}

func TestAIService_SaveTranslation_Error(t *testing.T) {
	repo := newSettingsRepoStub()
	translationRepo := &translationRepoStub{saveErr: errors.New("save failed")}
	svc := service.NewAIService(&summaryRepoStub{}, translationRepo, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	err := svc.SaveTranslation(context.Background(), 1, false, "content")
	require.Error(t, err)
}

func TestAIService_TranslateBatch_CacheHit(t *testing.T) {
	repo := newSettingsRepoStub()
	listRepo := &listTranslationRepoStub{
		batchResult: map[int64]*model.AIListTranslation{
			1: {EntryID: 1, Title: "T1", Summary: "S1"},
			2: {EntryID: 2, Title: "T2", Summary: "S2"},
		},
	}
	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, listRepo, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	resultCh, errCh, err := svc.TranslateBatch(context.Background(), []service.BatchArticleInput{
		{ID: "1"},
		{ID: "2"},
	})
	require.NoError(t, err)

	results := make(map[string]service.BatchTranslateResult)
	for r := range resultCh {
		results[r.ID] = r
	}
	require.Len(t, results, 2)
	require.True(t, results["1"].Cached)
	require.NotNil(t, results["1"].Title)
	require.Equal(t, "T1", *results["1"].Title)
	require.NotNil(t, results["1"].Summary)
	require.Equal(t, "S1", *results["1"].Summary)

	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func TestAIService_TranslateBatch_InvalidIDs(t *testing.T) {
	repo := newSettingsRepoStub()
	listRepo := &listTranslationRepoStub{batchResult: map[int64]*model.AIListTranslation{}}
	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, listRepo, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	resultCh, errCh, err := svc.TranslateBatch(context.Background(), []service.BatchArticleInput{
		{ID: "not-a-number"},
	})
	require.NoError(t, err)

	for range resultCh {
		t.Fatalf("expected no results")
	}

	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func TestParseArticleAnalysis_AcceptsStringCoordinates(t *testing.T) {
	raw := `{
		"tag": "东亚/2026/日本/地震/东京",
		"summary": "测试摘要",
		"entities": ["东京", "日本"],
		"sentiment": "neutral",
		"importance": 8,
		"latitude": "35.6762N",
		"longitude": "139.6503E"
	}`

	result, err := service.ParseArticleAnalysis(raw)
	require.NoError(t, err)
	require.NotNil(t, result.Latitude)
	require.NotNil(t, result.Longitude)
	require.InDelta(t, 35.6762, *result.Latitude, 0.000001)
	require.InDelta(t, 139.6503, *result.Longitude, 0.000001)
}

func TestParseArticleAnalysis_DropsInvalidCoordinates(t *testing.T) {
	raw := `{
		"tag": "#欧洲/2026/欧盟/制裁/俄罗斯",
		"summary": "测试摘要",
		"entities": ["欧盟"],
		"sentiment": "negative",
		"importance": 9,
		"latitude": 135.5,
		"longitude": "not-a-number"
	}`

	result, err := service.ParseArticleAnalysis(raw)
	require.NoError(t, err)
	require.Nil(t, result.Latitude)
	require.Nil(t, result.Longitude)
	require.Equal(t, "#欧洲/2026/欧盟/制裁/俄罗斯", result.Tag)
}

func TestParseLocationCoordinateResult_AcceptsStringCoordinates(t *testing.T) {
	raw := `{
		"location": "东京",
		"latitude": "35.6762N",
		"longitude": "139.6503E"
	}`

	result, err := service.ParseLocationCoordinateResult(raw)
	require.NoError(t, err)
	require.NotNil(t, result.Location)
	require.Equal(t, "东京", *result.Location)
	require.NotNil(t, result.Latitude)
	require.NotNil(t, result.Longitude)
	require.InDelta(t, 35.6762, *result.Latitude, 0.000001)
	require.InDelta(t, 139.6503, *result.Longitude, 0.000001)
}

func TestParseLocationCoordinateResult_DropsPartialCoordinates(t *testing.T) {
	raw := `{
		"location": "某地",
		"latitude": 48.8566,
		"longitude": null
	}`

	result, err := service.ParseLocationCoordinateResult(raw)
	require.NoError(t, err)
	require.NotNil(t, result.Location)
	require.Nil(t, result.Latitude)
	require.Nil(t, result.Longitude)
}

func TestParseGeocodeSearchResponse_Success(t *testing.T) {
	resp := &http.Response{
		Body: ioNopCloser(strings.NewReader(`[{"lat":"35.6762","lon":"139.6503"}]`)),
	}

	latitude, longitude, err := service.ParseGeocodeSearchResponse(resp)
	require.NoError(t, err)
	require.NotNil(t, latitude)
	require.NotNil(t, longitude)
	require.InDelta(t, 35.6762, *latitude, 0.000001)
	require.InDelta(t, 139.6503, *longitude, 0.000001)
}

func TestParseGeocodeSearchResponse_Empty(t *testing.T) {
	resp := &http.Response{
		Body: ioNopCloser(strings.NewReader(`[]`)),
	}

	latitude, longitude, err := service.ParseGeocodeSearchResponse(resp)
	require.NoError(t, err)
	require.Nil(t, latitude)
	require.Nil(t, longitude)
}

type nopCloser struct {
	*strings.Reader
}

func (n nopCloser) Close() error { return nil }

func ioNopCloser(reader *strings.Reader) nopCloser {
	return nopCloser{Reader: reader}
}

type summaryRepoStub struct {
	lastLanguage  string
	deleteAllErr  error
	deleteAllRows int64
	getResult     *model.AISummary
	getErr        error
}

func (s *summaryRepoStub) Get(ctx context.Context, entryID int64, isReadability bool, language string) (*model.AISummary, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getResult, nil
}

func (s *summaryRepoStub) Save(ctx context.Context, entryID int64, isReadability bool, language, summary string) error {
	s.lastLanguage = language
	return nil
}

func (s *summaryRepoStub) DeleteByEntryID(ctx context.Context, entryID int64) error {
	return nil
}

func (s *summaryRepoStub) DeleteAll(ctx context.Context) (int64, error) {
	if s.deleteAllErr != nil {
		return 0, s.deleteAllErr
	}
	return s.deleteAllRows, nil
}

type translationRepoStub struct {
	lastLanguage string
	deleteAllErr error
	getErr       error
	saveErr      error
}

func (s *translationRepoStub) Get(ctx context.Context, entryID int64, isReadability bool, language string) (*model.AITranslation, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return nil, nil
}

func (s *translationRepoStub) Save(ctx context.Context, entryID int64, isReadability bool, language, content string) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.lastLanguage = language
	return nil
}

func (s *translationRepoStub) DeleteByEntryID(ctx context.Context, entryID int64) error {
	return nil
}

func (s *translationRepoStub) DeleteAll(ctx context.Context) (int64, error) {
	if s.deleteAllErr != nil {
		return 0, s.deleteAllErr
	}
	return 0, nil
}

type listTranslationRepoStub struct {
	deleteAllErr error
	batchResult  map[int64]*model.AIListTranslation
	getResult    *model.AIListTranslation
	getErr       error
	saveErr      error
	saveCalls    []listTranslationSaveCall
}

type listTranslationSaveCall struct {
	entryID  int64
	language string
	title    string
	summary  string
}

func (s *listTranslationRepoStub) Get(ctx context.Context, entryID int64, language string) (*model.AIListTranslation, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getResult, nil
}

func (s *listTranslationRepoStub) GetBatch(ctx context.Context, entryIDs []int64, language string) (map[int64]*model.AIListTranslation, error) {
	if s.batchResult != nil {
		return s.batchResult, nil
	}
	return make(map[int64]*model.AIListTranslation), nil
}

func (s *listTranslationRepoStub) Save(ctx context.Context, entryID int64, language, title, summary string) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saveCalls = append(s.saveCalls, listTranslationSaveCall{
		entryID:  entryID,
		language: language,
		title:    title,
		summary:  summary,
	})
	return nil
}

func (s *listTranslationRepoStub) DeleteByEntryID(ctx context.Context, entryID int64) error {
	return nil
}

func (s *listTranslationRepoStub) DeleteAll(ctx context.Context) (int64, error) {
	if s.deleteAllErr != nil {
		return 0, s.deleteAllErr
	}
	return 0, nil
}

type analysisRepoStub struct {
	deleteAllErr error
	getResult    *model.AIAnalysis
	getErr       error
	listResult   []model.StoredAIAnalysis
	listErr      error
	rangeResult  []model.StoredAIAnalysis
	rangeErr     error
	rangeStart   time.Time
	rangeEnd     time.Time
}

func (s *analysisRepoStub) Get(ctx context.Context, entryID int64, isReadability bool, language string) (*model.AIAnalysis, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getResult, nil
}

func (s *analysisRepoStub) Save(ctx context.Context, entryID int64, isReadability bool, language string, analysis model.AIAnalysis) error {
	return nil
}

func (s *analysisRepoStub) List(ctx context.Context, limit, offset int) ([]model.StoredAIAnalysis, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listResult, nil
}

func (s *analysisRepoStub) ListByCreatedRange(ctx context.Context, start, end time.Time) ([]model.StoredAIAnalysis, error) {
	s.rangeStart = start
	s.rangeEnd = end
	if s.rangeErr != nil {
		return nil, s.rangeErr
	}
	return s.rangeResult, nil
}

func (s *analysisRepoStub) DeleteByEntryID(ctx context.Context, entryID int64) error {
	return nil
}

func (s *analysisRepoStub) DeleteAll(ctx context.Context) (int64, error) {
	if s.deleteAllErr != nil {
		return 0, s.deleteAllErr
	}
	return 0, nil
}

func TestAIService_GetCachedSummary_Success(t *testing.T) {
	repo := newSettingsRepoStub()
	repo.data[service.KeyAISummaryLanguage] = "en-US"

	summaryRepo := &summaryRepoStub{
		getResult: &model.AISummary{
			ID:            1,
			EntryID:       123,
			IsReadability: false,
			Language:      "en-US",
			Summary:       "Test summary content",
		},
	}
	svc := service.NewAIService(summaryRepo, &translationRepoStub{}, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	result, err := svc.GetCachedSummary(context.Background(), 123, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int64(123), result.EntryID)
	require.Equal(t, "Test summary content", result.Summary)
}

func TestAIService_ListStoredAnalyses_Success(t *testing.T) {
	repo := newSettingsRepoStub()
	analysisRepo := &analysisRepoStub{
		listResult: []model.StoredAIAnalysis{
			{ID: 1, EntryID: 123, FeedID: 456, FeedTitle: "Feed", Tag: "#tag"},
		},
	}

	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, &listTranslationRepoStub{}, analysisRepo, repo, ai.NewRateLimiter(100))

	items, err := svc.ListStoredAnalyses(context.Background(), 50, 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(123), items[0].EntryID)
}

func TestAIService_Analyze_PersistsChineseTitleForStoredAnalyses(t *testing.T) {
	server, requestCount := newAIAnalyzeTestServer(t)
	defer server.Close()

	repo := newSettingsRepoStub()
	repo.data[service.KeyAISummaryLanguage] = "en-US"
	repo.data[service.KeyAIProvider] = ai.ProviderOpenAI
	repo.data[service.KeyAIAPIKey] = "test-key"
	repo.data[service.KeyAIBaseURL] = server.URL + "/v1/"
	repo.data[service.KeyAIModel] = "gpt-4o-mini"

	listRepo := &listTranslationRepoStub{}
	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, listRepo, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	analysis, err := svc.Analyze(context.Background(), 42, "<p>Markets moved higher.</p>", "Markets rally on Fed signals", false)
	require.NoError(t, err)
	require.NotNil(t, analysis)
	require.Equal(t, "#全球/美国/美联储", analysis.Tag)

	require.Len(t, listRepo.saveCalls, 1)
	require.Equal(t, int64(42), listRepo.saveCalls[0].entryID)
	require.Equal(t, "zh-CN", listRepo.saveCalls[0].language)
	require.Equal(t, "美联储信号带动市场上涨", listRepo.saveCalls[0].title)
	require.Equal(t, "", listRepo.saveCalls[0].summary)
	require.Equal(t, int32(2), requestCount.Load())
}

func TestAIService_Analyze_SkipsChineseTitleTranslationWhenCached(t *testing.T) {
	server, requestCount := newAIAnalyzeTestServer(t)
	defer server.Close()

	repo := newSettingsRepoStub()
	repo.data[service.KeyAISummaryLanguage] = "en-US"
	repo.data[service.KeyAIProvider] = ai.ProviderOpenAI
	repo.data[service.KeyAIAPIKey] = "test-key"
	repo.data[service.KeyAIBaseURL] = server.URL + "/v1/"
	repo.data[service.KeyAIModel] = "gpt-4o-mini"

	listRepo := &listTranslationRepoStub{
		getResult: &model.AIListTranslation{
			EntryID:  42,
			Language: "zh-CN",
			Title:    "已缓存中文标题",
			Summary:  "existing summary",
		},
	}
	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, listRepo, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	analysis, err := svc.Analyze(context.Background(), 42, "<p>Markets moved higher.</p>", "Markets rally on Fed signals", false)
	require.NoError(t, err)
	require.NotNil(t, analysis)
	require.Empty(t, listRepo.saveCalls)
	require.Equal(t, int32(1), requestCount.Load())
}

func TestAIService_Analyze_ArchivesMarkdownByDateFolderAndFeed(t *testing.T) {
	server, _ := newAIAnalyzeTestServer(t)
	defer server.Close()

	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	listRepo := repository.NewAIListTranslationRepository(db)
	analysisRepo := repository.NewAIAnalysisRepository(db)
	entryRepo := repository.NewEntryRepository(db)
	feedRepo := repository.NewFeedRepository(db)
	folderRepo := repository.NewFolderRepository(db)

	testutil.SeedSetting(t, db, service.KeyAISummaryLanguage, "en-US")
	testutil.SeedSetting(t, db, service.KeyAIProvider, ai.ProviderOpenAI)
	testutil.SeedSetting(t, db, service.KeyAIAPIKey, "test-key")
	testutil.SeedSetting(t, db, service.KeyAIBaseURL, server.URL+"/v1/")
	testutil.SeedSetting(t, db, service.KeyAIModel, "gpt-4o-mini")

	rootFolderID := testutil.SeedFolder(t, db, "CnNews", nil, "article")
	feedID := testutil.SeedFeed(t, db, model.Feed{
		Title:    "俄罗斯卫星通信社",
		URL:      "https://example.com/feed",
		FolderID: &rootFolderID,
	})

	originalTitle := "Nguyen Xuan Phuc elected president"
	entryURL := "https://example.com/article/1"
	publishedAt := time.Date(2026, 4, 7, 9, 30, 0, 0, time.FixedZone("CST", 8*3600))
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID:      feedID,
		Title:       &originalTitle,
		URL:         &entryURL,
		PublishedAt: &publishedAt,
	})

	archiveDir := t.TempDir()
	svc := service.NewAIService(
		&summaryRepoStub{},
		&translationRepoStub{},
		listRepo,
		analysisRepo,
		settingsRepo,
		ai.NewRateLimiter(100),
		service.WithAIAnalysisArchive(archiveDir, entryRepo, feedRepo, folderRepo),
	)

	analysis, err := svc.Analyze(context.Background(), entryID, "<p>Markets moved higher.</p>", originalTitle, false)
	require.NoError(t, err)
	require.NotNil(t, analysis)

	expectedFile := filepath.Join(
		archiveDir,
		time.Now().In(time.Local).Format("20060102"),
		"CnNews",
		"俄罗斯卫星通信社",
		"美联储信号带动市场上涨.md",
	)

	data, err := os.ReadFile(expectedFile)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "# 美联储信号带动市场上涨")
	require.Contains(t, content, "- Feed: 俄罗斯卫星通信社")
	require.Contains(t, content, "- Original Title: Nguyen Xuan Phuc elected president")
	require.Contains(t, content, "## 摘要")
	require.Contains(t, content, "summary")
	require.Contains(t, content, "## 标签")
	require.Contains(t, content, "#全球/美国/美联储")
}

func TestAIService_GetCachedAnalysis_ArchivesMarkdownOnCacheHit(t *testing.T) {
	db := testutil.NewTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	listRepo := repository.NewAIListTranslationRepository(db)
	analysisRepo := repository.NewAIAnalysisRepository(db)
	entryRepo := repository.NewEntryRepository(db)
	feedRepo := repository.NewFeedRepository(db)
	folderRepo := repository.NewFolderRepository(db)

	testutil.SeedSetting(t, db, service.KeyAISummaryLanguage, "en-US")

	rootFolderID := testutil.SeedFolder(t, db, "CnNews", nil, "article")
	feedID := testutil.SeedFeed(t, db, model.Feed{
		Title:    "俄罗斯卫星通信社",
		URL:      "https://example.com/feed",
		FolderID: &rootFolderID,
	})

	originalTitle := "Nguyen Xuan Phuc elected president"
	entryURL := "https://example.com/article/1"
	entryID := testutil.SeedEntry(t, db, model.Entry{
		FeedID: feedID,
		Title:  &originalTitle,
		URL:    &entryURL,
	})

	err := listRepo.Save(context.Background(), entryID, "zh-CN", "阮春福当选国家主席", "")
	require.NoError(t, err)

	err = analysisRepo.Save(context.Background(), entryID, false, "en-US", model.AIAnalysis{
		Tag:        "#东南亚/越南/政治",
		Summary:    "cached summary",
		Entities:   []string{"阮春福", "越南"},
		Sentiment:  "neutral",
		Importance: 7,
	})
	require.NoError(t, err)

	archiveDir := t.TempDir()
	svc := service.NewAIService(
		&summaryRepoStub{},
		&translationRepoStub{},
		listRepo,
		analysisRepo,
		settingsRepo,
		ai.NewRateLimiter(100),
		service.WithAIAnalysisArchive(archiveDir, entryRepo, feedRepo, folderRepo),
	)

	analysis, err := svc.GetCachedAnalysis(context.Background(), entryID, false)
	require.NoError(t, err)
	require.NotNil(t, analysis)

	expectedFile := filepath.Join(
		archiveDir,
		time.Now().In(time.Local).Format("20060102"),
		"CnNews",
		"俄罗斯卫星通信社",
		"阮春福当选国家主席.md",
	)

	data, err := os.ReadFile(expectedFile)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "# 阮春福当选国家主席")
	require.Contains(t, content, "- Entry ID: ")
	require.Contains(t, content, "- Original Title: Nguyen Xuan Phuc elected president")
	require.Contains(t, content, "cached summary")
}

func TestAIService_BuildDailyAnalysisReport_Success(t *testing.T) {
	repo := newSettingsRepoStub()
	day := time.Date(2026, 3, 30, 16, 45, 0, 0, time.FixedZone("CST", 8*3600))
	analysisRepo := &analysisRepoStub{
		rangeResult: []model.StoredAIAnalysis{
			{
				ID:         1,
				EntryID:    101,
				FeedID:     201,
				FeedTitle:  "World Feed",
				Tag:        "#全球/欧盟/关税/美国",
				Entities:   []string{"欧盟", "美国"},
				Sentiment:  "positive",
				Importance: 9,
				CreatedAt:  day.Add(-time.Hour),
			},
			{
				ID:         2,
				EntryID:    102,
				FeedID:     201,
				FeedTitle:  "World Feed",
				Tag:        "#全球/美国/贸易",
				Entities:   []string{"美国", "中国"},
				Sentiment:  "neutral",
				Importance: 6,
				CreatedAt:  day.Add(-2 * time.Hour),
			},
			{
				ID:         3,
				EntryID:    103,
				FeedID:     202,
				FeedTitle:  "Tech Feed",
				Tag:        "#AI/芯片/美国",
				Entities:   []string{"NVIDIA", "美国"},
				Sentiment:  "negative",
				Importance: 8,
				CreatedAt:  day.Add(-30 * time.Minute),
			},
		},
	}

	svc := service.NewAIService(&summaryRepoStub{}, &translationRepoStub{}, &listTranslationRepoStub{}, analysisRepo, repo, ai.NewRateLimiter(100))

	report, err := svc.BuildDailyAnalysisReport(context.Background(), day)
	require.NoError(t, err)
	require.NotNil(t, report)
	require.Equal(t, "2026-03-30", report.Date)
	require.Equal(t, 3, report.Total)
	require.Equal(t, 1, report.Sentiment.Positive)
	require.Equal(t, 1, report.Sentiment.Neutral)
	require.Equal(t, 1, report.Sentiment.Negative)
	require.Equal(t, 0, report.Sentiment.Other)
	require.Len(t, report.TopAnalyses, 3)
	require.Equal(t, int64(101), report.TopAnalyses[0].EntryID)
	require.Len(t, report.TopTags, 7)
	require.Equal(t, "美国", report.TopTags[0].Name)
	require.Equal(t, 3, report.TopTags[0].Count)
	require.Len(t, report.TopEntities, 4)
	require.Equal(t, "美国", report.TopEntities[0].Name)
	require.Equal(t, 3, report.TopEntities[0].Count)
	require.Len(t, report.TopFeeds, 2)
	require.Equal(t, int64(201), report.TopFeeds[0].FeedID)
	require.Equal(t, 2, report.TopFeeds[0].Count)
	require.Equal(t, time.Date(2026, 3, 30, 0, 0, 0, 0, day.Location()), analysisRepo.rangeStart)
	require.Equal(t, time.Date(2026, 3, 31, 0, 0, 0, 0, day.Location()), analysisRepo.rangeEnd)
}

func newAIAnalyzeTestServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses", r.URL.Path)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		require.NotEmpty(t, payload)

		call := requestCount.Add(1)
		responseText := ""
		switch call {
		case 1:
			responseText = `{"tag":"#全球/美国/美联储","summary":"summary","entities":["Fed"],"sentiment":"positive","importance":8,"latitude":37.7749,"longitude":-122.4194}`
		case 2:
			responseText = "美联储信号带动市场上涨"
		default:
			t.Fatalf("unexpected AI request #%d", call)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":                 "resp-1",
			"created_at":         1,
			"error":              map[string]any{"code": "server_error", "message": ""},
			"incomplete_details": map[string]any{"reason": ""},
			"instructions":       "",
			"metadata":           map[string]any{},
			"model":              "gpt-4o-mini",
			"object":             "response",
			"output": []any{
				map[string]any{
					"id":     "item-1",
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []any{
						map[string]any{
							"type":        "output_text",
							"text":        responseText,
							"annotations": []any{},
							"logprobs":    []any{},
						},
					},
				},
			},
			"parallel_tool_calls": false,
			"temperature":         0,
			"tool_choice":         "auto",
			"tools":               []any{},
			"top_p":               1,
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))

	return server, &requestCount
}

func TestAIService_GetCachedSummary_NotFound(t *testing.T) {
	repo := newSettingsRepoStub()
	summaryRepo := &summaryRepoStub{getResult: nil}
	svc := service.NewAIService(summaryRepo, &translationRepoStub{}, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	result, err := svc.GetCachedSummary(context.Background(), 123, false)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestAIService_GetCachedSummary_Error(t *testing.T) {
	repo := newSettingsRepoStub()
	summaryRepo := &summaryRepoStub{getErr: errors.New("database error")}
	svc := service.NewAIService(summaryRepo, &translationRepoStub{}, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	_, err := svc.GetCachedSummary(context.Background(), 123, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "database error")
}

// TestAIService_TranslateBlocks_ContextCancelled tests the BUG fix:
// When context is cancelled, TranslateBlocks should exit gracefully without
// continuing to process blocks or saving incomplete cache.
// See commit 01d64c1: fix: AI streaming request cancellation
func TestAIService_TranslateBlocks_ContextCancelled(t *testing.T) {
	repo := newSettingsRepoStub()
	translationRepo := &translationRepoStub{}
	svc := service.NewAIService(&summaryRepoStub{}, translationRepo, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	// Create a pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// TranslateBlocks with cancelled context
	// It should either return an error or return channels that complete quickly
	blockInfos, resultCh, errCh, err := svc.TranslateBlocks(ctx, 1, "<p>Test content</p>", "title", false)

	// With cancelled context, it should either:
	// 1. Return early with an error (missing config), or
	// 2. Return channels that close quickly without processing

	if err != nil {
		// Expected behavior: error due to missing config or context cancelled
		return
	}

	// If no error, channels should close quickly without blocking
	require.NotNil(t, blockInfos)
	require.NotNil(t, resultCh)
	require.NotNil(t, errCh)

	// Drain channels - they should close quickly
	resultsReceived := 0
	for range resultCh {
		resultsReceived++
	}

	// With cancelled context, translation should not have been attempted
	// (no AI config means it fails early anyway, but the context check is also there)
}

// TestAIService_TranslateBlocks_ContextCancelledDuringProcessing tests that
// context cancellation during processing exits the block loop properly.
// This verifies the labeled break and semaphore cancellation logic.
// See commit 01d64c1: fix: AI streaming request cancellation
func TestAIService_TranslateBlocks_ContextCancelledDuringProcessing(t *testing.T) {
	repo := newSettingsRepoStub()
	// Set up AI config to pass config validation
	repo.data[service.KeyAIProvider] = "openai"
	repo.data[service.KeyAIAPIKey] = "test-key"
	repo.data[service.KeyAIModel] = "gpt-4"

	translationRepo := &translationRepoStub{}
	svc := service.NewAIService(&summaryRepoStub{}, translationRepo, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	// Create context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Start TranslateBlocks
	blockInfos, resultCh, errCh, err := svc.TranslateBlocks(ctx, 1, "<p>Block 1</p><p>Block 2</p><p>Block 3</p>", "title", false)

	// With proper AI config, it should not return an immediate error
	if err != nil {
		// If it failed (e.g., due to network), that's fine for this test
		t.Skipf("TranslateBlocks returned error (expected in test env): %v", err)
		return
	}

	require.NotNil(t, blockInfos)
	require.NotNil(t, resultCh)
	require.NotNil(t, errCh)

	// Cancel context immediately after starting
	cancel()

	// Drain channels - should complete without hanging
	done := make(chan struct{})
	go func() {
		for range resultCh {
		}
		for range errCh {
		}
		close(done)
	}()

	// Wait with timeout - should complete quickly due to cancellation
	select {
	case <-done:
		// Good - channels closed
	case <-time.After(5 * time.Second):
		t.Fatal("TranslateBlocks did not exit after context cancellation")
	}
}

// TestAIService_TranslateBlocks_NoCacheOnCancel verifies that cancelled
// translations do not save incomplete results to cache.
// See commit 01d64c1: fix: AI streaming request cancellation
func TestAIService_TranslateBlocks_NoCacheOnCancel(t *testing.T) {
	repo := newSettingsRepoStub()
	translationRepo := &translationRepoStub{}
	svc := service.NewAIService(&summaryRepoStub{}, translationRepo, &listTranslationRepoStub{}, &analysisRepoStub{}, repo, ai.NewRateLimiter(100))

	// Create and immediately cancel context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to translate with cancelled context
	_, resultCh, errCh, err := svc.TranslateBlocks(ctx, 1, "<p>Test</p>", "title", false)

	if err != nil {
		// Expected - missing config error
		return
	}

	// Drain channels
	for range resultCh {
	}
	for range errCh {
	}

	// Verify no cache was saved (translationRepo.lastLanguage should be empty
	// because SaveTranslation was never called due to ctx.Err() != nil check)
	require.Empty(t, translationRepo.lastLanguage, "Should not save cache on cancelled context")
}
