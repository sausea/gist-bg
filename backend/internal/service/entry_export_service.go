package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gist/backend/internal/repository"
	"gist/backend/pkg/logger"
)

type EntryExportService interface {
	ExportEntryMarkdown(ctx context.Context, id int64, tags []string) (string, string, error)
}

type entryExportService struct {
	entries repository.EntryRepository
	feeds   repository.FeedRepository
	exportDir string
}

func NewEntryExportService(exportDir string, entries repository.EntryRepository, feeds repository.FeedRepository) EntryExportService {
	return &entryExportService{
		entries:   entries,
		feeds:     feeds,
		exportDir: exportDir,
	}
}

func (s *entryExportService) ExportEntryMarkdown(ctx context.Context, id int64, tags []string) (string, string, error) {
	entry, err := s.entries.GetByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", ErrNotFound
		}
		return "", "", err
	}

	feed, err := s.feeds.GetByID(ctx, entry.FeedID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", ErrNotFound
		}
		return "", "", err
	}

	normalizedTags := normalizeTags(tags)

	now := time.Now()
	dateStr := now.Format("2006-01-02")
	savedAt := now.Format(time.RFC3339)

	if err := os.MkdirAll(s.exportDir, 0o755); err != nil {
		return "", "", err
	}

	fileName := fmt.Sprintf("%s.md", dateStr)
	filePath := filepath.Join(s.exportDir, fileName)

	needsHeader := true
	if info, err := os.Stat(filePath); err == nil {
		needsHeader = info.Size() == 0
	}

	title := "Untitled"
	if entry.Title != nil && strings.TrimSpace(*entry.Title) != "" {
		title = strings.TrimSpace(*entry.Title)
	}

	content := ""
	if entry.Content != nil && strings.TrimSpace(*entry.Content) != "" {
		content = *entry.Content
	} else if entry.ReadableContent != nil && strings.TrimSpace(*entry.ReadableContent) != "" {
		content = *entry.ReadableContent
	}
	if content == "" {
		content = "(no content)"
	}

	var builder strings.Builder
	if needsHeader {
		builder.WriteString(fmt.Sprintf("# Gist Export %s\n\n", dateStr))
	}
	builder.WriteString(fmt.Sprintf("## %s\n\n", title))
	builder.WriteString(fmt.Sprintf("- Feed: %s\n", strings.TrimSpace(feed.Title)))
	if entry.URL != nil && strings.TrimSpace(*entry.URL) != "" {
		builder.WriteString(fmt.Sprintf("- URL: %s\n", strings.TrimSpace(*entry.URL)))
	}
	if len(normalizedTags) > 0 {
		builder.WriteString(fmt.Sprintf("- Tags: %s\n", strings.Join(normalizedTags, ", ")))
	}
	builder.WriteString(fmt.Sprintf("- SavedAt: %s\n\n", savedAt))
	builder.WriteString("### Content\n\n")
	builder.WriteString(content)
	builder.WriteString("\n\n---\n\n")

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			logger.Warn("export markdown close failed", "module", "service", "action", "update", "resource", "entry", "result", "failed", "error", cerr)
		}
	}()

	if _, err := file.WriteString(builder.String()); err != nil {
		return "", "", err
	}

	return fileName, savedAt, nil
}

func normalizeTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, raw := range tags {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		trimmed = strings.ReplaceAll(trimmed, "\n", " ")
		trimmed = strings.ReplaceAll(trimmed, "\r", " ")
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
