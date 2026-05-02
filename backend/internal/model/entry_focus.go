package model

type EntryFocus struct {
	EntryID int64    `json:"entryId"`
	Focused bool     `json:"focused"`
	Tags    []string `json:"tags"`
}
