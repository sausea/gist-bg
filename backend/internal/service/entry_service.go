//go:generate mockgen -source=$GOFILE -destination=mock/$GOFILE -package=mock
package service

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"gist/backend/internal/model"
	"gist/backend/internal/repository"
	"gist/backend/pkg/logger"
)

type EntryListParams struct {
	FeedID       *int64
	FolderID     *int64
	ContentType  *string
	UnreadOnly   bool
	StarredOnly  bool
	HasThumbnail bool
	Limit        int
	Offset       int
}

type FeedAIStats struct {
	UnreadCount   int `json:"unreadCount"`
	AnalyzedCount int `json:"analyzedCount"`
	PendingCount  int `json:"pendingCount"`
}

type EntryServiceOption func(*entryService)

type EntryService interface {
	List(ctx context.Context, params EntryListParams) ([]model.Entry, error)
	GetByID(ctx context.Context, id int64) (model.Entry, error)
	GetFocus(ctx context.Context, id int64) (model.EntryFocus, error)
	MarkAsRead(ctx context.Context, id int64, read bool) error
	MarkAsStarred(ctx context.Context, id int64, starred bool) error
	UpdateFocus(ctx context.Context, id int64, focused bool, tags []string) (model.EntryFocus, error)
	MarkAllAsRead(ctx context.Context, feedID *int64, folderID *int64, contentType *string) error
	GetUnreadCounts(ctx context.Context) (map[int64]int, error)
	GetFeedAIStats(ctx context.Context) (map[int64]FeedAIStats, error)
	GetStarredCount(ctx context.Context) (int, error)
	// ClearReadabilityCache clears all readable_content from entries
	ClearReadabilityCache(ctx context.Context) (int64, error)
	// ClearEntryCache deletes all unstarred entries
	ClearEntryCache(ctx context.Context) (int64, error)
}

type entryService struct {
	entries      repository.EntryRepository
	feeds        repository.FeedRepository
	folders      repository.FolderRepository
	focusTagRepo repository.EntryFocusTagRepository
}

func NewEntryService(
	entries repository.EntryRepository,
	feeds repository.FeedRepository,
	folders repository.FolderRepository,
	options ...EntryServiceOption,
) EntryService {
	svc := &entryService{
		entries: entries,
		feeds:   feeds,
		folders: folders,
	}
	for _, option := range options {
		if option != nil {
			option(svc)
		}
	}
	return svc
}

func WithEntryFocusTags(repo repository.EntryFocusTagRepository) EntryServiceOption {
	return func(s *entryService) {
		s.focusTagRepo = repo
	}
}

func (s *entryService) List(ctx context.Context, params EntryListParams) ([]model.Entry, error) {
	// Validate feedID exists if provided
	if params.FeedID != nil {
		_, err := s.feeds.GetByID(ctx, *params.FeedID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}

	// Validate folderID exists if provided
	if params.FolderID != nil {
		_, err := s.folders.GetByID(ctx, *params.FolderID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}

	// Set default limit
	// Allow up to 101 for internal hasMore check (handler requests limit+1)
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 101 {
		limit = 101
	}

	filter := repository.EntryListFilter{
		FeedID:       params.FeedID,
		FolderID:     params.FolderID,
		ContentType:  params.ContentType,
		UnreadOnly:   params.UnreadOnly,
		StarredOnly:  params.StarredOnly,
		HasThumbnail: params.HasThumbnail,
		Limit:        limit,
		Offset:       params.Offset,
	}

	entries, err := s.entries.List(ctx, filter)
	if err != nil {
		logger.Error("entry list failed", "module", "service", "action", "list", "resource", "entry", "result", "failed", "error", err)
		return nil, err
	}
	logger.Debug("entry list", "module", "service", "action", "list", "resource", "entry", "result", "ok", "count", len(entries))
	return entries, nil
}

func (s *entryService) GetByID(ctx context.Context, id int64) (model.Entry, error) {
	entry, err := s.entries.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Entry{}, ErrNotFound
		}
		return model.Entry{}, err
	}
	logger.Debug("entry get", "module", "service", "action", "fetch", "resource", "entry", "result", "ok", "entry_id", id)
	return entry, nil
}

func (s *entryService) GetFocus(ctx context.Context, id int64) (model.EntryFocus, error) {
	entry, err := s.entries.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.EntryFocus{}, ErrNotFound
		}
		return model.EntryFocus{}, err
	}

	tags := []string{}
	if s.focusTagRepo != nil {
		tags, err = s.focusTagRepo.ListByEntryID(ctx, id)
		if err != nil {
			return model.EntryFocus{}, err
		}
	}

	return model.EntryFocus{
		EntryID: id,
		Focused: entry.Starred,
		Tags:    append([]string{}, tags...),
	}, nil
}

func (s *entryService) MarkAsRead(ctx context.Context, id int64, read bool) error {
	// Check entry exists
	_, err := s.entries.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if err := s.entries.UpdateReadStatus(ctx, id, read); err != nil {
		logger.Error("entry update read failed", "module", "service", "action", "update", "resource", "entry", "result", "failed", "entry_id", id, "read", read, "error", err)
		return err
	}
	logger.Info("entry read updated", "module", "service", "action", "update", "resource", "entry", "result", "ok", "entry_id", id, "read", read)
	return nil
}

