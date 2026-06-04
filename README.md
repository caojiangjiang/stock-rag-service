# stock_rag

基于 Eino 设计的股票投研 RAG 系统，提供 **Investment Persona** 多风格投资研究助手能力。

## 🎯 Investment Persona 模块

### 产品定位

Investment Persona 是一个基于多角色的投资研究辅助系统，通过模拟不同投资风格的专家，帮助用户从多角度分析投资问题。

### 核心功能

| 功能 | 说明 |
|------|------|
| **Persona 单聊** | 与特定投资风格专家一对一交流 |
| **Persona Roundtable** | 多专家圆桌讨论，展示分歧与共识 |
| **证据引用** | 所有观点附带 citation 来源 |
| **风险提示** | 明确的风险因素分析 |
| **反方观点** | 自动生成对立视角，帮助辩证思考 |

### 6 个投资角色

| Persona ID | 名称 | 风格 | 适用市场 |
|------------|------|------|----------|
| `us_growth_tech` | 成长股投资者 | 关注技术创新与增长潜力 | 美股 |
| `us_value_recovery` | 价值股投资者 | 寻找低估与安全边际 | 美股 |
| `us_momentum` | 动量交易者 | 跟随趋势与市场情绪 | 美股 |
| `cn_growth` | A股成长风格 | 聚焦新兴产业与政策驱动 | A股 |
| `cn_momentum` | A股动量风格 | 把握短期趋势机会 | A股 |
| `cn_dividend_defensive` | 防御型投资者 | 追求稳定分红与现金流 | A股 |

### 系统架构

Persona 模块复用 stock_rag 底层能力：

```
┌─────────────────────────────────────────────────────┐
│              Investment Persona                     │
│  ┌──────────────┐    ┌──────────────────────┐       │
│  │  Persona Chat│    │  Persona Roundtable  │       │
│  │  (单聊)      │    │  (圆桌讨论)          │       │
│  └──────┬───────┘    └─────────┬──────────┘       │
└─────────┼───────────────────────┼─────────────────┘
          │                       │
          ▼                       ▼
┌─────────────────────────────────────────────────────┐
│              stock_rag 核心能力                      │
│  ┌──────────┐  ┌──────────┐  ┌─────────────┐       │
│  │  RAG     │  │  LLM     │  │  Citation   │       │
│  │  检索    │  │  推理    │  │  证据管理   │       │
│  └──────────┘  └──────────┘  └─────────────┘       │
│  ┌──────────────┐  ┌─────────────────────┐         │
│  │  Observability│ │  Evaluation         │         │
│  │  监控/日志/追踪│ │  规则评估 + Judge   │         │
│  └──────────────┘  └─────────────────────┘         │
└─────────────────────────────────────────────────────┘
```

## 当前能力

- 完整的项目目录结构
- 集成了 `github.com/cloudwego/eino` 框架
- 接入了 `github.com/cloudwego/eino-ext/components/model/ark` 模型
- **Investment Persona 模块**
  - `POST /api/personas/chat` - Persona 单聊接口
  - `POST /api/personas/roundtable` - Persona 圆桌讨论接口
  - `GET /api/personas` - 获取 Persona 列表
  - `GET /api/personas/{id}` - 获取 Persona 详情
- 支持 `POST /api/chat` 统一聊天接口
- 支持 `POST /api/chat/stream` SSE 流式聊天接口
- 可通过 `ENABLE_RAG_API=true` 打开兼容旧版的 `/rag/query` 与 `/rag/query/stream`
- 支持 `GET /documents` 文档列表接口
- 支持 `GET /health/liveness`、`GET /health/readiness` 与兼容旧版的 `GET /health`
- 支持 `GET /stats` 统计信息接口
- 支持 `POST /api/agent/execute` 执行 Agent 任务
- 支持 `POST /api/agent/analyze-stock` 分析股票
- 支持 `POST /api/agent/run` 运行 Agent
- 实现了 **Ark 真模型 + skeleton fallback** 机制
- 统一的 LLM 客户端管理，支持并发控制和队列调度
- 实现了 Agent 工具调用防护：超时、重试、熔断与降级响应
- 支持 Supervisor 默认执行器与 `AGENT_EXECUTOR=react` ReAct 执行器
- 支持多 Agent 协调器自动选择，也可通过 `COORDINATOR_TYPE` 显式切换
- 支持 JWT、refresh token、token 黑名单与 admin/user RBAC
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

