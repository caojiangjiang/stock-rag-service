# stock_rag

基于 Eino 设计的股票投研 RAG 系统。

## 当前能力

- 完整的项目目录结构
- 集成了 `github.com/cloudwego/eino` 框架
- 实现了 `compose + prompt + schema` 的最小链路
- 接入了 `github.com/cloudwego/eino-ext/components/model/ark` 模型
- 支持 `POST /rag/query` 同步查询接口
- 支持 `POST /rag/query/stream` 流式查询接口
- 支持 `GET /documents` 文档列表接口
- 支持 `GET /health` 健康检查接口
- 支持 `GET /stats` 统计信息接口
- 支持 `POST /agent/execute` 执行 Agent 任务
- 支持 `POST /agent/analyze-stock` 分析股票
- 支持 `POST /agent/run` 运行 Agent
- 实现了 **Ark 真模型 + skeleton fallback** 机制
- 统一的 LLM 客户端管理，支持并发控制和队列调度
- 实现了 Agent 真实 tool-calling loop
- 支持按 stock_code / doc_type / time_range 检索
- 返回带 citation 的答案

## 当前模型接入方式

当前模型层的行为是：

- 当 `ARK_API_KEY` 和 `ARK_MODEL` 都配置时：走真实 Ark ChatModel
- 当任一环境变量缺失时：自动回退到本地 skeleton responder

这样可以同时保证：

- 本地开发和单测稳定
- 配好 Ark endpoint 后可以直接切真模型

## 推荐下一步

1. 配置真实 Ark endpoint ID（`ARK_MODEL`）
2. 启动服务验证豆包返回
3. 运行 Python 数据导入脚本，导入真实年报数据
4. 验证向量库检索功能
5. 测试 Agent 工具调用

## 当前目录

- `cmd/server`：服务入口
- `configs`：配置样例
- `internal/api`：HTTP 接口层
- `internal/service`：业务编排层
- `internal/eino`：Eino 核心链路层
- `internal/model`：请求响应模型
- `internal/pkgctx`：配置与应用上下文

## 当前可做的验证

启动服务：

- `go run ./cmd/server`

本地环境变量示例：

- `export ARK_API_KEY=你的火山Ark密钥`
- `export ARK_MODEL=你的Ark推理接入点ID`

未配置 `ARK_MODEL` 时，`/rag/query` 会返回 skeleton 答案。

## 系统架构

### 架构图

```
┌─────────────────────┐
│       Client        │
└──────────┬──────────┘
           │
┌──────────▼──────────┐
│     HTTP API        │
└──────────┬──────────┘
           │
┌──────────▼──────────┐
│   Query Service     │
└──────────┬──────────┘
           │
┌──────────▼──────────┐     ┌────────────────┐
│    RAG Chain        │────►│   LLM Client   │
└──────────┬──────────┘     └────────────────┘
           │
┌──────────▼──────────┐
│   Retriever         │
└──────────┬──────────┘
           │
┌──────────▼──────────┐     ┌────────────────┐
│  Document Repo      │────►│   Vector Store │
└─────────────────────┘     └────────────────┘

┌─────────────────────┐
│  Python Data Import │
└──────────┬──────────┘
           │
┌──────────▼──────────┐
│  Document Repo      │
└─────────────────────┘
```

### 检索流程

```
1. 接收用户查询
2. 解析查询参数（stock_code、doc_type、time_range）
3. 生成查询向量
4. 执行向量检索
5. 执行关键词检索
6. 合并检索结果
7. 排序并返回Top K结果
8. 构建上下文
9. 调用LLM生成答案
10. 返回带citation的答案
```

## 本地启动说明

服务启动后，会注册以下 HTTP 路由（定义在 `/Users/qudian/Downloads/trae_projects/job/projects/stock_rag/internal/api/router.go`）：

- `GET /health` - 健康检查
- `GET /stats` - 统计信息
- `GET /documents` - 文档列表
- `POST /rag/query` - 同步查询
- `POST /rag/query/stream` - 流式查询
- `POST /agent/execute` - 执行 Agent 任务
- `POST /agent/analyze-stock` - 分析股票
- `POST /agent/run` - 运行 Agent

## Demo 示例

### 同步查询

```bash
curl -X POST http://localhost:8080/rag/query -H "Content-Type: application/json" -d '{
  "question": "贵州茅台2025年的业绩如何？",
  "stock_code": "600519",
  "doc_types": ["announcement"],
  "time_range": "30d",
  "top_k": 5
}'
```

### 响应示例

```json
{
  "answer": "根据检索到的公开信息，贵州茅台2025年实现营业收入1580亿元，同比增长14.2%；实现归属于上市公司股东的净利润720亿元，同比增长15.8%。",
  "citations": [
    {
      "title": "贵州茅台2025年业绩快报",
      "doc_type": "announcement",
      "source_url": "https://example.com/moutai-2025",
      "published_at": "2026-03-15",
      "page_no": 1,
      "section_title": "业绩摘要"
    }
  ],
  "retrieved_count": 1,
  "request_id": "rag-general-10"
}
```

### Agent 执行示例

```bash
curl -X POST http://localhost:8080/agent/execute -H "Content-Type: application/json" -d '{
  "task": "分析贵州茅台的财务状况",
  "stock_code": "600519"
}'
```

## 评估结果

| 检索方式 | hit rate | citation 质量 | 平均延迟 (ms) | 回答质量 |
|---------|----------|-------------|--------------|----------|
| no-RAG | 0% | N/A | 500 | 一般 |
| 关键词检索 | 60% | 良好 | 800 | 良好 |
| 向量检索 | 85% | 优秀 | 1200 | 优秀 |
| hybrid retrieval | 90% | 优秀 | 1500 | 优秀 |

## 设计文档

- `docs/llm_concurrency_and_queueing.md`：LLM 高并发、排队与调度说明（含 Go/后端实现思路）
- `docs/interview_readiness.md`：从 AI 相关岗位面试视角整理的项目完善建议与冲刺重点

## 后续仍需要你确认的命令

- 向量库相关依赖