-- 修复时间戳类型问题
-- 将 TIMESTAMP 类型改为 BIGINT 以兼容代码中的 Unix 时间戳

-- 1. 修改 conversations 表
ALTER TABLE conversations ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;
ALTER TABLE conversations ALTER COLUMN updated_at TYPE BIGINT USING EXTRACT(EPOCH FROM updated_at)::BIGINT;
ALTER TABLE conversations ALTER COLUMN last_message_at TYPE BIGINT USING EXTRACT(EPOCH FROM last_message_at)::BIGINT;
ALTER TABLE conversations ALTER COLUMN archived_at TYPE BIGINT USING EXTRACT(EPOCH FROM archived_at)::BIGINT;

-- 2. 修改 conversation_messages 表
ALTER TABLE conversation_messages ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;

-- 3. 修改 conversation_summaries 表
ALTER TABLE conversation_summaries ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;

-- 4. 修改 route_decisions 表
ALTER TABLE route_decisions ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;

-- 5. 修改 conversation_jobs 表
ALTER TABLE conversation_jobs ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;
ALTER TABLE conversation_jobs ALTER COLUMN updated_at TYPE BIGINT USING EXTRACT(EPOCH FROM updated_at)::BIGINT;
ALTER TABLE conversation_jobs ALTER COLUMN next_run_at TYPE BIGINT USING EXTRACT(EPOCH FROM next_run_at)::BIGINT;
ALTER TABLE conversation_jobs ALTER COLUMN locked_at TYPE BIGINT USING EXTRACT(EPOCH FROM locked_at)::BIGINT;

-- 6. 修改 auth_sessions 表
ALTER TABLE auth_sessions ALTER COLUMN last_active_at TYPE BIGINT USING EXTRACT(EPOCH FROM last_active_at)::BIGINT;
ALTER TABLE auth_sessions ALTER COLUMN expires_at TYPE BIGINT USING EXTRACT(EPOCH FROM expires_at)::BIGINT;
ALTER TABLE auth_sessions ALTER COLUMN revoked_at TYPE BIGINT USING EXTRACT(EPOCH FROM revoked_at)::BIGINT;
ALTER TABLE auth_sessions ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;

-- 7. 修改 documents 表
ALTER TABLE documents ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;
ALTER TABLE documents ALTER COLUMN updated_at TYPE BIGINT USING EXTRACT(EPOCH FROM updated_at)::BIGINT;
ALTER TABLE documents ALTER COLUMN published_at TYPE BIGINT USING EXTRACT(EPOCH FROM published_at)::BIGINT;

-- 8. 修改 document_chunks 表
ALTER TABLE document_chunks ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;

-- 9. 修改 users 表
ALTER TABLE users ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;
ALTER TABLE users ALTER COLUMN updated_at TYPE BIGINT USING EXTRACT(EPOCH FROM updated_at)::BIGINT;

-- 10. 修改 sessions 表
ALTER TABLE sessions ALTER COLUMN created_at TYPE BIGINT USING EXTRACT(EPOCH FROM created_at)::BIGINT;
ALTER TABLE sessions ALTER COLUMN expires_at TYPE BIGINT USING EXTRACT(EPOCH FROM expires_at)::BIGINT;

-- 11. 修改 conversation_contexts 表
ALTER TABLE conversation_contexts ALTER COLUMN updated_at TYPE BIGINT;

-- 12. 完成提示
DO $$
BEGIN
    RAISE NOTICE '时间戳类型修复完成！';
END $$;