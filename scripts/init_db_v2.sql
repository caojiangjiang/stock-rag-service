-- Stock RAG 数据库初始化脚本 v2
-- 兼容现有表结构

-- 1. 启用 pgvector 扩展
CREATE EXTENSION IF NOT EXISTS vector;

-- 2. 文档主表
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

-- 3. 文档片段表（包含向量）
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
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint 
        WHERE conname = 'fk_document_chunks_doc_id'
    ) THEN
        ALTER TABLE document_chunks ADD CONSTRAINT fk_document_chunks_doc_id
            FOREIGN KEY (doc_id) REFERENCES documents(doc_id) ON DELETE CASCADE;
    END IF;
END $$;

-- 4. 文档表索引
CREATE INDEX IF NOT EXISTS idx_documents_stock_code ON documents(stock_code);
CREATE INDEX IF NOT EXISTS idx_documents_doc_type ON documents(doc_type);
CREATE INDEX IF NOT EXISTS idx_documents_published_at ON documents(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_documents_source ON documents(source);
CREATE INDEX IF NOT EXISTS idx_documents_company_name ON documents(company_name);

-- 5. 文档片段表索引
CREATE INDEX IF NOT EXISTS idx_document_chunks_doc_id ON document_chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_document_chunks_chunk_index ON document_chunks(doc_id, chunk_index);

-- 6. 触发器：自动更新 updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

DROP TRIGGER IF EXISTS update_documents_updated_at ON documents;
CREATE TRIGGER update_documents_updated_at
    BEFORE UPDATE ON documents
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- 7. 用户表（兼容现有结构）
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    email TEXT UNIQUE NOT NULL,
    password TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 8. 会话表（兼容现有结构）
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 9. 认证会话表（新增）
CREATE TABLE IF NOT EXISTS auth_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_token_hash VARCHAR(255),
    jwt_jti VARCHAR(128),
    client_info JSONB,
    ip TEXT,
    user_agent TEXT,
    last_active_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 10. 认证会话表索引
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_id ON auth_sessions(user_id, last_active_at DESC);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_revoked ON auth_sessions(revoked_at);

-- 11. 对话表（使用 TEXT 类型 ID）
CREATE TABLE IF NOT EXISTS conversations (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(255),
    status VARCHAR(20) DEFAULT 'active',
    mode_hint VARCHAR(20),
    last_route_mode VARCHAR(20),
    active_summary_version INT DEFAULT 0,
    message_count INT DEFAULT 0,
    total_input_tokens INT DEFAULT 0,
    total_output_tokens INT DEFAULT 0,
    context_token_budget INT DEFAULT 8000,
    last_message_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    archived_at TIMESTAMP
);

-- 12. 对话表索引
CREATE INDEX IF NOT EXISTS idx_conversations_user_id ON conversations(user_id, last_message_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversations_status ON conversations(status, updated_at DESC);

-- 13. 消息表（使用 TEXT 类型 ID）
CREATE TABLE IF NOT EXISTS conversation_messages (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    seq BIGINT NOT NULL,
    role VARCHAR(20) NOT NULL,
    message_type VARCHAR(30) DEFAULT 'chat',
    content TEXT NOT NULL DEFAULT '',
    content_json JSONB,
    parent_message_id TEXT,
    route_mode VARCHAR(20),
    tool_name VARCHAR(100),
    tool_calls JSONB,
    tool_results JSONB,
    citations JSONB,
    model_name VARCHAR(100),
    input_tokens INT DEFAULT 0,
    output_tokens INT DEFAULT 0,
    latency_ms INT DEFAULT 0,
    status VARCHAR(20) DEFAULT 'received',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(conversation_id, seq)
);

-- 14. 消息表索引
CREATE INDEX IF NOT EXISTS idx_messages_conversation_id ON conversation_messages(conversation_id, created_at);
CREATE INDEX IF NOT EXISTS idx_messages_user_id ON conversation_messages(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_route_mode ON conversation_messages(route_mode, created_at DESC);

-- 15. 对话摘要表（使用 TEXT 类型 ID）
CREATE TABLE IF NOT EXISTS conversation_summaries (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    version INT NOT NULL,
    summary_type VARCHAR(30) DEFAULT 'rolling',
    summary_text TEXT,
    summary_json JSONB,
    source_start_seq BIGINT,
    source_end_seq BIGINT,
    token_count INT DEFAULT 0,
    model_name VARCHAR(100),
    status VARCHAR(20) DEFAULT 'pending',
    created_by VARCHAR(30) DEFAULT 'async_worker',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(conversation_id, version)
);

-- 16. 对话摘要表索引
CREATE INDEX IF NOT EXISTS idx_summaries_conversation_id ON conversation_summaries(conversation_id, status, version DESC);

-- 17. 路由决策表（使用 TEXT 类型 ID）
CREATE TABLE IF NOT EXISTS route_decisions (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    message_id TEXT NOT NULL,
    classifier_type VARCHAR(20) DEFAULT 'llm',
    classifier_version VARCHAR(50),
    predicted_mode VARCHAR(20) NOT NULL,
    confidence DECIMAL(5,4) DEFAULT 0,
    reason TEXT,
    candidates JSONB,
    selected BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 18. 路由决策表索引
CREATE INDEX IF NOT EXISTS idx_route_message_id ON route_decisions(message_id);
CREATE INDEX IF NOT EXISTS idx_route_conversation_id ON route_decisions(conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_route_predicted_mode ON route_decisions(predicted_mode, created_at DESC);

-- 19. 对话任务表（使用 TEXT 类型 ID）
CREATE TABLE IF NOT EXISTS conversation_jobs (
    id TEXT PRIMARY KEY,
    job_type VARCHAR(30) NOT NULL,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    message_id TEXT,
    payload JSONB,
    status VARCHAR(20) DEFAULT 'pending',
    attempt_count INT DEFAULT 0,
    max_attempts INT DEFAULT 3,
    next_run_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    locked_at TIMESTAMP,
    last_error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 20. 对话任务表索引
CREATE INDEX IF NOT EXISTS idx_jobs_status ON conversation_jobs(status, next_run_at);
CREATE INDEX IF NOT EXISTS idx_jobs_conversation_id ON conversation_jobs(conversation_id, created_at DESC);

-- 21. 对话上下文表（使用 TEXT 类型 ID）
CREATE TABLE IF NOT EXISTS conversation_contexts (
    conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    context JSONB NOT NULL,
    updated_at BIGINT NOT NULL
);

-- 22. 插入默认用户（用于测试）
INSERT INTO users (id, username, email, password)
VALUES ('test_user_id', 'test_user', 'test@example.com', 'test_password')
ON CONFLICT (username) DO NOTHING;

-- 23. 完成提示
DO $$
BEGIN
    RAISE NOTICE '数据库初始化完成！';
    RAISE NOTICE '默认用户: test_user / test_password';
END $$;