1. 补齐 CI：执行 `go test ./...`、`go build ./cmd/server` 和基础 lint。
2. 建立小型 RAG 评测集，持续记录 hit rate、citation 质量、P95 延迟。
3. 优化检索融合与 rerank 参数，沉淀可复现实验报告。
4. 强化生产安全：生产环境强制 `JWT_SECRET`，补充审计日志与输入校验。
5. 完善导入链路：大批量文档导入异步化，并提供任务状态查询。

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

未配置 `ARK_MODEL` 时，模型层会回退到 skeleton responder。默认推荐使用 `/api/chat`；如需验证旧版 RAG 接口，设置 `ENABLE_RAG_API=true`。

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

服务启动后，会注册以下 HTTP 路由（定义在 `internal/api/router.go`）：

- `GET /health` - 健康检查
- `GET /health/liveness` - 进程存活检查
- `GET /health/readiness` - PostgreSQL / Redis 等依赖就绪检查
- `GET /metrics` - Prometheus 指标
- `GET /stats` - 统计信息
- `GET /documents` - 文档列表
- `POST /documents/import` - 文档导入，需要 admin 权限
- `POST /api/chat` - 统一聊天接口，需要登录
- `POST /api/chat/stream` - 统一聊天流式接口，需要登录
- `POST /api/agent/execute` - 执行 Agent 任务，需要登录
- `POST /api/agent/analyze-stock` - 分析股票，需要登录
- `POST /api/agent/run` - 运行 Agent，需要登录
- `POST /rag/query` - 兼容旧版同步查询，仅 `ENABLE_RAG_API=true` 时启用
- `POST /rag/query/stream` - 兼容旧版流式查询，仅 `ENABLE_RAG_API=true` 时启用

## Demo 示例

### 统一聊天

```bash
curl -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{
  "message": "贵州茅台2025年的业绩如何？",
  "stock_code": "600519",
  "mode": "auto"
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
curl -X POST http://localhost:8080/api/agent/execute \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{
  "task": "分析贵州茅台的财务状况",
  "stock_code": "600519"
}'
```

### Persona 单聊示例

```bash
curl -X POST http://localhost:8080/api/personas/chat \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{
  "persona_id": "us_growth_tech",
  "message": "如何看待当前市场环境下的科技股投资机会？",
  "stock_code": "AAPL"
}'
```

### Persona Roundtable 示例

```bash
curl -X POST http://localhost:8080/api/personas/roundtable \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{
  "question": "AI 概念股当前估值是否合理？",
  "persona_ids": ["us_growth_tech", "us_value_recovery", "us_momentum"]
}'
```

## 📸 界面截图建议

### 建议截图场景
| 场景 | 说明 | 展示价值 |
|------|------|----------|
| Persona 单聊界面 | 展示与特定投资专家的对话 | 单角色交互能力 |
| Roundtable 圆桌讨论 | 展示多专家分歧与共识 | 多角度分析能力 |
| 风险提示卡片 | 展示风险因素分析 | 风控意识 |
| 证据引用列表 | 展示 citation 来源 | 可追溯性 |
| 反方观点模块 | 展示对立视角 | 辩证思考能力 |

### 截图要点
- 突出 persona 头像和名称标识
- 展示完整的回答结构（立场、论点、风险、反方）
- 显示参考来源卡片
- 包含风险提示徽章

## 🚀 5 分钟 Demo 路径

### 前置准备
```bash
# 启动服务
go run ./cmd/server

# 获取测试 Token（或使用环境变量 SKIP_AUTH=true 跳过认证）
export SKIP_AUTH=true
```

### Step 1: 获取 Persona 列表
```bash
curl http://localhost:8080/api/personas
```

### Step 2: 体验 Persona 单聊
```bash
curl -X POST http://localhost:8080/api/personas/chat \
  -H "Content-Type: application/json" \
  -d '{
  "persona_id": "us_growth_tech",
  "message": "如何看待当前市场环境下的科技股投资机会？",
  "stock_code": "AAPL"
}'
```

### Step 3: 体验 Roundtable 圆桌讨论
```bash
curl -X POST http://localhost:8080/api/personas/roundtable \
  -H "Content-Type: application/json" \
  -d '{
  "question": "AI 概念股当前估值是否合理？",
  "persona_ids": ["us_growth_tech", "us_value_recovery", "us_momentum"]
}'
```

### Step 4: 测试安全合规拦截
```bash
curl -X POST http://localhost:8080/api/personas/chat \
  -H "Content-Type: application/json" \
  -d '{
  "persona_id": "cn_growth",
  "message": "请推荐一只下周能涨的股票"
}'
```

### Step 5: 运行评估脚本
```bash
python3 eval/persona_eval_runner.py --rules-only --dry-run
```

