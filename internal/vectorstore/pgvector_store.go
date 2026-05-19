// internal/vectorstore/pgvector_store.go
package vectorstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	appmodel "stock_rag/internal/model"

	"github.com/jackc/pgx/v5"
)

type PgVectorStore struct {
	conn *pgx.Conn
}

func NewPgVectorStore(ctx context.Context, host, port, user, password, dbname, sslmode string) (*PgVectorStore, error) {
	if sslmode == "" {
		sslmode = "disable"
	}
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)
	conn, err := pgx.Connect(context.Background(), connStr)
	if err != nil {
		return nil, err
	}

	// 初始化表结构
	if err := initTables(conn); err != nil {
		conn.Close(ctx)
		return nil, err
	}

	return &PgVectorStore{conn: conn}, nil
}

// initTables 初始化表结构
func initTables(conn *pgx.Conn) error {
	// 启用 pgvector 扩展
	_, err := conn.Exec(context.Background(), "CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		return err
	}
	_, err = conn.Exec(context.Background(), "CREATE EXTENSION IF NOT EXISTS pg_trgm")
	if err != nil {
		return err
	}

	// 创建统一文档表
	_, err = conn.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS documents (
			doc_id SERIAL PRIMARY KEY,
			stock_code VARCHAR(20) NOT NULL,
			company_name VARCHAR(100),
			doc_type VARCHAR(50) NOT NULL,
			title TEXT NOT NULL,
			source_url TEXT,
			published_at TIMESTAMP,
			source VARCHAR(100),
			raw_data JSONB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		
		-- 创建索引
		CREATE INDEX IF NOT EXISTS idx_documents_stock_code ON documents(stock_code);
		CREATE INDEX IF NOT EXISTS idx_documents_doc_type ON documents(doc_type);
		CREATE INDEX IF NOT EXISTS idx_documents_published_at ON documents(published_at DESC);
		CREATE INDEX IF NOT EXISTS idx_documents_source ON documents(source);
		CREATE INDEX IF NOT EXISTS idx_documents_title_trgm ON documents USING GIN (title gin_trgm_ops);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_title_stock_code_unique ON documents(title, stock_code);
	`)
	if err != nil {
		return err
	}

	return nil
}

// Upsert 插入或更新向量记录
func (s *PgVectorStore) Upsert(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	// 获取向量维度
	dimension := len(records[0].Vector)

	// 检查并更新表结构
	if err := s.ensureVectorTable(ctx, dimension); err != nil {
		return fmt.Errorf("ensureVectorTable failed: %w", err)
	}

	// 直接执行操作，不使用事务
	for i, record := range records {
		// 插入文档（如果不存在）
		var docID int
		// 处理可能缺失的字段
		stockCode := record.Metadata["stock_code"]
		if stockCode == "" {
			stockCode = "unknown"
		}
		docType := record.Metadata["doc_type"]
		if docType == "" {
			docType = "unknown"
		}
		publishedAt := parsePublishedAt(record.Metadata["published_at"])

		// 插入文档
		err := s.conn.QueryRow(ctx, `
			INSERT INTO documents (stock_code, company_name, doc_type, title, source_url, published_at, source, raw_data) 
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) 
				ON CONFLICT (title, stock_code) DO UPDATE SET
					company_name = EXCLUDED.company_name,
					doc_type = EXCLUDED.doc_type,
					source_url = EXCLUDED.source_url,
					published_at = COALESCE(EXCLUDED.published_at, documents.published_at),
					source = EXCLUDED.source,
					raw_data = EXCLUDED.raw_data,
					updated_at = CURRENT_TIMESTAMP
			RETURNING doc_id
		`, stockCode, record.Metadata["company_name"], docType,
			record.Metadata["title"], record.Metadata["source_url"], publishedAt,
			record.Metadata["source"], record.Metadata).Scan(&docID)
		if err != nil {
			return fmt.Errorf("upsert document failed: %w", err)
		}

		// 确保 docID 有效
		if docID == 0 {
			// 如果 docID 无效，跳过当前记录
			continue
		}

		// 将 []float32 转换为 PostgreSQL 向量格式
		vectorStr := "["
		for j, val := range record.Vector {
			if j > 0 {
				vectorStr += ", "
			}
			vectorStr += fmt.Sprintf("%f", val)
		}
		vectorStr += "]"

		// 处理可能的空值
		pageNo := parseOptionalInt(record.Metadata["page_no"])

		// 插入或更新向量，使用记录索引作为 chunk_index
		_, err = s.conn.Exec(ctx, `
				INSERT INTO document_chunks (doc_id, chunk_index, section_title, page_no, content, embedding, metadata) 
				VALUES ($1, $2, $3, $4, $5, $6, $7) 
				ON CONFLICT (doc_id, chunk_index) DO UPDATE SET 
					content = $5, embedding = $6, metadata = $7
			`, docID, i, record.Metadata["section_title"], pageNo,
			record.Content, vectorStr, record.Metadata)
		if err != nil {
			return fmt.Errorf("insert document_chunks failed: %w", err)
		}
	}

	return nil
}

// ensureVectorTable 确保向量表存在且维度正确
func (s *PgVectorStore) ensureVectorTable(ctx context.Context, dimension int) error {
	// 检查表是否存在
	var exists bool
	err := s.conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'document_chunks')").Scan(&exists)
	if err != nil {
		return err
	}

	if !exists {
		// 表不存在，创建新表
		_, err = s.conn.Exec(ctx, fmt.Sprintf(`
			CREATE TABLE document_chunks (
				chunk_id SERIAL PRIMARY KEY,
				doc_id INTEGER,
				chunk_index INTEGER NOT NULL,
				section_title VARCHAR(255),
				page_no INTEGER,
				content TEXT NOT NULL,
				embedding vector(%d) NOT NULL,
				metadata JSONB,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(doc_id, chunk_index)
			);
			
			-- 创建索引
			CREATE INDEX IF NOT EXISTS idx_document_chunks_doc_id ON document_chunks(doc_id);
			CREATE INDEX IF NOT EXISTS idx_document_chunks_chunk_index ON document_chunks(doc_id, chunk_index);
		`, dimension))
		if err != nil {
			return err
		}

		// 添加外键约束
		_, err = s.conn.Exec(ctx, `
			ALTER TABLE document_chunks ADD CONSTRAINT fk_document_chunks_doc_id
				FOREIGN KEY (doc_id) REFERENCES documents(doc_id) ON DELETE CASCADE
		`)
		if err != nil {
			// 外键约束添加失败，继续执行
		}

		// 只有当向量维度 <= 2000 时才创建向量索引
		if dimension <= 2000 {
			_, err = s.conn.Exec(ctx, `
				CREATE INDEX idx_document_chunks_embedding 
				ON document_chunks 
				USING ivfflat (embedding vector_cosine_ops)
				WITH (lists = 100);
			`)
			if err != nil {
				// 索引创建失败，继续执行
			}
		}
	}

	_, _ = s.conn.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_document_chunks_content_fts ON document_chunks USING GIN (to_tsvector('simple', content));
		CREATE INDEX IF NOT EXISTS idx_document_chunks_content_trgm ON document_chunks USING GIN (content gin_trgm_ops);
	`)

	return nil
}

