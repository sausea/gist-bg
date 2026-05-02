package model

import "time"

const (
	AIAnalysisJobStatusQueued    = "queued"
	AIAnalysisJobStatusRunning   = "running"
	AIAnalysisJobStatusSucceeded = "succeeded"
	AIAnalysisJobStatusFailed    = "failed"

	AIAnalysisJobSourceAuto   = "auto"
	AIAnalysisJobSourceManual = "manual"

	AIAnalysisContentModeOriginal    = "original"
	AIAnalysisContentModeReadability = "readability"
)

type AIAnalysisJob struct {
	ID           int64      `json:"id"`
	EntryID      int64      `json:"entryId"`
	FeedID       int64      `json:"feedId"`
	Status       string     `json:"status"`
	Source       string     `json:"source"`
	ContentMode  string     `json:"contentMode"`
	Language     string     `json:"language"`
	RetryCount   int        `json:"retryCount"`
	ErrorMessage *string    `json:"errorMessage,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type AIAnalysisQueueItem struct {
	ID           int64      `json:"id"`
	EntryID      int64      `json:"entryId"`
	FeedID       int64      `json:"feedId"`
	FeedType     string     `json:"feedType"`
	EntryTitle   *string    `json:"entryTitle,omitempty"`
	EntryURL     *string    `json:"entryUrl,omitempty"`
	FeedTitle    string     `json:"feedTitle"`
	Author       *string    `json:"author,omitempty"`
	PublishedAt  *time.Time `json:"publishedAt,omitempty"`
	Status       string     `json:"status"`
	Source       string     `json:"source"`
	ContentMode  string     `json:"contentMode"`
	Language     string     `json:"language"`
	RetryCount   int        `json:"retryCount"`
	ErrorMessage *string    `json:"errorMessage,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}
