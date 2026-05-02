package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"gist/backend/internal/config"
	"gist/backend/internal/db"
	"gist/backend/internal/handler"
	transport "gist/backend/internal/http"
	"gist/backend/internal/repository"
	"gist/backend/internal/scheduler"
	"gist/backend/internal/service"
	"gist/backend/internal/service/ai"
	"gist/backend/internal/service/anubis"
	"gist/backend/pkg/logger"
	"gist/backend/pkg/network"
	"gist/backend/pkg/snowflake"
)

// @title Gist API
// @version 1.0.1
// @description This is a modern RSS reader API.
// @BasePath /api
func main() {
	cfg := config.Load()

	logger.Init(logger.ParseLevel(cfg.LogLevel))

	if err := snowflake.Init(1); err != nil {
		logger.Error("init snowflake", "error", err)
		os.Exit(1)
	}

	dbConn, err := db.Open(cfg.DBPath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer dbConn.Close()

	folderRepo := repository.NewFolderRepository(dbConn)
	feedRepo := repository.NewFeedRepository(dbConn)
	entryRepo := repository.NewEntryRepository(dbConn)
	settingsRepo := repository.NewSettingsRepository(dbConn)
	aiSummaryRepo := repository.NewAISummaryRepository(dbConn)
	aiTranslationRepo := repository.NewAITranslationRepository(dbConn)
	aiListTranslationRepo := repository.NewAIListTranslationRepository(dbConn)
	aiAnalysisRepo := repository.NewAIAnalysisRepository(dbConn)
	aiAnalysisJobRepo := repository.NewAIAnalysisJobRepository(dbConn)
	entryFocusTagRepo := repository.NewEntryFocusTagRepository(dbConn)
	domainRateLimitRepo := repository.NewDomainRateLimitRepository(dbConn)

	// Initialize rate limiter with stored setting
	initialRateLimit := ai.DefaultRateLimit
	if setting, err := settingsRepo.Get(context.Background(), "ai.rate_limit"); err == nil && setting != nil {
		var val int
		fmt.Sscanf(setting.Value, "%d", &val)
		if val > 0 {
			initialRateLimit = val
		}
	}
	initialAIWorkerCount := 2
	if setting, err := settingsRepo.Get(context.Background(), "ai.worker_count"); err == nil && setting != nil {
		var val int
		fmt.Sscanf(setting.Value, "%d", &val)
		if val > 0 {
			initialAIWorkerCount = val
		}
	}
	rateLimiter := ai.NewRateLimiter(initialRateLimit)

	promptManager := ai.NewPromptManager(cfg.PromptsDir)
	if err := promptManager.EnsureDefaults(); err != nil {
		logger.Error("ensure ai prompts", "dir", cfg.PromptsDir, "error", err)
		os.Exit(1)
	}
	settingsService := service.NewSettingsService(
		settingsRepo,
		rateLimiter,
		service.WithSettingsPromptManager(promptManager),
	)

	// Initialize client factory for proxy and IP stack support
	clientFactory := network.NewClientFactory(settingsService, settingsService)

	// Initialize Anubis solver for bypassing Anubis protection
	anubisStore := anubis.NewStore(settingsRepo)
	anubisSolver := anubis.NewSolver(clientFactory, anubisStore)

	iconService := service.NewIconService(cfg.DataDir, feedRepo, clientFactory, anubisSolver)

	// Backfill icons for existing feeds (run in background)
	backfillCtx, cancelBackfill := context.WithCancel(context.Background())
	var backfillWG sync.WaitGroup
	backfillWG.Add(1)
	go func() {
		defer backfillWG.Done()
		if err := iconService.BackfillIcons(backfillCtx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("backfill icons", "error", err)
		}
	}()

	folderService := service.NewFolderService(folderRepo, feedRepo)
	feedService := service.NewFeedService(feedRepo, folderRepo, entryRepo, iconService, settingsService, clientFactory, anubisSolver)
	entryService := service.NewEntryService(
		entryRepo,
		feedRepo,
		folderRepo,
		service.WithEntryFocusTags(entryFocusTagRepo),
	)
	entryExportService := service.NewEntryExportService(cfg.ExportDir, entryRepo, feedRepo)
	readabilityService := service.NewReadabilityService(entryRepo, clientFactory, anubisSolver)
	domainRateLimitService := service.NewDomainRateLimitService(domainRateLimitRepo)
	refreshService := service.NewRefreshService(feedRepo, entryRepo, settingsService, iconService, clientFactory, anubisSolver, domainRateLimitService)
	opmlService := service.NewOPMLService(folderService, feedService, refreshService, iconService, folderRepo, feedRepo)

	proxyService := service.NewProxyService(clientFactory, anubisSolver)
	aiService := service.NewAIService(
		aiSummaryRepo,
		aiTranslationRepo,
		aiListTranslationRepo,
		aiAnalysisRepo,
		settingsRepo,
		rateLimiter,
		service.WithAIPromptManager(promptManager),
		service.WithAIEntryFocusTags(entryFocusTagRepo),
		service.WithAIAnalysisArchive(filepath.Join(cfg.DataDir, "ai-archive"), entryRepo, feedRepo, folderRepo),
	)
	aiStreamProcessor := service.NewAIStreamProcessor(entryRepo, aiAnalysisJobRepo, settingsRepo, aiService, initialAIWorkerCount, 256)
	service.AttachRefreshEntryIngestor(refreshService, aiStreamProcessor)
	authService := service.NewAuthService(settingsRepo)

	var dailyArchiveScheduler *scheduler.AIDailyArchiveScheduler
	if publisher, ok := aiService.(interface {
		PublishDailyArchiveReport(context.Context, time.Time) error
	}); ok {
		dailyArchiveScheduler = scheduler.NewAIDailyArchiveScheduler(publisher, 21, 0)
		dailyArchiveScheduler.Start()
	}

	folderHandler := handler.NewFolderHandler(folderService)
	feedHandler := handler.NewFeedHandler(feedService, refreshService)
	entryHandler := handler.NewEntryHandler(entryService, readabilityService, entryExportService)
	importTaskService := service.NewImportTaskService()
	opmlHandler := handler.NewOPMLHandler(opmlService, importTaskService)
	iconHandler := handler.NewIconHandler(iconService)
	proxyHandler := handler.NewProxyHandler(proxyService)
	settingsHandler := handler.NewSettingsHandler(settingsService, clientFactory)
	aiHandler := handler.NewAIHandler(aiService)
	handler.AttachAIStatusProvider(aiHandler, aiStreamProcessor)
	handler.AttachAIDailyReportAccess(aiHandler, authService, settingsService)
	authHandler := handler.NewAuthHandler(authService)
	domainRateLimitHandler := handler.NewDomainRateLimitHandler(domainRateLimitService)

	router := transport.NewRouter(folderHandler, feedHandler, entryHandler, opmlHandler, iconHandler, proxyHandler, settingsHandler, aiHandler, authHandler, domainRateLimitHandler, authService, cfg.StaticDir, cfg.EnableSwagger)

	// Start background scheduler (15 minutes interval)
	sched := scheduler.New(refreshService, 15*time.Minute)
	sched.Start()

	// Handle graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down...")

		// Create a deadline for shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		sched.Stop()
		if dailyArchiveScheduler != nil {
			dailyArchiveScheduler.Stop()
		}
		aiStreamProcessor.Close()
		readabilityService.Close()
		proxyService.Close()
		cancelBackfill()

		// Wait for backfill task to exit within shutdown deadline.
		backfillDone := make(chan struct{})
		go func() {
			backfillWG.Wait()
			close(backfillDone)
		}()
		select {
		case <-backfillDone:
		case <-ctx.Done():
			logger.Warn("backfill stop timeout")
		}

		// Gracefully shutdown the HTTP server
		if err := router.Shutdown(ctx); err != nil {
			logger.Error("server shutdown", "error", err)
		}
	}()

	if err := router.Start(cfg.Addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("start server", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}
