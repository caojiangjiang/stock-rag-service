package model

// RAGQueryRequest 是第一版问答请求模型。
type RAGQueryRequest struct {
	Question     string   `json:"question"`
	StockCode    string   `json:"stock_code,omitempty"`
	TimeRange    string   `json:"time_range,omitempty"`
	DocTypes     []string `json:"doc_types,omitempty"`
	TopK         int      `json:"top_k,omitempty"`
	UseLocalOnly bool     `json:"use_local_only,omitempty"`
	// SystemPrompt 非空时替换默认「股票投研助手」系统提示（Persona 单聊等场景）
	SystemPrompt string `json:"system_prompt,omitempty"`
	// UserQuestion 非空时作为展示给大模型的问题文案（检索仍用 Question）
	UserQuestion string `json:"user_question,omitempty"`
}

// Citation 是引用来源结构。
type Citation struct {
	Title        string `json:"title"`
	DocType      string `json:"doc_type"`
	SourceURL    string `json:"source_url"`
	Published    string `json:"published_at"`
	PageNo       int    `json:"page_no,omitempty"`
	SectionTitle string `json:"section_title,omitempty"`
	Content      string `json:"content"` // 添加内容字段
}

// RAGQueryResponse 是第一版问答响应模型。
type RAGQueryResponse struct {
	Answer         string     `json:"answer"`
	Citations      []Citation `json:"citations"`
	RetrievedCount int        `json:"retrieved_count"`
	RequestID      string     `json:"request_id"`
}
