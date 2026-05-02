package scheduler

import (
	"context"
	"sync"
	"time"

	"gist/backend/pkg/logger"
)

type dailyArchivePublisher interface {
	PublishDailyArchiveReport(ctx context.Context, day time.Time) error
}

type AIDailyArchiveScheduler struct {
	publisher     dailyArchivePublisher
	hour          int
	minute        int
	checkInterval time.Duration
	now           func() time.Time
	stopCh        chan struct{}
	wg            sync.WaitGroup

	mu         sync.Mutex
	lastRunKey string
	cancelFunc context.CancelFunc
}

func NewAIDailyArchiveScheduler(publisher dailyArchivePublisher, hour, minute int) *AIDailyArchiveScheduler {
	if hour < 0 || hour > 23 {
		hour = 21
	}
	if minute < 0 || minute > 59 {
		minute = 0
	}

	return &AIDailyArchiveScheduler{
		publisher:     publisher,
		hour:          hour,
		minute:        minute,
		checkInterval: time.Minute,
		now:           time.Now,
		stopCh:        make(chan struct{}),
	}
}

func (s *AIDailyArchiveScheduler) Start() {
	if s == nil || s.publisher == nil {
		return
	}

	s.wg.Add(1)
	go s.run()
	logger.Info("ai daily archive scheduler started", "module", "scheduler", "action", "archive", "resource", "ai_report", "result", "ok", "hour", s.hour, "minute", s.minute)
}

func (s *AIDailyArchiveScheduler) Stop() {
	if s == nil {
		return
	}

	s.mu.Lock()
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	s.mu.Unlock()

	close(s.stopCh)
	s.wg.Wait()
	logger.Info("ai daily archive scheduler stopped", "module", "scheduler", "action", "archive", "resource", "ai_report", "result", "ok")
}

func (s *AIDailyArchiveScheduler) run() {
	defer s.wg.Done()

	s.runIfDue()

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.runIfDue()
		case <-s.stopCh:
			return
		}
	}
}

func (s *AIDailyArchiveScheduler) runIfDue() {
	now := s.now().In(time.Local)
	if now.Hour() < s.hour || (now.Hour() == s.hour && now.Minute() < s.minute) {
		return
	}

	runKey := now.Format("2006-01-02")

	s.mu.Lock()
	if s.lastRunKey == runKey {
		s.mu.Unlock()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	s.cancelFunc = cancel
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		s.cancelFunc = nil
		s.mu.Unlock()
	}()

	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if err := s.publisher.PublishDailyArchiveReport(ctx, day); err != nil {
		logger.Error("ai daily archive publish failed", "module", "scheduler", "action", "archive", "resource", "ai_report", "result", "failed", "date", day.Format("2006-01-02"), "error", err)
		return
	}

	s.mu.Lock()
	s.lastRunKey = runKey
	s.mu.Unlock()

	logger.Info("ai daily archive published", "module", "scheduler", "action", "archive", "resource", "ai_report", "result", "ok", "date", day.Format("2006-01-02"))
}
