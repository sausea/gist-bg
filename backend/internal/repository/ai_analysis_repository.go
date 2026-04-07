//go:generate mockgen -source=$GOFILE -destination=mock/$GOFILE -package=mock
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"gist/backend/internal/model"
	"gist/backend/pkg/snowflake"
)

const storedAnalysisTitleLanguage = "zh-CN"

type AIAnalysisRepository interface {
	Get(ctx context.Context, entryID int64, isReadability bool, language string) (*model.AIAnalysis, error)
	List(ctx context.Context, limit, offset int) ([]model.StoredAIAnalysis, error)
	ListByCreatedRange(ctx context.Context, start, end time.Time) ([]model.StoredAIAnalysis, error)
	Save(ctx context.Context, entryID int64, isReadability bool, language string, analysis model.AIAnalysis) error
	DeleteByEntryID(ctx context.Context, entryID int64) error
	DeleteAll(ctx context.Context) (int64, error)
}

type aiAnalysisRepository struct {
	db dbtx
}

func NewAIAnalysisRepository(db dbtx) AIAnalysisRepository {
	return &aiAnalysisRepository{db: db}
}

func (r *aiAnalysisRepository) Get(ctx context.Context, entryID int64, isReadability bool, language string) (*model.AIAnalysis, error) {
	isReadabilityInt := 0
	if isReadability {
		isReadabilityInt = 1
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, entry_id, is_readability, language, tag, summary, entities, sentiment, importance, latitude, longitude, created_at
		 FROM ai_analyses WHERE entry_id = ? AND is_readability = ? AND language = ?`,
		entryID, isReadabilityInt, language,
	)

	var analysis model.AIAnalysis
	var isReadabilityDB int
	var entitiesJSON string
	var createdAt string
	var latitude sql.NullFloat64
	var longitude sql.NullFloat64

	err := row.Scan(
		&analysis.ID,
		&analysis.EntryID,
		&isReadabilityDB,
		&analysis.Language,
		&analysis.Tag,
		&analysis.Summary,
		&entitiesJSON,
		&analysis.Sentiment,
		&analysis.Importance,
		&latitude,
		&longitude,
		&createdAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	analysis.IsReadability = isReadabilityDB == 1
	analysis.CreatedAt, _ = parseTime(createdAt)
	if latitude.Valid {
		analysis.Latitude = &latitude.Float64
	}
	if longitude.Valid {
		analysis.Longitude = &longitude.Float64
	}
	if entitiesJSON != "" {
		_ = json.Unmarshal([]byte(entitiesJSON), &analysis.Entities)
	}
	if analysis.Entities == nil {
		analysis.Entities = []string{}
	}

	return &analysis, nil
}

func (r *aiAnalysisRepository) List(ctx context.Context, limit, offset int) ([]model.StoredAIAnalysis, error) {
	query := `SELECT
		a.id, a.entry_id, e.feed_id, f.type, COALESCE(NULLIF(lt.title, ''), e.title), e.url, f.title, e.author, e.published_at,
		a.is_readability, a.language, a.tag, a.summary, a.entities, a.sentiment, a.importance,
		a.latitude, a.longitude, a.created_at
	FROM ai_analyses a
	INNER JOIN entries e ON e.id = a.entry_id
	INNER JOIN feeds f ON f.id = e.feed_id
	LEFT JOIN ai_list_translations lt ON lt.entry_id = a.entry_id AND lt.language = ?
	ORDER BY a.created_at DESC, a.id DESC`

	args := []interface{}{storedAnalysisTitleLanguage}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanStoredAIAnalyses(rows)
}

func (r *aiAnalysisRepository) ListByCreatedRange(ctx context.Context, start, end time.Time) ([]model.StoredAIAnalysis, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT
			a.id, a.entry_id, e.feed_id, f.type, COALESCE(NULLIF(lt.title, ''), e.title), e.url, f.title, e.author, e.published_at,
			a.is_readability, a.language, a.tag, a.summary, a.entities, a.sentiment, a.importance,
			a.latitude, a.longitude, a.created_at
		FROM ai_analyses a
		INNER JOIN entries e ON e.id = a.entry_id
		INNER JOIN feeds f ON f.id = e.feed_id
		LEFT JOIN ai_list_translations lt ON lt.entry_id = a.entry_id AND lt.language = ?
		WHERE a.created_at >= ? AND a.created_at < ?
		ORDER BY a.created_at DESC, a.id DESC`,
		storedAnalysisTitleLanguage,
		formatTime(start),
		formatTime(end),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanStoredAIAnalyses(rows)
}

func scanStoredAIAnalyses(rows *sql.Rows) ([]model.StoredAIAnalysis, error) {
	var items []model.StoredAIAnalysis
	for rows.Next() {
		var item model.StoredAIAnalysis
		var entitiesJSON string
		var publishedAt sql.NullString
		var createdAt string
		var isReadabilityDB int
		var latitude sql.NullFloat64
		var longitude sql.NullFloat64

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
			&isReadabilityDB,
			&item.Language,
			&item.Tag,
			&item.Summary,
			&entitiesJSON,
			&item.Sentiment,
			&item.Importance,
			&latitude,
			&longitude,
			&createdAt,
		)
		if err != nil {
			return nil, err
		}

		item.IsReadability = isReadabilityDB == 1
		item.CreatedAt, _ = parseTime(createdAt)
		if publishedAt.Valid {
			item.PublishedAt = parseTimePtr(publishedAt.String)
		}
		if latitude.Valid {
			item.Latitude = &latitude.Float64
		}
		if longitude.Valid {
			item.Longitude = &longitude.Float64
		}
		if entitiesJSON != "" {
			_ = json.Unmarshal([]byte(entitiesJSON), &item.Entities)
		}
		if item.Entities == nil {
			item.Entities = []string{}
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *aiAnalysisRepository) Save(ctx context.Context, entryID int64, isReadability bool, language string, analysis model.AIAnalysis) error {
	id := snowflake.NextID()
	now := formatTime(time.Now())

	isReadabilityInt := 0
	if isReadability {
		isReadabilityInt = 1
	}

	entitiesJSON, err := json.Marshal(analysis.Entities)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(
		ctx,
		`INSERT INTO ai_analyses (id, entry_id, is_readability, language, tag, summary, entities, sentiment, importance, latitude, longitude, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(entry_id, is_readability, language) DO UPDATE SET
		   tag = excluded.tag,
		   summary = excluded.summary,
		   entities = excluded.entities,
		   sentiment = excluded.sentiment,
		   importance = excluded.importance,
		   latitude = excluded.latitude,
		   longitude = excluded.longitude,
		   created_at = excluded.created_at`,
		id,
		entryID,
		isReadabilityInt,
		language,
		analysis.Tag,
		analysis.Summary,
		string(entitiesJSON),
		analysis.Sentiment,
		analysis.Importance,
		analysis.Latitude,
		analysis.Longitude,
		now,
	)
	return err
}

func (r *aiAnalysisRepository) DeleteByEntryID(ctx context.Context, entryID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM ai_analyses WHERE entry_id = ?`, entryID)
	return err
}

func (r *aiAnalysisRepository) DeleteAll(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM ai_analyses`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
