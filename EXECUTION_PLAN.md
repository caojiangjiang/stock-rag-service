# 统一 Schema 执行计划

## 当前状态

### ✅ 已完成
1. **统一 Schema 设计**
   - 创建 `unified_schema.sql` 文件
   - 设计 `documents` 和 `document_chunks` 两张核心表
   - 提供完整的数据迁移函数

2. **Python 脚本更新**
   - 更新 `financial_reports.py` 使用统一 schema
   - 更新 `stock_news_collector.py` 使用统一 schema
   - 所有数据写入统一的表结构

3. **迁移工具**
   - 创建 `cmd/migrate/main.go` 迁移工具
   - 支持执行统一 schema 和数据迁移

4. **文档完善**
   - 创建 `UNIFIED_SCHEMA.md` 详细说明文档
   - 创建 `SCHEMA_IMPROVEMENTS.md` 改进总结文档

### ⏳ 待完成
1. **Go 检索层更新**
   - 更新 `pgvector_store.go` 支持统一 schema
   - 更新相关模型和接口

2. **数据迁移执行**
   - 运行迁移工具
   - 验证数据完整性

3. **测试验证**
   - 更新单元测试
   - 验证检索功能

## 执行步骤

### 第一步：更新 Go 检索层

#### 1.1 更新数据模型

**文件**：`internal/model/document.go` (创建或更新)

```go
package model

import "time"

// Document 统一的文档模型
type Document struct {
    DocID       int
    StockCode   string
    CompanyName string
    DocType     string  // 'report', 'announcement', 'news'
    Title       string
    SourceURL   string
    PublishedAt time.Time
    Source      string
    RawData     []byte  // JSONB data
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// DocumentChunk 统一的文档片段模型
type DocumentChunk struct {
    ChunkID      int
    DocID        int
    ChunkIndex   int
    SectionTitle string
    PageNo       int
    Content      string
    Embedding    []float32
    Metadata     []byte  // JSONB data
    CreatedAt    time.Time
}
```

#### 1.2 更新 vectorstore 接口

**文件**：`internal/vectorstore/pgvector_store.go`

需要修改的部分：
- `Upsert` 方法：使用统一的 `documents` 和 `document_chunks` 表
- `Search` 方法：支持统一 schema 的查询
- `initTables` 方法：使用统一 schema

#### 1.3 更新检索器

**文件**：`internal/eino/retriever/retriever.go`

需要修改的部分：
- `matchDocuments` 方法：适配统一 schema 的字段名
- `matchesTimeRange` 方法：确保时间过滤逻辑一致

### 第二步：执行数据迁移

#### 2.1 准备数据库

```bash
# 确保数据库服务运行
docker compose ps postgres_server

# 如果没有运行，启动服务
docker compose up -d postgres_server
```

#### 2.2 运行迁移工具

```bash
# 进入项目目录
cd /Users/qudian/Downloads/trae_projects/job/projects/stock_rag

# 编译并运行迁移工具
cd cmd/migrate
go run main.go
```

#### 2.3 验证迁移结果

```bash
# 连接到数据库
docker compose exec postgres_server psql -U postgres -d stock_rag

# 检查表结构
\d documents
\d document_chunks

# 检查数据数量
SELECT COUNT(*) FROM documents;
SELECT COUNT(*) FROM document_chunks;

# 按文档类型统计
SELECT doc_type, COUNT(*) FROM documents GROUP BY doc_type;

# 退出数据库
\q
```

### 第三步：测试验证

#### 3.1 运行单元测试

```bash
# 运行所有测试
cd /Users/qudian/Downloads/trae_projects/job/projects/stock_rag
go test ./...

# 运行特定包的测试
go test ./internal/eino/retriever/... -v
go test ./internal/vectorstore/... -v
```

#### 3.2 测试检索功能

```bash
# 启动应用
go run cmd/server/main.go

# 测试检索API
curl -X POST http://localhost:8080/api/retrieve \
  -H "Content-Type: application/json" \
  -d '{
    "question": "宁德时代港股上市进展怎么样？",
    "stock_code": "300750",
    "doc_types": ["announcement", "news"],
    "time_range": "30d",
    "top_k": 2
  }'
```

#### 3.3 验证数据完整性

```sql
-- 连接到数据库
docker compose exec postgres_server psql -U postgres -d stock_rag

-- 检查文档和片段的关联
SELECT 
    d.doc_id,
    d.stock_code,
    d.doc_type,
    d.title,
    COUNT(dc.chunk_id) as chunk_count
FROM documents d
LEFT JOIN document_chunks dc ON d.doc_id = dc.doc_id
GROUP BY d.doc_id, d.stock_code, d.doc_type, d.title
ORDER BY d.doc_id;

-- 检查向量数据
SELECT 
    d.doc_type,
    COUNT(dc.chunk_id) as total_chunks,
    COUNT(CASE WHEN dc.embedding IS NOT NULL THEN 1 END) as vectors_count
FROM documents d
LEFT JOIN document_chunks dc ON d.doc_id = dc.doc_id
GROUP BY d.doc_type;

-- 测试查询函数
SELECT * FROM query_documents('300750', ARRAY['news', 'announcement'], '30d', 10);

-- 退出数据库
\q
```

