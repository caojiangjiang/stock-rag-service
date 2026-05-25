package memory

// Fragment is a scored memory snippet returned by semantic search.
type Fragment struct {
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
	SourceType string                 `json:"source_type"` // short/medium/long
	SourceID   string                 `json:"source_id"`
	Metadata   map[string]interface{} `json:"metadata"`
}
