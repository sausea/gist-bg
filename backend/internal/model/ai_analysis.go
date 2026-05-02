package model

import "time"

type AIAnalysis struct {
	ID            int64     `json:"id"`
	EntryID       int64     `json:"entryId"`
	IsReadability bool      `json:"isReadability"`
	Language      string    `json:"language"`
	Tag           string    `json:"tag"`
	Summary       string    `json:"summary"`
	Entities      []string  `json:"entities"`
	Sentiment     string    `json:"sentiment"`
	Importance    int       `json:"importance"`
	Latitude      *float64  `json:"latitude,omitempty"`
	Longitude     *float64  `json:"longitude,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type StoredAIAnalysis struct {
	ID            int64      `json:"id"`
	EntryID       int64      `json:"entryId"`
	FeedID        int64      `json:"feedId"`
	FeedType      string     `json:"feedType"`
	EntryTitle    *string    `json:"entryTitle,omitempty"`
	EntryURL      *string    `json:"entryUrl,omitempty"`
	FeedTitle     string     `json:"feedTitle"`
	Author        *string    `json:"author,omitempty"`
	PublishedAt   *time.Time `json:"publishedAt,omitempty"`
	Focused       bool       `json:"focused"`
	FocusTags     []string   `json:"focusTags,omitempty"`
	IsReadability bool       `json:"isReadability"`
	Language      string     `json:"language"`
	Tag           string     `json:"tag"`
	Summary       string     `json:"summary"`
	Entities      []string   `json:"entities"`
	Sentiment     string     `json:"sentiment"`
	Importance    int        `json:"importance"`
	Latitude      *float64   `json:"latitude,omitempty"`
	Longitude     *float64   `json:"longitude,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

type AIDailyReport struct {
	Date         string                    `json:"date"`
	Total        int                       `json:"total"`
	FocusedTotal int                       `json:"focusedTotal"`
	Sentiment    AIDailyReportSentiment    `json:"sentiment"`
	TopAnalyses  []StoredAIAnalysis        `json:"topAnalyses"`
	TopTags      []AIDailyReportCountItem  `json:"topTags"`
	TopEntities  []AIDailyReportCountItem  `json:"topEntities"`
	TopFeeds     []AIDailyReportFeedMetric `json:"topFeeds"`
	FocusedTags  []AIDailyReportCountItem  `json:"focusedTags"`
	FocusedItems []StoredAIAnalysis        `json:"focusedItems"`
	Overview     string                    `json:"overview,omitempty"`
	RiskReview   string                    `json:"riskReview,omitempty"`
	TrendOutlook string                    `json:"trendOutlook,omitempty"`
}

type AIDailyReportSentiment struct {
	Positive int `json:"positive"`
	Neutral  int `json:"neutral"`
	Negative int `json:"negative"`
	Other    int `json:"other"`
}

type AIDailyReportCountItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type AIDailyReportFeedMetric struct {
	FeedID    int64  `json:"feedId"`
	FeedTitle string `json:"feedTitle"`
	Count     int    `json:"count"`
}
