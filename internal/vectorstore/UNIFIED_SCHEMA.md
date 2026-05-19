# 统一数据库 Schema 设计

## 概述

本项目已经统一了所有数据类型的数据库存储结构，解决了之前"PDF一套、新闻一套、Go导入又一套"的结构性问题。

## 统一 Schema 结构

### 1. 文档主表 (`documents`)

```sql
CREATE TABLE documents (
    doc_id SERIAL PRIMARY KEY,
    stock_code VARCHAR(20) NOT NULL,      -- 股票代码
    company_name VARCHAR(100),            -- 公司名称
    doc_type VARCHAR(50) NOT NULL,       -- 文档类型: 'report', 'announcement', 'news'
    title TEXT NOT NULL,                 -- 文档标题
    source_url TEXT,                     -- 来源URL
    published_at TIMESTAMP,              -- 发布时间
    source VARCHAR(100),                 -- 数据来源: 'akshare', 'eastmoney', 'sina', 'manual', etc.
    raw_data JSONB,                     -- 原始数据，用于调试和扩展
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**索引**：
- `idx_documents_stock_code` - 按股票代码索引
- `idx_documents_doc_type` - 按文档类型索引
- `idx_documents_published_at` - 按发布时间索引
- `idx_documents_source` - 按数据来源索引

### 2. 文档片段表 (`document_chunks`)

```sql
CREATE TABLE document_chunks (
    chunk_id SERIAL PRIMARY KEY,
    doc_id INTEGER REFERENCES documents(doc_id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,        -- 文档内的片段序号
    section_title VARCHAR(255),         -- 章节标题（用于年报）
    page_no INTEGER,                     -- 页码（用于PDF）
    content TEXT NOT NULL,               -- 片段内容
    embedding VECTOR(2048),              -- 向量嵌入
    metadata JSONB,                      -- 额外元数据: sentiment, keywords, etc.
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**索引**：
- `idx_document_chunks_doc_id` - 按文档ID索引
- `idx_document_chunks_chunk_index` - 按文档ID和片段序号索引
- `idx_document_chunks_embedding` - 向量相似度索引（当维度 <= 2000 时）

## 数据类型映射

| 数据类型 | doc_type | 数据来源 | 示例 |
|---------|----------|----------|------|
| 年报 | `report` | `financial_reports.py` | 财报数据、年报摘要 |
| 公告 | `announcement` | `stock_news_collector.py` | 公司公告、重要通知 |
| 新闻 | `news` | `stock_news_collector.py` | 财经新闻、市场动态 |

## 元数据结构

### 文档表元数据 (`raw_data`)

**年报数据**：
```json
{
  "stock_symbol": "600519",
  "report_type": "年报",
  "title": "贵州茅台2025年年报摘要",
  "summary": "公司业绩稳健增长...",
  "source": "akshare",
  "report_date": "2026-03-01",
  "financial_metrics": {
    "revenue": 1234.56,
    "net_profit": 456.78,
    "growth_rate": 15.2
  }
}
```

**新闻数据**：
```json
{
  "stock_symbol": "300750",
  "title": "宁德时代发布最新公告",
  "summary": "公司宣布...",
  "source": "东方财富",
  "time_published": "2026-04-07T12:00:00",
  "overall_sentiment_score": 0.8,
  "overall_sentiment_label": "positive"
}
```

### 文档片段元数据 (`metadata`)

**年报片段**：
```json
{
  "report_type": "年报",
  "report_date": "2026-03-01",
  "source": "akshare",
  "chunk_type": "financial_metrics"
}
```

**新闻片段**：
```json
{
  "news_source": "东方财富",
  "news_time": "2026-04-07T12:00:00",
  "sentiment_score": 0.8,
  "sentiment_label": "positive",
  "chunk_type": "news_content"
}
```

## 数据迁移

### 迁移函数

SQL脚本提供了完整的迁移函数：

```sql
-- 迁移所有数据
SELECT * FROM migrate_all_data();

-- 单独迁移特定类型
SELECT migrate_pdf_files_to_documents();
SELECT migrate_news_documents_to_documents();
SELECT migrate_financial_documents_to_documents();
```

### Go 迁移工具

```bash
# 编译并运行迁移工具
cd cmd/migrate
go run main.go
```

## 查询函数

### 1. 文档查询

```sql
-- 查询特定股票和时间范围的文档
SELECT * FROM query_documents(
    '300750',                    -- stock_code
    ARRAY['news', 'announcement'], -- doc_types
    '30d',                       -- time_range
    100                          -- limit
);
```

### 2. 向量相似度搜索

```sql
-- 查询相似的文档片段
SELECT * FROM search_similar_chunks(
    '[0.1, 0.2, ...]',          -- query_vector
    '300750',                   -- stock_code
    ARRAY['news', 'report'],     -- doc_types
    '30d',                      -- time_range
    10,                         -- limit
    0.5                         -- similarity_threshold
);
```

## Python 脚本更新

### `financial_reports.py`

- ✅ 使用统一的 `documents` 表存储财报文档
- ✅ 使用统一的 `document_chunks` 表存储向量片段
- ✅ `doc_type` 设置为 `'report'`

### `stock_news_collector.py`

- ✅ 使用统一的 `documents` 表存储新闻文档
- ✅ 使用统一的 `document_chunks` 表存储向量片段
- ✅ `doc_type` 设置为 `'news'` 或 `'announcement'`

## Go 检索层更新

### `pgvector_store.go`

需要更新以支持统一schema：

```go
// 更新后的表结构
type Document struct {
    DocID       int
    StockCode   string
    CompanyName string
    DocType     string  // 'report', 'announcement', 'news'
    Title       string
    SourceURL   string
    PublishedAt time.Time
    Source      string
    RawData     json.RawMessage
}

type DocumentChunk struct {
    ChunkID      int
    DocID        int
    ChunkIndex   int
    SectionTitle string
    PageNo       int
    Content      string
    Embedding    []float32
    Metadata     json.RawMessage
}
```

## 优势

### 1. 统一检索
- 所有数据类型使用相同的查询接口
- 简化检索逻辑，提高代码可维护性

### 2. 一致性
- 统一的 citation 格式
- 一致的元数据结构
- 标准化的时间范围过滤

### 3. 可扩展性
- JSONB 字段支持灵活的元数据扩展
- 统一的索引策略
- 易于添加新的数据类型

### 4. 面试友好
- 清晰的数据模型
- 统一的技术栈
- 易于解释的架构设计

## 向后兼容

旧的数据表仍然保留，可以逐步迁移：

- `pdf_files` → `documents` (doc_type='report')
- `pdf_documents` → `document_chunks`
- `news_documents` → `documents` (doc_type='news')
- `news_vectors` → `document_chunks`
- `financial_documents` → `documents` (doc_type='report')
- `financial_vectors` → `document_chunks`

## 下一步

1. ✅ 创建统一schema SQL文件
2. ✅ 更新Python脚本使用统一schema
3. ⏳ 更新Go检索层支持统一schema
4. ⏳ 执行数据迁移
5. ⏳ 测试统一检索功能
6. ⏳ 清理旧表（可选）

## 注意事项

1. **向量维度**：确保所有向量使用相同的维度
2. **时间格式**：统一使用 ISO 8601 格式
3. **数据类型**：严格遵循 `doc_type` 的枚举值
4. **外键约束**：`document_chunks.doc_id` 必须引用有效的 `documents.doc_id`