// recreateVectorTable 重建向量表
func (s *PgVectorStore) recreateVectorTable(ctx context.Context, dimension int) error {
	// 先删除表（如果存在）
	_, err := s.conn.Exec(ctx, "DROP TABLE IF EXISTS document_chunks CASCADE")
	if err != nil {
		return err
	}

	// 创建新表
	_, err = s.conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE document_chunks (
			chunk_id SERIAL PRIMARY KEY,
			doc_id INTEGER,
			chunk_index INTEGER NOT NULL,
			section_title VARCHAR(255),
			page_no INTEGER,
			content TEXT NOT NULL,
			embedding vector(%d) NOT NULL,
			metadata JSONB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(doc_id, chunk_index)
		);
		
		-- 创建索引
		CREATE INDEX IF NOT EXISTS idx_document_chunks_doc_id ON document_chunks(doc_id);
		CREATE INDEX IF NOT EXISTS idx_document_chunks_chunk_index ON document_chunks(doc_id, chunk_index);
	`, dimension))
	if err != nil {
		return err
	}

	// 添加外键约束
	_, err = s.conn.Exec(ctx, `
		ALTER TABLE document_chunks ADD CONSTRAINT fk_document_chunks_doc_id
			FOREIGN KEY (doc_id) REFERENCES documents(doc_id) ON DELETE CASCADE
	`)
	if err != nil {
		// 外键约束添加失败，继续执行
	}

	// 只有当向量维度 <= 2000 时才创建向量索引
	if dimension <= 2000 {
		_, err = s.conn.Exec(ctx, `
			CREATE INDEX idx_document_chunks_embedding 
			ON document_chunks 
			USING ivfflat (embedding vector_cosine_ops)
			WITH (lists = 100);
		`)
		if err != nil {
			// 索引创建失败，继续执行
		}
	}

	return nil
}

// Search 搜索向量
func (s *PgVectorStore) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	// 处理文档类型映射，支持中英文转换
	docTypes := req.Filter.DocTypes
	mappedDocTypes := make([]string, 0, len(docTypes))
	for _, dt := range docTypes {
		mappedDocTypes = append(mappedDocTypes, dt)
		// 添加中文映射
		switch dt {
		case "report":
			mappedDocTypes = append(mappedDocTypes, "年报", "中报")
		case "announcement":
			mappedDocTypes = append(mappedDocTypes, "公告")
		}
	}

	// 检查向量表结构，确保维度匹配
	dimension := len(req.QueryVector)
	if err := s.ensureVectorTable(ctx, dimension); err != nil {
		// 如果表结构更新失败，继续执行，使用 fallback 机制
	}

	// 构建查询（使用统一的 document_chunks 表）
	query := `
		SELECT dc.content, d.title, d.stock_code, d.doc_type, d.source_url, d.published_at, 
			dc.page_no, dc.section_title, 1 - (dc.embedding <=> $1) as similarity
		FROM document_chunks dc
		JOIN documents d ON dc.doc_id = d.doc_id
		WHERE 1=1
	`

	// 添加相似度阈值过滤
	if req.SimilarityThreshold > 0 {
		query += fmt.Sprintf(" AND (1 - (dc.embedding <=> $1)) >= %f", req.SimilarityThreshold)
	}

	// 将 []float32 转换为 PostgreSQL 向量格式
	vectorStr := "["
	for i, val := range req.QueryVector {
		if i > 0 {
			vectorStr += ", "
		}
		vectorStr += fmt.Sprintf("%f", val)
	}
	vectorStr += "]"

	args := []interface{}{vectorStr}
	argPos := 2

	// 添加过滤条件
	if req.Filter.StockCode != "" {
		query += fmt.Sprintf(" AND d.stock_code = $%d", argPos)
		args = append(args, req.Filter.StockCode)
		argPos++
	}

	if len(mappedDocTypes) > 0 {
		query += fmt.Sprintf(" AND d.doc_type = ANY($%d)", argPos)
		args = append(args, mappedDocTypes)
		argPos++
	}

	if req.Filter.TimeRange != "" {
		timeRange := strings.TrimSpace(strings.ToLower(req.Filter.TimeRange))
		if timeRange != "latest" {
			query += fmt.Sprintf(" AND d.published_at IS NOT NULL")

			switch timeRange {
			case "7d":
				query += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '7 days'")
			case "30d":
				query += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '30 days'")
			case "90d":
				query += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '90 days'")
			case "180d":
				query += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '180 days'")
			case "365d", "1y":
				query += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '365 days'")
			}
		}
	}

	// 添加排序和限制
	query += fmt.Sprintf(" ORDER BY similarity DESC LIMIT $%d", argPos)
	args = append(args, req.TopK)

	// 执行查询
	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		// 如果查询失败，尝试使用简单查询
		fallbackQuery := `
			SELECT dc.content, d.title, d.stock_code, d.doc_type, d.source_url, d.published_at, 
				dc.page_no, dc.section_title, 0.0 as similarity
			FROM document_chunks dc
			JOIN documents d ON dc.doc_id = d.doc_id
			WHERE 1=1
		`
		fallbackArgs := []interface{}{}
		fallbackArgPos := 1

		if req.Filter.StockCode != "" {
			fallbackQuery += fmt.Sprintf(" AND d.stock_code = $%d", fallbackArgPos)
			fallbackArgs = append(fallbackArgs, req.Filter.StockCode)
			fallbackArgPos++
		}

		if len(mappedDocTypes) > 0 {
			fallbackQuery += fmt.Sprintf(" AND d.doc_type = ANY($%d)", fallbackArgPos)
			fallbackArgs = append(fallbackArgs, mappedDocTypes)
			fallbackArgPos++
		}

		// 添加时间范围过滤
		if req.Filter.TimeRange != "" {
			timeRange := strings.TrimSpace(strings.ToLower(req.Filter.TimeRange))
			if timeRange != "latest" {
				fallbackQuery += fmt.Sprintf(" AND d.published_at IS NOT NULL")

				switch timeRange {
				case "7d":
					fallbackQuery += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '7 days'")
				case "30d":
					fallbackQuery += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '30 days'")
				case "90d":
					fallbackQuery += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '90 days'")
				case "180d":
					fallbackQuery += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '180 days'")
				case "365d", "1y":
					fallbackQuery += fmt.Sprintf(" AND d.published_at >= NOW() - INTERVAL '365 days'")
				}
			}
		}

		fallbackQuery += fmt.Sprintf(" LIMIT $%d", fallbackArgPos)
		fallbackArgs = append(fallbackArgs, req.TopK)

		rows, err = s.conn.Query(ctx, fallbackQuery, fallbackArgs...)
		if err != nil {
			// 如果查询失败，返回空结果
			return []SearchResult{}, nil
		}
	}
	defer rows.Close()

	// 处理结果
	results := make([]SearchResult, 0)
	for rows.Next() {
		var content, title, stockCode, docType, sourceURL string
		var publishedAt interface{}
		var pageNo interface{}
		var sectionTitle string
		var score float64

		err := rows.Scan(&content, &title, &stockCode, &docType, &sourceURL, &publishedAt, &pageNo, &sectionTitle, &score)
		if err != nil {
			return nil, err
		}

		// 处理时间格式
		publishedStr := ""
		if publishedAt != nil {
			publishedStr = fmt.Sprintf("%v", publishedAt)
		}

		// 处理页码
		pageNoInt := 0
		if pageNo != nil {
			if pn, ok := pageNo.(int); ok {
				pageNoInt = pn
			}
		}

		result := SearchResult{
			Content: content,
			Citation: appmodel.Citation{
				Title:        title,
				DocType:      docType,
				SourceURL:    sourceURL,
				Published:    publishedStr,
				PageNo:       pageNoInt,
				SectionTitle: sectionTitle,
			},
			Score: score,
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// KeywordSearch 使用 PostgreSQL 全文检索 + trigram + LIKE 做关键词召回。
func (s *PgVectorStore) KeywordSearch(ctx context.Context, req KeywordSearchRequest) ([]SearchResult, error) {
	queryText := strings.TrimSpace(req.QueryText)
	if queryText == "" && len(req.Terms) == 0 {
		return nil, nil
	}

	mappedDocTypes := mapDocTypes(req.Filter.DocTypes)
	likePatterns := buildLikePatterns(req.Terms)
	if len(likePatterns) == 0 && queryText != "" {
		likePatterns = []string{"%" + escapeLikeValue(strings.ToLower(queryText)) + "%"}
	}

	query := `
		SELECT dc.content, d.title, d.stock_code, d.doc_type, d.source_url, d.published_at,
			dc.page_no, dc.section_title,
			(
				(ts_rank_cd(
					to_tsvector('simple', concat_ws(' ', d.title, COALESCE(dc.section_title, ''), dc.content)),
					websearch_to_tsquery('simple', $1)
				) * 0.6) +
				(GREATEST(
					similarity(COALESCE(d.title, ''), $1),
					similarity(COALESCE(dc.section_title, ''), $1),
					similarity(dc.content, $1)
				) * 0.25) +
				(COALESCE((
					SELECT COUNT(*)::float
					FROM unnest($2::text[]) AS pattern
					WHERE lower(concat_ws(' ', d.title, COALESCE(dc.section_title, ''), dc.content)) LIKE pattern ESCAPE '\\'
				), 0) * 0.15)
			) AS score
		FROM document_chunks dc
		JOIN documents d ON dc.doc_id = d.doc_id
		WHERE 1=1
	`

	args := []interface{}{queryText, likePatterns}
	argPos := 3

	if req.Filter.StockCode != "" && req.Filter.StockCode != "GENERAL" {
		query += fmt.Sprintf(" AND d.stock_code = $%d", argPos)
		args = append(args, req.Filter.StockCode)
		argPos++
	}
	if len(mappedDocTypes) > 0 {
		query += fmt.Sprintf(" AND d.doc_type = ANY($%d)", argPos)
		args = append(args, mappedDocTypes)
		argPos++
	}
	query, args, argPos = appendTimeRangeFilter(query, args, argPos, req.Filter.TimeRange)

	query += `
		 AND (
			to_tsvector('simple', concat_ws(' ', d.title, COALESCE(dc.section_title, ''), dc.content)) @@ websearch_to_tsquery('simple', $1)
			OR GREATEST(
				similarity(COALESCE(d.title, ''), $1),
				similarity(COALESCE(dc.section_title, ''), $1),
				similarity(dc.content, $1)
			) > 0.08
			OR lower(concat_ws(' ', d.title, COALESCE(dc.section_title, ''), dc.content)) LIKE ANY($2) ESCAPE '\\'
		 )
	`
	query += fmt.Sprintf(" ORDER BY score DESC LIMIT $%d", argPos)
	args = append(args, req.TopK)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SearchResult, 0)
	for rows.Next() {
		var content, title, stockCode, docType, sourceURL string
		var publishedAt interface{}
		var pageNo interface{}
		var sectionTitle string
		var score float64

		if err := rows.Scan(&content, &title, &stockCode, &docType, &sourceURL, &publishedAt, &pageNo, &sectionTitle, &score); err != nil {
			return nil, err
		}

		publishedStr := ""
		if publishedAt != nil {
			publishedStr = fmt.Sprintf("%v", publishedAt)
		}
		pageNoInt := 0
		if pageNo != nil {
			switch value := pageNo.(type) {
			case int32:
				pageNoInt = int(value)
			case int64:
				pageNoInt = int(value)
			case int:
				pageNoInt = value
			}
		}

		results = append(results, SearchResult{
			Content: content,
			Citation: appmodel.Citation{
				Title:        title,
				DocType:      docType,
				SourceURL:    sourceURL,
				Published:    publishedStr,
				PageNo:       pageNoInt,
				SectionTitle: sectionTitle,
			},
			Score: score,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// ListDocuments 返回当前 PostgreSQL 中的全部文档。
func (s *PgVectorStore) ListDocuments(ctx context.Context) ([]appmodel.Document, error) {
	query := `
		SELECT d.stock_code, d.company_name, d.doc_type, d.title, d.source_url, d.published_at, d.raw_data,
			COALESCE((
				SELECT string_agg(dc.content, E'\n' ORDER BY dc.chunk_index)
				FROM document_chunks dc
				WHERE dc.doc_id = d.doc_id
			), '') AS content
		FROM documents d
		ORDER BY created_at DESC
	`

	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make([]appmodel.Document, 0)
	for rows.Next() {
		var doc appmodel.Document
		var publishedAt interface{}
		var rawData []byte
		var aggregatedContent string

		err := rows.Scan(&doc.StockCode, &doc.CompanyName, &doc.DocType, &doc.Title, &doc.SourceURL, &publishedAt, &rawData, &aggregatedContent)
		if err != nil {
			return nil, err
		}

		// 处理时间格式
		if publishedAt != nil {
			switch value := publishedAt.(type) {
			case time.Time:
				doc.Published = value.Format(time.RFC3339)
			default:
				doc.Published = fmt.Sprintf("%v", publishedAt)
			}
		}

		doc.Content = aggregatedContent
		mergeRawDataIntoDocument(&doc, rawData)

		docs = append(docs, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return docs, nil
}

// ListAvailableDocTypes 直接在 PostgreSQL 中返回当前股票可用的 doc_type。
func (s *PgVectorStore) ListAvailableDocTypes(ctx context.Context, stockCode string) ([]string, error) {
	query := `
		SELECT DISTINCT LOWER(TRIM(doc_type)) AS doc_type
		FROM documents
		WHERE TRIM(doc_type) <> ''
	`
	args := []interface{}{}
	if strings.TrimSpace(stockCode) != "" && !strings.EqualFold(strings.TrimSpace(stockCode), "GENERAL") {
		query += " AND stock_code = $1"
		args = append(args, strings.TrimSpace(stockCode))
	}
	query += " ORDER BY doc_type"

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]string, 0)
	for rows.Next() {
		var docType string
		if err := rows.Scan(&docType); err != nil {
			return nil, err
		}
		if docType != "" {
			result = append(result, docType)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// FallbackDocumentSearch 使用 PostgreSQL 侧聚合文档做更宽松的 fallback 检索。
func (s *PgVectorStore) FallbackDocumentSearch(ctx context.Context, req KeywordSearchRequest) ([]appmodel.Document, error) {
	queryText := strings.TrimSpace(req.QueryText)
	if queryText == "" && len(req.Terms) == 0 {
		return nil, nil
	}

	mappedDocTypes := mapDocTypes(req.Filter.DocTypes)
	likePatterns := buildLikePatterns(req.Terms)
	if len(likePatterns) == 0 && queryText != "" {
		likePatterns = []string{"%" + escapeLikeValue(strings.ToLower(queryText)) + "%"}
	}

	cte := `
		WITH doc_view AS (
			SELECT d.doc_id, d.stock_code, d.company_name, d.doc_type, d.title, d.source_url, d.published_at, d.raw_data,
				COALESCE(string_agg(dc.content, E'\n' ORDER BY dc.chunk_index), '') AS content
			FROM documents d
			LEFT JOIN document_chunks dc ON dc.doc_id = d.doc_id
			WHERE 1=1
	`
	args := []interface{}{}
	argPos := 1
	if req.Filter.StockCode != "" && req.Filter.StockCode != "GENERAL" {
		cte += fmt.Sprintf(" AND d.stock_code = $%d", argPos)
		args = append(args, req.Filter.StockCode)
		argPos++
	}
	if len(mappedDocTypes) > 0 {
		cte += fmt.Sprintf(" AND d.doc_type = ANY($%d)", argPos)
		args = append(args, mappedDocTypes)
		argPos++
	}
	cte, args, argPos = appendTimeRangeFilter(cte, args, argPos, req.Filter.TimeRange)
	cte += `
			GROUP BY d.doc_id, d.stock_code, d.company_name, d.doc_type, d.title, d.source_url, d.published_at, d.raw_data
		)
	`

	textArgPos := argPos
	patternArgPos := argPos + 1
	limitArgPos := argPos + 2
	query := cte + fmt.Sprintf(`
		SELECT stock_code, company_name, doc_type, title, source_url, published_at, raw_data, content,
			(
				(ts_rank_cd(
					to_tsvector('simple', concat_ws(' ', title, content, COALESCE(raw_data->>'keywords', ''))),
					websearch_to_tsquery('simple', $%d)
				) * 0.45) +
				(GREATEST(
					similarity(COALESCE(title, ''), $%d),
					similarity(COALESCE(content, ''), $%d),
					similarity(COALESCE(raw_data->>'keywords', ''), $%d)
				) * 0.2) +
				(COALESCE((
					SELECT COUNT(*)::float
					FROM unnest($%d::text[]) AS pattern
					WHERE lower(concat_ws(' ', title, content, COALESCE(raw_data->>'keywords', ''))) LIKE pattern ESCAPE '\\'
				), 0) * 0.35)
			) AS score
		FROM doc_view
		WHERE (
			to_tsvector('simple', concat_ws(' ', title, content, COALESCE(raw_data->>'keywords', ''))) @@ websearch_to_tsquery('simple', $%d)
			OR GREATEST(
				similarity(COALESCE(title, ''), $%d),
				similarity(COALESCE(content, ''), $%d),
				similarity(COALESCE(raw_data->>'keywords', ''), $%d)
			) > 0.05
			OR lower(concat_ws(' ', title, content, COALESCE(raw_data->>'keywords', ''))) LIKE ANY($%d) ESCAPE '\\'
		)
		ORDER BY score DESC, published_at DESC NULLS LAST
		LIMIT $%d
	`, textArgPos, textArgPos, textArgPos, textArgPos, patternArgPos, textArgPos, textArgPos, textArgPos, textArgPos, patternArgPos, limitArgPos)
	args = append(args, queryText, likePatterns, req.TopK)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make([]appmodel.Document, 0)
	for rows.Next() {
		var doc appmodel.Document
		var publishedAt interface{}
		var rawData []byte
		var aggregatedContent string
		var score float64

		if err := rows.Scan(&doc.StockCode, &doc.CompanyName, &doc.DocType, &doc.Title, &doc.SourceURL, &publishedAt, &rawData, &aggregatedContent, &score); err != nil {
			return nil, err
		}
		_ = score
		if publishedAt != nil {
			switch value := publishedAt.(type) {
			case time.Time:
				doc.Published = value.Format(time.RFC3339)
			default:
				doc.Published = fmt.Sprintf("%v", publishedAt)
			}
		}
		doc.Content = aggregatedContent
		mergeRawDataIntoDocument(&doc, rawData)
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}

func parsePublishedAt(value string) interface{} {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return nil
}

func mergeRawDataIntoDocument(doc *appmodel.Document, rawData []byte) {
	if doc == nil || len(rawData) == 0 {
		return
	}

	metadata := make(map[string]string)
	if err := json.Unmarshal(rawData, &metadata); err != nil {
		return
	}
	if doc.Content == "" {
		doc.Content = metadata["content"]
	}
	if doc.CompanyName == "" {
		doc.CompanyName = metadata["company_name"]
	}
	if doc.SourceURL == "" {
		doc.SourceURL = metadata["source_url"]
	}
	if doc.Published == "" {
		doc.Published = metadata["published_at"]
	}
	if keywords := strings.TrimSpace(metadata["keywords"]); keywords != "" {
		parts := strings.Split(keywords, ",")
		doc.Keywords = make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				doc.Keywords = append(doc.Keywords, part)
			}
		}
	}
	if doc.Content == "" {
		if rawContent := strings.TrimSpace(metadata["raw_content"]); rawContent != "" {
			doc.Content = rawContent
		}
	}
}

func parseOptionalInt(value string) interface{} {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return parsed
}

func mapDocTypes(docTypes []string) []string {
	mappedDocTypes := make([]string, 0, len(docTypes))
	for _, dt := range docTypes {
		dt = strings.TrimSpace(dt)
		if dt == "" {
			continue
		}
		mappedDocTypes = append(mappedDocTypes, dt)
		switch dt {
		case "report":
			mappedDocTypes = append(mappedDocTypes, "年报", "中报")
		case "announcement":
			mappedDocTypes = append(mappedDocTypes, "公告")
		}
	}
	return mappedDocTypes
}

func buildLikePatterns(terms []string) []string {
	patterns := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(strings.ToLower(term))
		if term == "" {
			continue
		}
		pattern := "%" + escapeLikeValue(term) + "%"
		if _, exists := seen[pattern]; exists {
			continue
		}
		seen[pattern] = struct{}{}
		patterns = append(patterns, pattern)
	}
	return patterns
}

func escapeLikeValue(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func appendTimeRangeFilter(query string, args []interface{}, argPos int, timeRange string) (string, []interface{}, int) {
	timeRange = strings.TrimSpace(strings.ToLower(timeRange))
	if timeRange == "" || timeRange == "latest" {
		return query, args, argPos
	}

	query += " AND d.published_at IS NOT NULL"
	switch timeRange {
	case "7d":
		query += " AND d.published_at >= NOW() - INTERVAL '7 days'"
	case "30d":
		query += " AND d.published_at >= NOW() - INTERVAL '30 days'"
	case "90d":
		query += " AND d.published_at >= NOW() - INTERVAL '90 days'"
	case "180d":
		query += " AND d.published_at >= NOW() - INTERVAL '180 days'"
	case "365d", "1y":
		query += " AND d.published_at >= NOW() - INTERVAL '365 days'"
	}
	return query, args, argPos
}