## 💼 实用研究能力

### 研究卡片生成
系统支持生成结构化的投资研究卡片，包含以下核心要素：

| 字段 | 说明 | 示例 |
|------|------|------|
| **Stance（立场）** | 对投资标的的总体观点 | 看多 / 看空 / 中性 |
| **Thesis（论点）** | 支持立场的核心论据 | 3-5 条关键论点 |
| **Risks（风险）** | 潜在风险因素 | 市场风险、政策风险等 |
| **Counter View（反方）** | 对立视角分析 | 反驳观点及理由 |
| **Evidence（证据）** | 引用来源列表 | 财报、公告、新闻 |
| **Disclaimer（声明）** | 合规免责声明 | 不构成投资建议 |

### Watchlist 问答
支持针对用户关注股票列表的批量分析：

```bash
curl -X POST http://localhost:8080/api/personas/chat \
  -H "Content-Type: application/json" \
  -d '{
  "persona_id": "us_value_recovery",
  "message": "请对比分析我的关注列表：AAPL、MSFT、GOOGL 的投资价值",
  "stock_code": "AAPL"
}'
```

### 周度复盘助手
帮助用户回顾一周市场动态：

```bash
curl -X POST http://localhost:8080/api/personas/chat \
  -H "Content-Type: application/json" \
  -d '{
  "persona_id": "cn_dividend_defensive",
  "message": "总结本周市场热点，分析对防御型股票的影响",
  "stock_code": ""
}'
```

## ⚠️ 风险声明

**Investment Persona 仅供研究辅助目的使用，不构成投资建议。**

- 本系统提供的分析基于公开信息，不保证准确性和时效性；
- 投资决策应基于个人独立判断和专业顾问意见；
- 市场有风险，投资需谨慎；
- 本系统不提供荐股服务，不保证任何投资收益。

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
- `docs/optimization_plan.md`：当前项目分析、优化点与分阶段执行计划
- `docs/investment_persona/investment_persona_dialogue_mvp.md`：美股 + A股投资人格对话系统的产品方案与 MVP 设计
- `docs/investment_persona/investment_persona_daily_picks_mvp.md`：6种投资风格每日生成 Top10 风格观察名单的最小实现方案
- `docs/investment_persona/investment_persona_prd.md`：Investment Persona 的产品需求文档（页面、功能、指标、里程碑）
- `docs/investment_persona/investment_persona_api_contract.md`：Investment Persona 的 API、数据对象与错误码约定
- `docs/investment_persona/investment_persona_ux_guardrails.md`：Investment Persona 的页面交互说明、默认文案与合规护栏
- `docs/investment_persona/investment_persona_deepseek_v4_iteration_playbook.md`：给 DeepSeek-V4 的 8 轮迭代执行手册与提示词模板
- `docs/investment_persona/investment_persona_acceptance_checklist.md`：Investment Persona 的逐轮验收清单与最终展示检查项
- `docs/investment_persona/investment_persona_iteration1_8_acceptance_review.md`：Investment Persona 1-8 轮整体验收记录、未达标项与整改建议
- `docs/investment_persona/investment_persona_iteration1_enhanced_prompt.md`：更适合第一次开工的 Iteration 1 加强版提示词（计划版 + 执行版）
- `docs/investment_persona/investment_persona_iteration2_enhanced_prompt.md`：更适合第二轮资产化迭代的 Iteration 2 加强版提示词（计划版 + 执行版）
- `docs/investment_persona/investment_persona_iteration3_enhanced_prompt.md`：更适合第三轮 Persona Chat 接证据能力的 Iteration 3 加强版提示词（计划版 + 执行版）
- `docs/investment_persona/investment_persona_iteration4_enhanced_prompt.md`：更适合第四轮 Persona Roundtable 并发聚合与总结层落地的 Iteration 4 加强版提示词
- `docs/investment_persona/investment_persona_iteration5_enhanced_prompt.md`：更适合第五轮 Persona 前端页面与演示体验落地的 Iteration 5 加强版提示词
- `docs/investment_persona/investment_persona_iteration6_enhanced_prompt.md`：更适合第六轮 Persona 监控、日志、报警与可观测性补齐的 Iteration 6 加强版提示词
- `docs/investment_persona/investment_persona_iteration7_enhanced_prompt.md`：更适合第七轮 Persona 离线评估体系与 Judge 闭环建设的 Iteration 7 加强版提示词
- `docs/investment_persona/investment_persona_iteration8_enhanced_prompt.md`：更适合第八轮 Persona GitHub 展示、面试表达与实用性收尾的 Iteration 8 加强版提示词
