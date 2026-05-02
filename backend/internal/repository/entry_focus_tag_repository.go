package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"gist/backend/pkg/snowflake"
)

type EntryFocusTagRepository interface {
	ListByEntryID(ctx context.Context, entryID int64) ([]string, error)
	ListByEntryIDs(ctx context.Context, entryIDs []int64) (map[int64][]string, error)
	ReplaceByEntryID(ctx context.Context, entryID int64, tags []string) error
}

type entryFocusTagRepository struct {
	db *sql.DB
}

func NewEntryFocusTagRepository(db *sql.DB) EntryFocusTagRepository {
	return &entryFocusTagRepository{db: db}
}

func (r *entryFocusTagRepository) ListByEntryID(ctx context.Context, entryID int64) ([]string, error) {
	if entryID == 0 {
		return []string{}, nil
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT tag FROM entry_focus_tags WHERE entry_id = ? ORDER BY tag`,
		entryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if tags == nil {
		return []string{}, nil
	}
	return tags, nil
}

func (r *entryFocusTagRepository) ListByEntryIDs(ctx context.Context, entryIDs []int64) (map[int64][]string, error) {
	result := make(map[int64][]string, len(entryIDs))
	uniqueIDs := uniqueInt64s(entryIDs)
	if len(uniqueIDs) == 0 {
		return result, nil
	}

	args := make([]any, 0, len(uniqueIDs))
	for _, entryID := range uniqueIDs {
		args = append(args, entryID)
	}

	rows, err := r.db.QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT entry_id, tag FROM entry_focus_tags WHERE entry_id IN (%s) ORDER BY entry_id, tag`,
			buildPlaceholders(len(uniqueIDs)),
		),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var entryID int64
		var tag string
		if err := rows.Scan(&entryID, &tag); err != nil {
			return nil, err
		}
		result[entryID] = append(result[entryID], tag)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (r *entryFocusTagRepository) ReplaceByEntryID(ctx context.Context, entryID int64, tags []string) error {
	if entryID == 0 {
		return nil
	}

	normalized := normalizeFocusTags(tags)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM entry_focus_tags WHERE entry_id = ?`, entryID); err != nil {
		return err
	}

	now := formatTime(time.Now())
	for _, tag := range normalized {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO entry_focus_tags (id, entry_id, tag, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			snowflake.NextID(),
			entryID,
			tag,
			now,
			now,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func normalizeFocusTags(tags []string) []string {
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

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func buildPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}