### 第四步：清理旧表（可选）

⚠️ **警告**：在确认新系统运行正常之前，不要删除旧表！

```sql
-- 连接到数据库
docker compose exec postgres_server psql -U postgres -d stock_rag

-- 备份旧表（可选）
CREATE TABLE pdf_files_backup AS SELECT * FROM pdf_files;
CREATE TABLE pdf_documents_backup AS SELECT * FROM pdf_documents;
CREATE TABLE news_documents_backup AS SELECT * FROM news_documents;
CREATE TABLE news_vectors_backup AS SELECT * FROM news_vectors;
CREATE TABLE financial_documents_backup AS SELECT * FROM financial_documents;
CREATE TABLE financial_vectors_backup AS SELECT * FROM financial_vectors;

-- 删除旧表（谨慎操作！）
DROP TABLE IF EXISTS pdf_files CASCADE;
DROP TABLE IF EXISTS pdf_documents CASCADE;
DROP TABLE IF EXISTS news_documents CASCADE;
DROP TABLE IF EXISTS news_vectors CASCADE;
DROP TABLE IF EXISTS financial_documents CASCADE;
DROP TABLE IF EXISTS financial_vectors CASCADE;

-- 删除旧表相关的索引（如果有）
DROP INDEX IF EXISTS idx_news_docs_symbol;
DROP INDEX IF EXISTS idx_news_docs_time;
DROP INDEX IF EXISTS idx_fin_docs_symbol;
DROP INDEX IF EXISTS idx_fin_docs_type;
DROP INDEX IF EXISTS idx_fin_docs_date;
DROP INDEX IF EXISTS idx_news_vectors_symbol;
DROP INDEX IF EXISTS idx_fin_vectors_symbol;
DROP INDEX IF EXISTS idx_fin_vectors_embedding;

-- 退出数据库
\q
```

## 预期结果

### 成功标志

1. **Schema 统一**
   - ✅ 所有数据存储在 `documents` 和 `document_chunks` 表中
   - ✅ 不同数据类型通过 `doc_type` 字段区分

2. **检索功能正常**
   - ✅ 所有类型的文档都能被检索到
   - ✅ 向量搜索和关键词搜索都正常工作
   - ✅ 时间过滤逻辑正确

3. **数据完整性**
   - ✅ 文档和片段的关联正确
   - ✅ 向量数据完整
   - ✅ 元数据保留完整

4. **性能良好**
   - ✅ 查询响应时间合理
   - ✅ 索引有效工作
   - ✅ 资源使用正常

## 风险和应对

### 潜在风险

1. **数据迁移失败**
   - **应对**：先备份旧表，分步迁移
   - **回滚**：可以从备份表恢复数据

2. **性能下降**
   - **应对**：优化索引，调整查询语句
   - **监控**：密切关注查询性能

3. **兼容性问题**
   - **应对**：保留旧表一段时间
   - **测试**：充分测试后再清理旧表

### 回滚方案

如果迁移出现问题，可以按以下步骤回滚：

1. **停止新系统**
   ```bash
   # 停止应用服务
   docker compose down
   ```

2. **恢复旧表**
   ```sql
   -- 从备份恢复
   DROP TABLE IF EXISTS documents CASCADE;
   DROP TABLE IF EXISTS document_chunks CASCADE;
   
   -- 恢复旧表（如果已删除）
   CREATE TABLE pdf_files AS SELECT * FROM pdf_files_backup;
   CREATE TABLE pdf_documents AS SELECT * FROM pdf_documents_backup;
   CREATE TABLE news_documents AS SELECT * FROM news_documents_backup;
   CREATE TABLE news_vectors AS SELECT * FROM news_vectors_backup;
   CREATE TABLE financial_documents AS SELECT * FROM financial_documents_backup;
   CREATE TABLE financial_vectors AS SELECT * FROM financial_vectors_backup;
   ```

3. **恢复旧代码**
   ```bash
   # 回退到迁移前的代码版本
   git checkout <commit-hash>
   ```

## 时间估算

| 步骤 | 预计时间 | 依赖 |
|------|---------|------|
| 更新 Go 检索层 | 2-3 小时 | - |
| 执行数据迁移 | 1-2 小时 | Go 检索层更新 |
| 测试验证 | 2-3 小时 | 数据迁移完成 |
| 清理旧表 | 1 小时 | 测试验证通过 |
| **总计** | **6-9 小时** | - |

## 总结

通过这个执行计划，我们可以：

1. ✅ **统一数据模型**：所有数据使用相同的表结构
2. ✅ **简化维护**：减少重复代码，提高可维护性
3. ✅ **提升性能**：优化索引和查询策略
4. ✅ **便于扩展**：新增数据类型更加简单
5. ✅ **面试友好**：清晰统一的架构设计

这个改进将显著提升项目的整体质量和可维护性。

---

**创建时间**：2026-04-07  
**状态**：待执行  
**优先级**：高