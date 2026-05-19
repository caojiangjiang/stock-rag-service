package model

// Document 表示当前项目中的一条可检索文档。
type Document struct {
	StockCode   string   `json:"stock_code"`
	CompanyName string   `json:"company_name"`
	DocType     string   `json:"doc_type"`
	Title       string   `json:"title"`
	SourceURL   string   `json:"source_url,omitempty"`
	Published   string   `json:"published_at,omitempty"`
	Content     string   `json:"content"`
	Keywords    []string `json:"keywords,omitempty"`
}

// DocumentsImportRequest 是最小文档导入请求。
type DocumentsImportRequest struct {
	Documents []Document `json:"documents"`
}

// DocumentsImportResponse 是最小文档导入响应。
type DocumentsImportResponse struct {
	ImportedCount int `json:"imported_count"`
	TotalCount    int `json:"total_count"`
}

// DocumentsListResponse 是最小文档列表响应。
type DocumentsListResponse struct {
	Documents  []Document `json:"documents"`
	TotalCount int        `json:"total_count"`
}
