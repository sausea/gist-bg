//go:generate mockgen -source=$GOFILE -destination=mock/$GOFILE -package=mock
package repository

import (
	"context"
	"database/sql"
	"time"

	"gist/backend/internal/model"
	"gist/backend/pkg/snowflake"
)

type AIAnalysisQueueStats struct {
	QueuedCount  int
	RunningCount int
	FailedCount  int
}

type AIAnalysisJobRepository interface {
	GetByEntryID(ctx context.Context, entryID int64) (*model.AIAnalysisJob, error)
	UpsertQueued(ctx context.Context, entryID, feedID int64, source, contentMode, language string) error
	MarkRunning(ctx context.Context, entryID int64) error
	MarkSucceeded(ctx context.Context, entryID int64, contentMode, language string) error
	MarkFailed(ctx context.Context, entryID int64, errorMessage string) error
	RequeueRunning(ctx context.Context) (int64, error)
	ListQueuedEntryIDs(ctx context.Context, dayStart, dayEnd time.Time, includeOlderAuto bool, limit int) ([]int64, error)
	ListBackfillEntryIDs(ctx context.Context, dayStart, dayEnd time.Time, limit int) ([]int64, error)
	HasPendingForDay(ctx context.Context, dayStart, dayEnd time.Time) (bool, error)
	ListQueue(ctx context.Context, limit int) ([]model.AIAnalysisQueueItem, error)
	GetQueueStats(ctx context.Context) (AIAnalysisQueueStats, error)
}

type aiAnalysisJobRepository struct {
	db dbtx
}

func NewAIAnalysisJobRepository(db dbtx) AIAnalysisJobRepository {
	return &aiAnalysisJobRepository{db: db}
}

func (r *aiAnalysisJobRepository) GetByEntryID(ctx context.Context, entryID int64) (*model.AIAnalysisJob, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, entry_id, feed_id, status, source, content_mode, language, retry_count, error_message, created_at, started_at, finished_at, updated_at
		 FROM ai_analysis_jobs
		 WHERE entry_id = ?`,
		entryID,
	)

	job, err := scanAIAnalysisJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *aiAnalysisJobRepository) UpsertQueued(ctx context.Context, entryID, feedID int64, source, contentMode, language string) error {
	if source == "" {
		source = model.AIAnalysisJobSourceAuto
	}
	if contentMode == "" {
		contentMode = model.AIAnalysisContentModeOriginal
	}

	id := snowflake.NextID()
	now := formatTime(time.Now())

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO ai_analysis_jobs (
			id, entry_id, feed_id, status, source, content_mode, language,
			retry_count, error_message, created_at, started_at, finished_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 0, NULL, ?, NULL, NULL, ?)
		ON CONFLICT(entry_id) DO UPDATE SET
			feed_id = excluded.feed_id,
			status = excluded.status,
			source = excluded.source,
			content_mode = excluded.content_mode,
			language = excluded.language,
			error_message = NULL,
			started_at = NULL,
			finished_at = NULL,
			updated_at = excluded.updated_at`,
		id,
		entryID,
		feedID,
		model.AIAnalysisJobStatusQueued,
		source,
		contentMode,
		language,
		now,
		now,
	)
	return err
}

func (r *aiAnalysisJobRepository) MarkRunning(ctx context.Context, entryID int64) error {
	now := formatTime(time.Now())
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE ai_analysis_jobs
		 SET status = ?,
		     retry_count = retry_count + CASE WHEN status != ? THEN 1 ELSE 0 END,
		     started_at = ?,
		     updated_at = ?
		 WHERE entry_id = ?`,
		model.AIAnalysisJobStatusRunning,
		model.AIAnalysisJobStatusRunning,
		now,
		now,
		entryID,
	)
	return err
}

func (r *aiAnalysisJobRepository) MarkSucceeded(ctx context.Context, entryID int64, contentMode, language string) error {
	if contentMode == "" {
		contentMode = model.AIAnalysisContentModeOriginal
	}

	now := formatTime(time.Now())
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE ai_analysis_jobs
		 SET status = ?,
		     content_mode = ?,
		     language = ?,
		     error_message = NULL,
		     finished_at = ?,
		     updated_at = ?
		 WHERE entry_id = ?`,
		model.AIAnalysisJobStatusSucceeded,
		contentMode,
		language,
		now,
		now,
		entryID,
	)
	return err
}

