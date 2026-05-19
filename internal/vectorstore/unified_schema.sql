-- 统一的数据库 Schema 设计
-- 目标：统一所有类型的数据（年报、公告、新闻）到一套 canonical schema

-- 1. 文档主表
CREATE TABLE IF NOT EXISTS documents (
    doc_id SERIAL PRIMARY KEY,
    stock_code VARCHAR(20) NOT NULL,
    company_name VARCHAR(100),
    doc_type VARCHAR(50) NOT NULL, -- 'report', 'announcement', 'news'
    title TEXT NOT NULL,
    source_url TEXT,
    published_at TIMESTAMP,
    source VARCHAR(100), -- 数据来源：'akshare', 'eastmoney', 'sina', 'manual', etc.
    raw_data JSONB, -- 原始数据，用于调试和扩展
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 2. 文档片段表（包含向量）
CREATE TABLE IF NOT EXISTS document_chunks (
    chunk_id SERIAL PRIMARY KEY,
    doc_id INTEGER,
    chunk_index INTEGER NOT NULL, -- 文档内的片段序号
    section_title VARCHAR(255), -- 章节标题（用于年报）
    page_no INTEGER, -- 页码（用于PDF）
    content TEXT NOT NULL,
    embedding VECTOR(2048), -- 向量维度根据实际模型调整
    metadata JSONB, -- 额外元数据：sentiment, keywords, etc.
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 添加外键约束
ALTER TABLE document_chunks ADD CONSTRAINT fk_document_chunks_doc_id
    FOREIGN KEY (doc_id) REFERENCES documents(doc_id) ON DELETE CASCADE;

-- 3. 索引设计
-- 文档表索引
CREATE INDEX IF NOT EXISTS idx_documents_stock_code ON documents(stock_code);
CREATE INDEX IF NOT EXISTS idx_documents_doc_type ON documents(doc_type);
CREATE INDEX IF NOT EXISTS idx_documents_published_at ON documents(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_documents_source ON documents(source);
CREATE INDEX IF NOT EXISTS idx_documents_company_name ON documents(company_name);

-- 文档片段表索引
CREATE INDEX IF NOT EXISTS idx_document_chunks_doc_id ON document_chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_document_chunks_chunk_index ON document_chunks(doc_id, chunk_index);

-- 向量索引（仅当向量维度 <= 2000 时创建）
-- 注意：ivfflat 索引需要足够的样本数据才能有效
DO $$
BEGIN
    -- 检查向量维度
    DECLARE vector_dim INTEGER;
    SELECT data_type INTO vector_dim
    FROM information_schema.columns
    WHERE table_name = 'document_chunks' AND column_name = 'embedding';
    
    -- 提取维度信息
    vector_dim := substring(vector_dim from 'vector\((\d+)\)')::INTEGER;
    
    -- 只有当向量维度 <= 2000 时才创建向量索引
    IF vector_dim <= 2000 THEN
        CREATE INDEX IF NOT EXISTS idx_document_chunks_embedding
        ON document_chunks
        USING ivfflat (embedding vector_cosine_ops)
        WITH (lists = 100);
    END IF;
END $$;

-- 4. 触发器：自动更新 updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_documents_updated_at
    BEFORE UPDATE ON documents
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- 5. 数据迁移函数
-- 从 pdf_files 迁移到 documents
CREATE OR REPLACE FUNCTION migrate_pdf_files_to_documents()
RETURNS INTEGER AS $$
DECLARE
    migrated_count INTEGER := 0;
BEGIN
    -- 迁移 PDF 文件信息到 documents 表
    INSERT INTO documents (stock_code, company_name, doc_type, title, source_url, published_at, source, raw_data)
    SELECT 
        COALESCE(stock_code, 'UNKNOWN'),
        company,
        'report',
        filename,
        NULL,
        created_at,
        'pdf_processor',
        jsonb_build_object(
            'file_id', id,
            'report_type', report_type,
            'file_path', file_path
        )
    FROM pdf_files
    ON CONFLICT DO NOTHING;
    
    GET DIAGNOSTICS migrated_count = ROW_COUNT;
    
    RETURN migrated_count;
END;
$$ LANGUAGE plpgsql;

-- 从 pdf_documents 迁移到 document_chunks
CREATE OR REPLACE FUNCTION migrate_pdf_documents_to_chunks()
RETURNS INTEGER AS $$
DECLARE
    migrated_count INTEGER := 0;
    doc_id INTEGER;
BEGIN
    -- 首先确保文档记录存在
    INSERT INTO documents (stock_code, company_name, doc_type, title, source_url, published_at, source, raw_data)
    SELECT 
        COALESCE(stock_code, 'UNKNOWN'),
        company,
        'report',
        filename,
        NULL,
        created_at,
        'pdf_processor',
        jsonb_build_object(
            'chunk_id', chunk_id,
            'file_id', file_id,
            'page', page,
            'report_type', report_type
        )
    FROM pdf_documents
    ON CONFLICT DO NOTHING;
    
    -- 迁移文档片段
    INSERT INTO document_chunks (doc_id, chunk_index, section_title, page_no, content, embedding, metadata)
    SELECT 
        d.doc_id,
        pd.chunk_index,
        NULL, -- section_title 从 pdf_documents 中没有
        pd.page,
        pd.text,
        pd.embedding,
        jsonb_build_object(
            'chunk_id', pd.chunk_id,
            'file_id', pd.file_id,
            'report_type', pd.report_type
        )::jsonb
    FROM pdf_documents pd
    JOIN documents d ON d.stock_code = pd.stock_code AND d.doc_type = 'report'
    ON CONFLICT DO NOTHING;
    
    GET DIAGNOSTICS migrated_count = ROW_COUNT;
    
    RETURN migrated_count;
END;
$$ LANGUAGE plpgsql;

-- 从 news_documents 迁移到 documents
CREATE OR REPLACE FUNCTION migrate_news_documents_to_documents()
RETURNS INTEGER AS $$
DECLARE
    migrated_count INTEGER := 0;
BEGIN
    -- 迁移新闻文档到 documents 表
    INSERT INTO documents (stock_code, company_name, doc_type, title, source_url, published_at, source, raw_data)
    SELECT 
        stock_symbol,
        NULL, -- company_name 从 news_documents 中没有
        'news',
        title,
        NULL, -- source_url 从 news_documents 中没有
        time_published,
        source,
        raw_data
    FROM news_documents
    ON CONFLICT DO NOTHING;
    
    GET DIAGNOSTICS migrated_count = ROW_COUNT;
    
    RETURN migrated_count;
END;
$$ LANGUAGE plpgsql;

-- 从 news_vectors 迁移到 document_chunks
CREATE OR REPLACE FUNCTION migrate_news_vectors_to_chunks()
RETURNS INTEGER AS $$
DECLARE
    migrated_count INTEGER := 0;
BEGIN
    -- 迁移新闻向量到 document_chunks 表
    INSERT INTO document_chunks (doc_id, chunk_index, section_title, page_no, content, embedding, metadata)
    SELECT 
        d.doc_id,
        ROW_NUMBER() OVER (PARTITION BY d.doc_id ORDER BY nv.id),
        NULL, -- section_title
        NULL, -- page_no
        nv.content,
        nv.embedding,
        jsonb_build_object(
            'news_source', nv.news_source,
            'news_time', nv.news_time,
            'sentiment_score', nv.sentiment_score,
            'sentiment_label', nv.sentiment_label,
            'chunk_type', nv.chunk_type
        )::jsonb
    FROM news_vectors nv
    JOIN documents d ON d.stock_code = nv.stock_symbol AND d.doc_type = 'news'
    ON CONFLICT DO NOTHING;
    
    GET DIAGNOSTICS migrated_count = ROW_COUNT;
    
    RETURN migrated_count;
END;
$$ LANGUAGE plpgsql;

-- 从 financial_documents 迁移到 documents
CREATE OR REPLACE FUNCTION migrate_financial_documents_to_documents()
RETURNS INTEGER AS $$
DECLARE
    migrated_count INTEGER := 0;
BEGIN
    -- 迁移财报文档到 documents 表
    INSERT INTO documents (stock_code, company_name, doc_type, title, source_url, published_at, source, raw_data)
    SELECT 
        stock_symbol,
        NULL, -- company_name 从 financial_documents 中没有
        'report',
        title,
        NULL, -- source_url 从 financial_documents 中没有
        report_date,
        source,
        raw_data
    FROM financial_documents
    ON CONFLICT DO NOTHING;
    
    GET DIAGNOSTICS migrated_count = ROW_COUNT;
    
    RETURN migrated_count;
END;
$$ LANGUAGE plpgsql;

-- 从 financial_vectors 迁移到 document_chunks
CREATE OR REPLACE FUNCTION migrate_financial_vectors_to_chunks()
RETURNS INTEGER AS $$
DECLARE
    migrated_count INTEGER := 0;
BEGIN
    -- 迁移财报向量到 document_chunks 表
    INSERT INTO document_chunks (doc_id, chunk_index, section_title, page_no, content, embedding, metadata)
    SELECT 
        d.doc_id,
        ROW_NUMBER() OVER (PARTITION BY d.doc_id ORDER BY fv.id),
        NULL, -- section_title
        NULL, -- page_no
        fv.content,
        fv.embedding,
        jsonb_build_object(
            'report_type', fv.report_type,
            'report_date', fv.report_date,
            'news_source', fv.news_source,
            'chunk_type', fv.chunk_type
        )::jsonb
    FROM financial_vectors fv
    JOIN documents d ON d.stock_code = fv.stock_symbol AND d.doc_type = 'report'
    ON CONFLICT DO NOTHING;
    
    GET DIAGNOSTICS migrated_count = ROW_COUNT;
    
    RETURN migrated_count;
END;
$$ LANGUAGE plpgsql;

-- 6. 统一迁移函数
CREATE OR REPLACE FUNCTION migrate_all_data()
RETURNS TABLE(
    table_name TEXT,
    migrated_count INTEGER
) AS $$
BEGIN
    -- 迁移 PDF 数据
    RETURN QUERY
    SELECT 'pdf_files', migrate_pdf_files_to_documents()
    UNION ALL
    SELECT 'pdf_documents', migrate_pdf_documents_to_chunks()
    UNION ALL
    SELECT 'news_documents', migrate_news_documents_to_documents()
    UNION ALL
    SELECT 'news_vectors', migrate_news_vectors_to_chunks()
    UNION ALL
    SELECT 'financial_documents', migrate_financial_documents_to_documents()
    UNION ALL
    SELECT 'financial_vectors', migrate_financial_vectors_to_chunks();
END;
$$ LANGUAGE plpgsql;

-- 7. 查询函数
-- 根据股票代码和时间范围查询文档
CREATE OR REPLACE FUNCTION query_documents(
    p_stock_code VARCHAR(20),
    p_doc_types VARCHAR(50)[],
    p_time_range VARCHAR(20),
    p_limit INTEGER DEFAULT 100
)
RETURNS TABLE(
    doc_id INTEGER,
    stock_code VARCHAR(20),
    company_name VARCHAR(100),
    doc_type VARCHAR(50),
    title TEXT,
    source_url TEXT,
    published_at TIMESTAMP,
    source VARCHAR(100),
    created_at TIMESTAMP
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        d.doc_id,
        d.stock_code,
        d.company_name,
        d.doc_type,
        d.title,
        d.source_url,
        d.published_at,
        d.source,
        d.created_at
    FROM documents d
    WHERE 
        (p_stock_code IS NULL OR d.stock_code = p_stock_code)
        AND (p_doc_types IS NULL OR d.doc_type = ANY(p_doc_types))
        AND (
            p_time_range IS NULL OR 
            p_time_range = '' OR 
            p_time_range = 'latest' OR
            d.published_at >= CURRENT_DATE - (
                CASE p_time_range
                    WHEN '7d' THEN INTERVAL '7 days'
                    WHEN '30d' THEN INTERVAL '30 days'
                    WHEN '90d' THEN INTERVAL '90 days'
                    WHEN '180d' THEN INTERVAL '180 days'
                    WHEN '365d' THEN INTERVAL '365 days'
                    WHEN '1y' THEN INTERVAL '1 year'
                    ELSE INTERVAL '100 years'
                END
            )
        )
    ORDER BY d.published_at DESC
    LIMIT p_limit;
END;
$$ LANGUAGE plpgsql;

-- 向量相似度搜索函数
CREATE OR REPLACE FUNCTION search_similar_chunks(
    p_query_vector VECTOR(2048),
    p_stock_code VARCHAR(20),
    p_doc_types VARCHAR(50)[],
    p_time_range VARCHAR(20),
    p_limit INTEGER DEFAULT 10,
    p_similarity_threshold FLOAT DEFAULT 0.5
)
RETURNS TABLE(
    chunk_id INTEGER,
    doc_id INTEGER,
    stock_code VARCHAR(20),
    doc_type VARCHAR(50),
    title TEXT,
    section_title VARCHAR(255),
    page_no INTEGER,
    content TEXT,
    similarity FLOAT,
    metadata JSONB
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        dc.chunk_id,
        dc.doc_id,
        d.stock_code,
        d.doc_type,
        d.title,
        dc.section_title,
        dc.page_no,
        dc.content,
        1 - (dc.embedding <=> p_query_vector) AS similarity, -- 余弦相似度
        dc.metadata
    FROM document_chunks dc
    JOIN documents d ON d.doc_id = dc.doc_id
    WHERE 
        (p_stock_code IS NULL OR d.stock_code = p_stock_code)
        AND (p_doc_types IS NULL OR d.doc_type = ANY(p_doc_types))
        AND (
            p_time_range IS NULL OR 
            p_time_range = '' OR 
            p_time_range = 'latest' OR
            d.published_at >= CURRENT_DATE - (
                CASE p_time_range
                    WHEN '7d' THEN INTERVAL '7 days'
                    WHEN '30d' THEN INTERVAL '30 days'
                    WHEN '90d' THEN INTERVAL '90 days'
                    WHEN '180d' THEN INTERVAL '180 days'
                    WHEN '365d' THEN INTERVAL '365 days'
                    WHEN '1y' THEN INTERVAL '1 year'
                    ELSE INTERVAL '100 years'
                END
            )
        )
        AND (1 - (dc.embedding <=> p_query_vector)) >= p_similarity_threshold
    ORDER BY similarity DESC
    LIMIT p_limit;
END;
$$ LANGUAGE plpgsql;