func (s *entryService) MarkAllAsRead(ctx context.Context, feedID *int64, folderID *int64, contentType *string) error {
	// Validate feedID exists if provided
	if feedID != nil {
		_, err := s.feeds.GetByID(ctx, *feedID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
	}

	// Validate folderID exists if provided
	if folderID != nil {
		_, err := s.folders.GetByID(ctx, *folderID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
	}

	var feedIDValue any
	if feedID != nil {
		feedIDValue = *feedID
	}
	var folderIDValue any
	if folderID != nil {
		folderIDValue = *folderID
	}
	var contentTypeValue any
	if contentType != nil {
		contentTypeValue = *contentType
	}

	if err := s.entries.MarkAllAsRead(ctx, feedID, folderID, contentType); err != nil {
		logger.Error("entries mark all read failed", "module", "service", "action", "update", "resource", "entry", "result", "failed", "feed_id", feedIDValue, "folder_id", folderIDValue, "content_type", contentTypeValue, "error", err)
		return err
	}
	logger.Info("entries marked read", "module", "service", "action", "update", "resource", "entry", "result", "ok", "feed_id", feedIDValue, "folder_id", folderIDValue, "content_type", contentTypeValue)
	return nil
}

func (s *entryService) GetUnreadCounts(ctx context.Context) (map[int64]int, error) {
	counts, err := s.entries.GetAllUnreadCounts(ctx)
	if err != nil {
		logger.Error("entry unread counts failed", "module", "service", "action", "list", "resource", "entry", "result", "failed", "error", err)
		return nil, err
	}

	result := make(map[int64]int)
	for _, uc := range counts {
		result[uc.FeedID] = uc.Count
	}

	return result, nil
}

func (s *entryService) GetFeedAIStats(ctx context.Context) (map[int64]FeedAIStats, error) {
	stats, err := s.entries.GetFeedAIStats(ctx)
	if err != nil {
		logger.Error("feed ai stats failed", "module", "service", "action", "list", "resource", "entry", "result", "failed", "error", err)
		return nil, err
	}

	result := make(map[int64]FeedAIStats, len(stats))
	for _, stat := range stats {
		pendingCount := stat.UnreadCount - stat.AnalyzedCount
		if pendingCount < 0 {
			pendingCount = 0
		}
		result[stat.FeedID] = FeedAIStats{
			UnreadCount:   stat.UnreadCount,
			AnalyzedCount: stat.AnalyzedCount,
			PendingCount:  pendingCount,
		}
	}

	return result, nil
}

func (s *entryService) MarkAsStarred(ctx context.Context, id int64, starred bool) error {
	// Check entry exists
	_, err := s.entries.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if err := s.entries.UpdateStarredStatus(ctx, id, starred); err != nil {
		logger.Error("entry update starred failed", "module", "service", "action", "update", "resource", "entry", "result", "failed", "entry_id", id, "starred", starred, "error", err)
		return err
	}
	if !starred && s.focusTagRepo != nil {
		if err := s.focusTagRepo.ReplaceByEntryID(ctx, id, nil); err != nil {
			logger.Error("entry focus tags clear failed", "module", "service", "action", "update", "resource", "entry_focus", "result", "failed", "entry_id", id, "error", err)
			return err
		}
	}
	logger.Info("entry starred updated", "module", "service", "action", "update", "resource", "entry", "result", "ok", "entry_id", id, "starred", starred)
	return nil
}

func (s *entryService) UpdateFocus(ctx context.Context, id int64, focused bool, tags []string) (model.EntryFocus, error) {
	normalizedTags := normalizeEntryFocusTags(tags)
	if !focused {
		normalizedTags = []string{}
	}

	if err := s.MarkAsStarred(ctx, id, focused); err != nil {
		return model.EntryFocus{}, err
	}

	if s.focusTagRepo != nil {
		if err := s.focusTagRepo.ReplaceByEntryID(ctx, id, normalizedTags); err != nil {
			logger.Error("entry focus tags update failed", "module", "service", "action", "update", "resource", "entry_focus", "result", "failed", "entry_id", id, "error", err)
			return model.EntryFocus{}, err
		}
	}

	return model.EntryFocus{
		EntryID: id,
		Focused: focused,
		Tags:    append([]string{}, normalizedTags...),
	}, nil
}

func (s *entryService) GetStarredCount(ctx context.Context) (int, error) {
	count, err := s.entries.GetStarredCount(ctx)
	if err != nil {
		logger.Error("entry starred count failed", "module", "service", "action", "list", "resource", "entry", "result", "failed", "error", err)
		return 0, err
	}
	logger.Debug("entry starred count", "module", "service", "action", "list", "resource", "entry", "result", "ok", "count", count)
	return count, nil
}

func (s *entryService) ClearReadabilityCache(ctx context.Context) (int64, error) {
	deleted, err := s.entries.ClearAllReadableContent(ctx)
	if err != nil {
		logger.Error("readability cache clear failed", "module", "service", "action", "clear", "resource", "entry", "result", "failed", "error", err)
		return 0, err
	}
	logger.Info("readability cache cleared", "module", "service", "action", "clear", "resource", "entry", "result", "ok", "count", deleted)
	return deleted, nil
}

func (s *entryService) ClearEntryCache(ctx context.Context) (int64, error) {
	deleted, err := s.entries.DeleteUnstarred(ctx)
	if err != nil {
		logger.Error("entry cache clear failed", "module", "service", "action", "clear", "resource", "entry", "result", "failed", "error", err)
		return 0, err
	}
	// 重置所有 feeds 的 Conditional GET 信息，强制下次刷新时全量拉取
	// 避免因 304 Not Modified 导致已删除的文章无法被重新拉取
	if _, resetErr := s.feeds.ClearAllConditionalGet(ctx); resetErr != nil {
		logger.Warn("feed conditional get reset failed", "module", "service", "action", "update", "resource", "feed", "result", "failed", "error", resetErr)
	}
	logger.Info("entry cache cleared", "module", "service", "action", "clear", "resource", "entry", "result", "ok", "count", deleted)
	return deleted, nil
}

func normalizeEntryFocusTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		tag = strings.Join(strings.Fields(tag), " ")
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}