func (r *aiAnalysisJobRepository) MarkFailed(ctx context.Context, entryID int64, errorMessage string) error {
	now := formatTime(time.Now())
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE ai_analysis_jobs
		 SET status = ?,
		     error_message = ?,
		     finished_at = ?,
		     updated_at = ?
		 WHERE entry_id = ?`,
		model.AIAnalysisJobStatusFailed,
		errorMessage,
		now,
		now,
		entryID,
	)
	return err
}

func (r *aiAnalysisJobRepository) RequeueRunning(ctx context.Context) (int64, error) {
	now := formatTime(time.Now())
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE ai_analysis_jobs
		 SET status = ?,
		     started_at = NULL,
		     updated_at = ?
		 WHERE status = ?`,
		model.AIAnalysisJobStatusQueued,
		now,
		model.AIAnalysisJobStatusRunning,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *aiAnalysisJobRepository) ListQueuedEntryIDs(ctx context.Context, dayStart, dayEnd time.Time, includeOlderAuto bool, limit int) ([]int64, error) {
	dayStartValue := formatTime(dayStart)
	dayEndValue := formatTime(dayEnd)
	query := `SELECT j.entry_id
		FROM ai_analysis_jobs j
		INNER JOIN entries e ON e.id = j.entry_id
		WHERE j.status = ?
		  AND (
			? = 1
			OR j.source = ?
			OR (` + entryLogicalTimeExpr + ` >= ? AND ` + entryLogicalTimeExpr + ` < ?)
		  )
		ORDER BY
			CASE
				WHEN j.source = ? THEN 0
				WHEN (` + entryLogicalTimeExpr + ` >= ? AND ` + entryLogicalTimeExpr + ` < ?) THEN 1
				ELSE 2
			END,
			CASE
				WHEN (` + entryLogicalTimeExpr + ` >= ? AND ` + entryLogicalTimeExpr + ` < ?) THEN ` + entryLogicalTimeExpr + `
				ELSE NULL
			END DESC,
			j.updated_at ASC,
			j.entry_id ASC`
	includeOlderInt := 0
	if includeOlderAuto {
		includeOlderInt = 1
	}
	args := []interface{}{
		model.AIAnalysisJobStatusQueued,
		includeOlderInt,
		model.AIAnalysisJobSourceManual,
		dayStartValue,
		dayEndValue,
		model.AIAnalysisJobSourceManual,
		dayStartValue,
		dayEndValue,
		dayStartValue,
		dayEndValue,
	}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanInt64Rows(rows)
}

func (r *aiAnalysisJobRepository) ListBackfillEntryIDs(ctx context.Context, dayStart, dayEnd time.Time, limit int) ([]int64, error) {
	dayStartValue := formatTime(dayStart)
	dayEndValue := formatTime(dayEnd)
	query := `SELECT e.id
		FROM entries e
		WHERE (
			(e.readable_content IS NOT NULL AND TRIM(e.readable_content) != '')
			OR (e.content IS NOT NULL AND TRIM(e.content) != '')
		)
		AND (` + entryLogicalTimeExpr + ` >= ? AND ` + entryLogicalTimeExpr + ` < ?)
		AND NOT EXISTS (
			SELECT 1
			FROM ai_analyses a
			WHERE a.entry_id = e.id
		)
		AND NOT EXISTS (
			SELECT 1
			FROM ai_analysis_jobs j
			WHERE j.entry_id = e.id
			  AND j.status IN (?, ?)
		)
		ORDER BY ` + entryLogicalTimeExpr + ` DESC, e.id DESC`
	args := []interface{}{
		dayStartValue,
		dayEndValue,
		model.AIAnalysisJobStatusQueued,
		model.AIAnalysisJobStatusRunning,
	}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanInt64Rows(rows)
}

