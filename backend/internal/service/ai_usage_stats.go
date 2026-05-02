package service

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode"
)

const keyAIUsageDailyPrefix = "stats.ai_usage.daily."

type AIUsageCounter struct {
	RequestCount     int `json:"requestCount"`
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

type AIUsageSceneStats struct {
	Scene string `json:"scene"`
	AIUsageCounter
}

type AIUsagePeriodStats struct {
	AIUsageCounter
	Scenes []AIUsageSceneStats `json:"scenes"`
}

type AIUsageDayStats struct {
	Date string `json:"date"`
	AIUsagePeriodStats
}

type AIUsageStats struct {
	Today     AIUsagePeriodStats `json:"today"`
	Last7Days AIUsagePeriodStats `json:"last7Days"`
	AllTime   AIUsagePeriodStats `json:"allTime"`
	Daily     []AIUsageDayStats  `json:"daily"`
}

type storedAIUsageDayStats struct {
	Date   string                    `json:"date"`
	Totals AIUsageCounter            `json:"totals"`
	Scenes map[string]AIUsageCounter `json:"scenes,omitempty"`
}

func (c *AIUsageCounter) Add(promptTokens, completionTokens int) {
	if promptTokens < 0 {
		promptTokens = 0
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	c.RequestCount++
	c.PromptTokens += promptTokens
	c.CompletionTokens += completionTokens
	c.TotalTokens += promptTokens + completionTokens
}

func (c *AIUsageCounter) Merge(other AIUsageCounter) {
	c.RequestCount += other.RequestCount
	c.PromptTokens += other.PromptTokens
	c.CompletionTokens += other.CompletionTokens
	c.TotalTokens += other.TotalTokens
}

func aiUsageDailyKey(day time.Time) string {
	return keyAIUsageDailyPrefix + day.In(time.Local).Format("2006-01-02")
}

func parseAIUsageDateFromKey(key string) string {
	if !strings.HasPrefix(key, keyAIUsageDailyPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(key, keyAIUsageDailyPrefix))
}

func normalizeAIUsageScene(scene string) string {
	switch strings.TrimSpace(strings.ToLower(scene)) {
	case aiSceneTranslation:
		return aiSceneTranslation
	case aiSceneReport:
		return aiSceneReport
	default:
		return aiSceneAnalysis
	}
}

func normalizeStoredAIUsageDayStats(day *storedAIUsageDayStats, fallbackDate string) {
	if day == nil {
		return
	}
	if strings.TrimSpace(day.Date) == "" {
		day.Date = strings.TrimSpace(fallbackDate)
	}
	if day.Scenes == nil {
		day.Scenes = make(map[string]AIUsageCounter)
	}
	normalizedScenes := make(map[string]AIUsageCounter, len(day.Scenes))
	for scene, counter := range day.Scenes {
		normalizedScenes[normalizeAIUsageScene(scene)] = counter
	}
	day.Scenes = normalizedScenes
}

func buildAIUsagePeriodStats(days []storedAIUsageDayStats) AIUsagePeriodStats {
	stats := AIUsagePeriodStats{
		Scenes: []AIUsageSceneStats{},
	}
	sceneTotals := make(map[string]AIUsageCounter)
	for _, day := range days {
		stats.Merge(day.Totals)
		for scene, counter := range day.Scenes {
			normalized := normalizeAIUsageScene(scene)
			merged := sceneTotals[normalized]
			merged.Merge(counter)
			sceneTotals[normalized] = merged
		}
	}
	stats.Scenes = sortedAIUsageScenes(sceneTotals)
	return stats
}

func toAIUsageDayStats(day storedAIUsageDayStats) AIUsageDayStats {
	return AIUsageDayStats{
		Date: day.Date,
		AIUsagePeriodStats: AIUsagePeriodStats{
			AIUsageCounter: day.Totals,
			Scenes:         sortedAIUsageScenes(day.Scenes),
		},
	}
}

func sortedAIUsageScenes(items map[string]AIUsageCounter) []AIUsageSceneStats {
	if len(items) == 0 {
		return []AIUsageSceneStats{}
	}
	scenes := make([]AIUsageSceneStats, 0, len(items))
	for scene, counter := range items {
		scenes = append(scenes, AIUsageSceneStats{
			Scene:          normalizeAIUsageScene(scene),
			AIUsageCounter: counter,
		})
	}
	sort.Slice(scenes, func(i, j int) bool {
		if scenes[i].TotalTokens != scenes[j].TotalTokens {
			return scenes[i].TotalTokens > scenes[j].TotalTokens
		}
		return scenes[i].Scene < scenes[j].Scene
	})
	return scenes
}

func estimateAITokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}

	var total int
	var asciiChars int

	flushASCII := func() {
		if asciiChars == 0 {
			return
		}
		total += (asciiChars + 3) / 4
		asciiChars = 0
	}

	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			flushASCII()
		case r <= unicode.MaxASCII:
			asciiChars++
		default:
			flushASCII()
			total++
		}
	}

	flushASCII()
	if total <= 0 {
		return 1
	}
	return total
}

func decodeStoredAIUsageDayStats(raw string, fallbackDate string) (storedAIUsageDayStats, error) {
	day := storedAIUsageDayStats{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &day); err != nil {
			return storedAIUsageDayStats{}, err
		}
	}
	normalizeStoredAIUsageDayStats(&day, fallbackDate)
	return day, nil
}