func (r *aiAnalysisJobRepository) HasPendingForDay(ctx context.Context, dayStart, dayEnd time.Time) (bool, error) {
	dayStartValue := formatTime(dayStart)
	dayEndValue := formatTime(dayEnd)
	row := r.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1
			FROM ai_analysis_jobs j
			INNER JOIN entries e ON e.id = j.entry_id
			WHERE j.status IN (?, ?)
			  AND (`+entryLogicalTimeExpr+` >= ? AND `+entryLogicalTimeExpr+` < ?)
		)`,
		model.AIAnalysisJobStatusQueued,
		model.AIAnalysisJobStatusRunning,
		dayStartValue,
		dayEndValue,
	)

	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *aiAnalysisJobRepository) ListQueue(ctx context.Context, limit int) ([]model.AIAnalysisQueueItem, error) {
	query := `SELECT
		j.id, j.entry_id, j.feed_id, f.type, COALESCE(NULLIF(lt.title, ''), e.title), e.url, f.title, e.author, e.published_at,
		j.status, j.source, j.content_mode, j.language, j.retry_count, j.error_message,
		j.created_at, j.started_at, j.finished_at, j.updated_at
	FROM ai_analysis_jobs j
	INNER JOIN entries e ON e.id = j.entry_id
	INNER JOIN feeds f ON f.id = e.feed_id
	LEFT JOIN ai_list_translations lt ON lt.entry_id = j.entry_id AND lt.language = ?
	WHERE j.status IN (?, ?, ?)
	ORDER BY
		CASE j.status
			WHEN ? THEN 0
			WHEN ? THEN 1
			WHEN ? THEN 2
			ELSE 3
		END,
		CASE WHEN j.status IN (?, ?) THEN j.updated_at END ASC,
		CASE WHEN j.status = ? THEN j.updated_at END DESC,
		j.id DESC`

	args := []interface{}{
		storedAnalysisTitleLanguage,
		model.AIAnalysisJobStatusQueued,
		model.AIAnalysisJobStatusRunning,
		model.AIAnalysisJobStatusFailed,
		model.AIAnalysisJobStatusRunning,
		model.AIAnalysisJobStatusQueued,
		model.AIAnalysisJobStatusFailed,
		model.AIAnalysisJobStatusRunning,
		model.AIAnalysisJobStatusQueued,
		model.AIAnalysisJobStatusFailed,
	}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAIAnalysisQueueItems(rows)
}

func (r *aiAnalysisJobRepository) GetQueueStats(ctx context.Context) (AIAnalysisQueueStats, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS queued_count,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS running_count,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS failed_count
		FROM ai_analysis_jobs`,
		model.AIAnalysisJobStatusQueued,
		model.AIAnalysisJobStatusRunning,
		model.AIAnalysisJobStatusFailed,
	)

	var stats AIAnalysisQueueStats
	if err := row.Scan(&stats.QueuedCount, &stats.RunningCount, &stats.FailedCount); err != nil {
		return AIAnalysisQueueStats{}, err
	}
	return stats, nil
}

type aiAnalysisJobScanner interface {
	Scan(dest ...interface{}) error
}

const entryLogicalTimeExpr = `CASE
	WHEN e.published_at IS NOT NULL AND TRIM(e.published_at) != '' THEN e.published_at
	ELSE e.created_at
END`

func scanAIAnalysisJob(scanner aiAnalysisJobScanner) (model.AIAnalysisJob, error) {
	var job model.AIAnalysisJob
	var errorMessage sql.NullString
	var createdAt string
	var startedAt sql.NullString
	var finishedAt sql.NullString
	var updatedAt string

	err := scanner.Scan(
		&job.ID,
		&job.EntryID,
		&job.FeedID,
		&job.Status,
		&job.Source,
		&job.ContentMode,
		&job.Language,
		&job.RetryCount,
		&errorMessage,
		&createdAt,
		&startedAt,
		&finishedAt,
		&updatedAt,
	)
	if err != nil {
		return model.AIAnalysisJob{}, err
	}

	job.CreatedAt, _ = parseTime(createdAt)
	job.UpdatedAt, _ = parseTime(updatedAt)
	if errorMessage.Valid {
		job.ErrorMessage = &errorMessage.String
	}
	if startedAt.Valid {
		job.StartedAt = parseTimePtr(startedAt.String)
	}
	if finishedAt.Valid {
		job.FinishedAt = parseTimePtr(finishedAt.String)
	}
	return job, nil
}

func scanInt64Rows(rows *sql.Rows) ([]int64, error) {
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func scanAIAnalysisQueueItems(rows *sql.Rows) ([]model.AIAnalysisQueueItem, error) {
	var items []model.AIAnalysisQueueItem
	for rows.Next() {
		var item model.AIAnalysisQueueItem
		var errorMessage sql.NullString
		var publishedAt sql.NullString
		var createdAt string
		var startedAt sql.NullString
		var finishedAt sql.NullString
		var updatedAt string

		err := rows.Scan(
			&item.ID,
			&item.EntryID,
			&item.FeedID,
			&item.FeedType,
			&item.EntryTitle,
			&item.EntryURL,
			&item.FeedTitle,
			&item.Author,
			&publishedAt,
			&item.Status,
			&item.Source,
			&item.ContentMode,
			&item.Language,
			&item.RetryCount,
			&errorMessage,
			&createdAt,
			&startedAt,
			&finishedAt,
			&updatedAt,
		)
		if err != nil {
			return nil, err
		}

		item.CreatedAt, _ = parseTime(createdAt)
		item.UpdatedAt, _ = parseTime(updatedAt)
		if publishedAt.Valid {
			item.PublishedAt = parseTimePtr(publishedAt.String)
		}
		if errorMessage.Valid {
			item.ErrorMessage = &errorMessage.String
		}
		if startedAt.Valid {
			item.StartedAt = parseTimePtr(startedAt.String)
		}
		if finishedAt.Valid {
			item.FinishedAt = parseTimePtr(finishedAt.String)
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}
