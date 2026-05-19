# stock_rag 面试追问清单与项目详解

> **本版本基于当前 Multi-Agent Coordinator 架构（Eino ADK 版）重写**。
> 旧版的"单 Agent 循环 + SessionState"内容已不再适用，对应的 `agent.go` 在代码里已标记 `Deprecated`。

## 1. 这份文档怎么用

这份文档解决两个问题：

1. **面试回答**：按 `Ken` 风格准备追问清单和标准回答。
2. **项目熟悉**：帮助你快速理解 `stock_rag` 的背景、架构、主链路、优缺点和真实技术细节。

建议你按这个顺序看：

1. 先看“项目一句话介绍”和“3 分钟项目讲解稿”
2. 再看“系统架构”——重点是 **Profile / Coordinator / TaskState / Persistence** 四层
3. 然后看“评估结果”和“真实短板”
4. 最后背“Ken 风格追问清单”

---

## 2. 项目一句话介绍

`stock_rag` 是一个面向**股票投研 / 金融年报问答**场景的 Go + Eino ADK RAG/Multi-Agent 系统，目标是把分散在年报、公告、新闻里的信息，通过检索增强问答、引用返回、流式输出和多 Agent 协作的方式组织起来，降低人工检索和阅读成本。

更稳妥的面试表达是：

> 我做的是一个金融事实约束场景的 Go + Eino ADK RAG/Multi-Agent 工程项目。
> 它的特点是把"谁来做"（AgentProfile）和"怎么协作"（Coordinator）解耦——
> 7 个预定义 Profile（Planner / EvidenceCollector / MetricExtractor / AnalystWriter / Risk / Comparison / Verifier）
> 可以组合成 8 种协作模式（Supervisor / Pipeline / Workflow / Plan / Peer / Debate / Committee / Deep），
> 所有中间状态都汇聚到一个 TaskState 黑板对象，再以 JSONB 形式持久化到 Postgres。
> 我也做了统一 LLMClient + 有界队列做模型层调度，以及人工复判级别的评估。

---

## 3. 项目背景、目标和场景

### 3.1 为什么做这个项目

股票投研场景里，信息通常散落在：

- 年报
- 公告
- 新闻
- 财务报表附注
- 管理层讨论与分析

人工检索的痛点是：

- 文档长，定位成本高
- 数值、时间、章节容易看错
- 同一个问题可能需要跨章节、跨年份查证
- 如果资料里没有，还需要明确拒答，不能编造

### 3.2 项目目标

这个项目不是做泛聊天，而是做**事实约束较强的金融问答系统**，核心目标包括：

- 支持同步问答和流式问答
- 支持按照 `stock_code / doc_type / time_range` 检索
- 返回带 `citation` 的答案
- 提供 Agent 能力完成多步工具调用
- 提供统一的 LLMClient，控制模型调用并发和队列
- 建立评估体系，而不是只展示 demo

### 3.3 适合回答的问题类型

- **事实抽取**：某公司某年的研发费用是多少
- **总结类**：某公司的主营业务、核心竞争力、风险因素
- **对比类**：同比增速、利润变化、跨期变化
- **定位类**：某个信息出自年报哪个章节
- **不可回答类**：资料里不存在的信息要拒答

---

## 4. 整体架构

### 4.1 工程分层

项目结构可以概括为：

- `cmd/server`：程序入口，按依赖顺序装配 Repo / Embedder / VectorStore / LLMClient / ToolRegistry / ProfileRegistry / CoordinatorFactory / Services
- `internal/api`：HTTP 路由和 Handler
- `internal/service`：业务编排层（`QueryService` / `AgentService` / `ConversationService`）
- `internal/eino`：RAG / Prompt / Retriever / Agent 核心链路
  - `internal/eino/agent`：**Multi-Agent 主目录**——Coordinator、AgentProfile、TaskState、StatefulRunner
- `internal/concurrency`：队列、并发控制、统一 LLM 请求调度
- `internal/llm`：全局 LLMClient 单例管理
- `internal/vectorstore`：pgvector 向量存储
- `internal/tools`：Agent 工具层（统一 `ToolRegistry`）
- `internal/repository`：会话和上下文持久化层（PostgreSQL）
- `eval`：RAG 和 Agent 的评估脚本、数据集和结果

### 4.2 Multi-Agent 四层架构（本项目的核心）

```
┌──────────────────────────────────────────────────────────────┐
│ Layer 4: Persistence (PostgreSQL)                            │
│   conversations / messages / conversation_contexts(JSONB)    │
└──────────────────────────────────────────────────────────────┘
                       ▲  整体覆盖 JSONB
┌──────────────────────────────────────────────────────────────┐
│ Layer 3: TaskState (跨 Agent 共享黑板)                        │
│   Plan / Evidence / Metrics / Citations / StepTraces /       │
│   CheckPoint / NeedReplan / RetryCount / Status              │
└──────────────────────────────────────────────────────────────┘
                       ▲  Add/Update 语义化方法
┌──────────────────────────────────────────────────────────────┐
│ Layer 2: Coordinator (协作模式，8 种可插拔)                   │
│   Supervisor / Pipeline / Workflow / Plan /                  │
│   Peer / Debate / Committee / Deep                           │
└──────────────────────────────────────────────────────────────┘
                       ▲  GetAgentProfiles / SetAgentProfiles
┌──────────────────────────────────────────────────────────────┐
│ Layer 1: AgentProfile (角色定义，7 个预定义)                  │
│   TaskPlanner / EvidenceCollector / MetricExtractor /        │
│   AnalystWriter / Risk / Comparison / Verifier               │
│   (Role / RolePrompt / AvailableTools / ModelConfig)         │
└──────────────────────────────────────────────────────────────┘
```

**核心设计思想**：把"谁来做"（Profile）和"怎么协作"（Coordinator）解耦。
同一组 Profile 可以塞进不同 Coordinator 里跑不同任务复杂度——
简单查询用 Pipeline，复杂任务用 Plan，需要多视角用 Debate / Committee。

### 4.3 对外接口

当前已暴露的主要接口：

- `GET /health`
- `GET /stats`
- `GET /documents`
- `POST /rag/query` / `POST /rag/query/stream`
- `POST /agent/execute`：选择 Coordinator 类型执行任务
- `POST /agent/analyze-stock`：股票分析的预设场景入口
- `POST /agent/run`：通用 Agent 执行入口
- 会话相关：列表 / 获取 / 删除，上下文（TaskState）单独查询

### 4.4 为什么这个分层适合面试讲

它体现了一个比较清晰的 AI 工程项目结构：

- API 层只负责协议和输入输出
- Service 层负责编排业务
- Multi-Agent 层做"角色 + 协作"解耦，可以横向扩展 Profile，也可以纵向扩展 Coordinator
- TaskState 把所有跨步骤状态收口在一个数据结构里，方便持久化和断点续传
- LLM 调用被统一收口，而不是散落在业务代码里

这让你在面试时既能讲 AI 架构抽象（Profile / Coordinator / State），也能讲工程化（队列、并发、持久化）。

---

## 5. RAG 主链路：系统最核心的一段

### 5.1 请求主流程

RAG 查询的核心流程可以概括为：

1. 接收用户问题
2. 自动补全或提取 `stock_code`
3. 根据 `stock_code / doc_type / time_range / top_k` 组织检索请求
4. 检索相关文档 chunk
5. 组装 Prompt
6. 调用 LLM 生成答案
7. 返回答案和 citations

### 5.2 QueryService 做了什么

`QueryService` 是 API 层和 Eino 层之间的编排层，主要职责：

- 接收依赖注入：`LLMClient / Retriever / Repo / Embedder / VectorStore`
- 如果没显式传 `stock_code`，自动从问题中提取公司名或股票代码
- 调用 `runner.Invoke()` 执行完整 RAG 链路
- 提供同步 `Query` 和流式 `QueryStream`
- 周期性刷新公司名和股票代码映射

这个设计点很适合讲，因为它说明：

- 业务层不直接依赖底层模型实现
- 检索和模型调用都是可替换、可注入的
- 后续做实验或换实现时，不需要改 API

### 5.3 Prompt 约束

当前 Prompt 的核心约束是：

- 只能基于给定资料回答
- 给出引用来源
- 不要编造不存在的事实
- 不要直接给出投资建议

这说明项目不是单纯“问一句答一句”，而是在做**grounded answer** 的约束。

---

## 6. 检索链路：当前是“可演进 hybrid retriever”

### 6.1 检索器设计

当前的 `HybridRetriever` 采用分层回退策略：

1. 如果配置了 `embedder + vectorStore`，优先走**向量检索**
2. 如果向量检索未命中，再回退到**文档仓库匹配**
3. 再未命中，回退到**本地样本文档匹配**
4. 最终 fallback 返回空 chunk，避免伪造证据

这个点面试里可以讲成：

> 我不是把 RAG 写成单一路径，而是做了多层回退，优先保证“有真实证据就基于证据回答，没有证据就宁可返回空 citation，也不伪造结果”。

### 6.2 检索过滤能力

检索阶段支持：

- `stock_code` 过滤
- `doc_type` 过滤
- `time_range` 过滤
- `top_k` 控制

在 pgvector 查询里，时间范围支持：

- `7d`
- `30d`
- `90d`
- `180d`
- `365d / 1y`
- `latest`

### 6.3 动态降级策略

如果用户没有显式传 `doc_types`，Retriever 会尝试获取当前股票可用的文档类型，然后做动态降级：

- 如果当前只有 `report` 可用，就用 `report` 重试
- 如果有其他可用类型，就用可用类型集合重新检索

这个设计可以讲成“提升召回鲁棒性”的一个工程策略。

### 6.4 当前检索链路的真实状态

优点：

- 已经支持 stock/doc/time 维度过滤
- 已经接上向量检索、数据库文档匹配和样本文档回退
- 明确避免 fallback 伪造 citation

缺点：

- 还没有看到成熟的 rerank 模块
- 检索指标接线还不完整，`retrieval_metrics` 在 v2 里不够可信
- 仍然存在“设计上支持 hybrid，但实验和可解释评估还不够完整”的问题

---

## 7. 向量存储：pgvector 方案

### 7.1 数据结构

向量存储抽象层定义了：

- `Record`：写入向量库的记录
- `SearchRequest`：查询向量 + 过滤条件 + TopK
- `SearchResult`：内容、citation、相似度分数

### 7.2 pgvector 表设计

当前 PostgreSQL 侧主要有两张表：

- `documents`
  - `stock_code`
  - `company_name`
  - `doc_type`
  - `title`
  - `source_url`
  - `published_at`
  - `raw_data`
- `document_chunks`
  - `doc_id`
  - `chunk_index`
  - `section_title`
  - `page_no`
  - `content`
  - `embedding vector(n)`
  - `metadata`

### 7.3 搜索方式

pgvector 查询大致思路是：

- 用 `1 - (embedding <=> query_vector)` 计算相似度
- 对 `stock_code / doc_type / time_range` 做 SQL 过滤
- `ORDER BY similarity DESC LIMIT top_k`
- 如果向量查询失败，再走一个简单 fallback 查询

### 7.4 可讲的优点和风险

优点：

- 向量检索和元数据过滤放在同一层执行，结构清晰
- 支持 `ivfflat + vector_cosine_ops`
- chunk 与 citation 能对应到 `page_no / section_title`

风险 / 缺点：

- 一些实现还偏“可用优先”，不是完全成熟的生产级 schema
- `published_at` 处理和写入逻辑较保守，可能影响时间过滤精度
- 更精细的 chunk 管理、重建索引、数据一致性策略还可以继续打磨

---

## 8. 模型接入：Ark 真模型 + skeleton fallback

### 8.1 当前模型接入方式

`ChatModel` 的策略是：

- 如果 `ARK_API_KEY` 和 `ARK_MODEL` 都配置了，就走真实 Ark 模型
- 如果缺任何一个，就自动回退到 skeleton 模型

### 8.2 这个设计的意义

优点：

- 本地开发稳定
- 单测容易跑
- 没配真实模型时也能把主链路跑通

缺点：

- 文档演示和真实效果之间可能存在落差
- 如果面试官追问“你这个效果是真模型还是 fallback”，你必须诚实说明

### 8.3 推荐面试表达

> 我在模型层保留了真实 Ark + skeleton fallback 的双轨机制。这样做的目的不是伪装效果，而是保证本地开发、测试和无密钥环境下主链路也能稳定运行；真正验证效果时再切到真实模型。

---

## 9. 统一 LLMClient：这个项目很值得讲的工程点

### 9.1 为什么要统一收口

项目里明确要求：`QueryService` 不允许自己 new 一个模型客户端，必须注入统一的 `LLMClient`。这么做的原因是：

- 统一并发控制
- 统一排队
- 统一超时
- 统一流式和非流式调用
- 统一统计和调度

### 9.2 LLMRequest 里包含什么

一个 LLM 请求会带：

- `request_id`
- `question`
- `messages`
- `task_type`（如 `agent / rag / stream`）
- `priority`
- `timeout`
- `stream`
- `metadata`
- `on_chunk`

这让模型调用不再是“直接调用 SDK”，而是变成一个可调度对象。

### 9.3 队列和并发控制怎么做

当前 `QueueManager` 维护：

- `pending` 等待队列
- `maxQueueSize`
- `maxConcurrency`
- `maxWaitTime`
- `activeCount`

处理逻辑是：

1. 请求先进入有界队列
2. 调度器监听事件触发 dispatch
3. 只有当 `activeCount < maxConcurrency` 才会放请求进入执行
4. 已超时或已取消的请求会在 dispatch / cleanup 阶段被剔除
5. 请求处理完后释放槽位，再继续补位

### 9.4 为什么这块适合面试讲

因为它体现了一个很重要的工程意识：

> Go 擅长高并发连接，不等于下游 LLM 可以无上限并发。真正要控制的是进入模型层的活跃请求数，而不是 HTTP goroutine 数量。

这句话很适合背。

---

## 10. Multi-Agent：本项目的核心设计点

> 旧版的"单 Agent 循环 + JSON action"已废弃（`agent.go` 顶部标注 `Deprecated`）。
> 当前架构是 **Profile + Coordinator + TaskState** 三件套，基于 Eino ADK 重构。

### 10.1 设计思想：把"谁来做"和"怎么协作"解耦

| 维度 | 旧版 | 当前版 |
|---|---|---|
| Agent 数量 | 1 个 | 7 个预定义 Profile，可组合 |
| 协作模式 | 单循环 | 8 种 Coordinator，按任务复杂度选 |
| 状态 | SessionState（轻） | TaskState（共享黑板，含 Plan/Evidence/Metrics/Citations 等） |
| 工具调用 | 自己写 JSON action 协议 | 复用 Eino ADK 原生 tool calling |
| 持久化 | 内存 | Postgres 三表 + JSONB |
| 断点续传 | ❌ | StatefulRunner + CheckPointStore |

### 10.2 七个预定义 AgentProfile

每个 Profile = `Role + RolePrompt + AvailableTools + ModelConfig + Constraints`，
在 `internal/eino/agent/profiles.go` 里集中维护：

| Profile | 角色 | 主要工具 | Temperature |
|---|---|---|---|
| `task_planner` | 任务规划专家 | `resolve_entity` | 0.5 |
| `evidence_collector` | 证据收集专家 | `retrieve_evidence` / `web_search` / `search_announcements` / `fetch_webpage` / `rerank_evidence` / `dedupe_sources` / `extract_timeline` | 0.3 |
| `metric_extractor` | 财务指标专家 | `extract_metrics` / `normalize_units` / `compare_periods` / `calculator` | 0.2 |
| `analyst_writer` | 投资分析专家 | `generate_report` / `verify_citations` / `get_market_snapshot` / `sentiment_or_risk_scan` / `peer_company_lookup` | 0.7 |
| `risk_agent` | 风险分析师 | `web_search` / `search_announcements` / `extract_timeline` / `sentiment_or_risk_scan` | 0.3 |
| `comparison_agent` | 对比分析专家 | `compare_periods` / `calculator` / `peer_company_lookup` / `retrieve_evidence` | 0.3 |
| `verifier_agent` | 质量审核专家 | `verify_citations` / `fetch_webpage` / `retrieve_evidence` | 0.2 |

**关键观察**：Temperature 按角色分级——抽取/审核类 0.2，规划 0.5，写作 0.7。这是 Profile 抽象带来的好处，可以按角色调参而不是全局调参。

### 10.3 八种 Coordinator（协作模式）

`internal/eino/agent/coordinator.go` 定义统一接口：

```go
type Coordinator interface {
    Name() string
    Execute(ctx context.Context, taskState *TaskState) (string, error)
    GetAgentProfiles() []*AgentProfile
    SetAgentProfiles(profiles []*AgentProfile)
}
```

8 种实现及适用场景：

| Coordinator | 适用场景 | 简要逻辑 |
|---|---|---|
| `Supervisor` | 中等复杂度，需要动态路由 | Eino ADK Supervisor，由主 Agent 调度子 Agent |
| `Pipeline` | 固定顺序流水线 | Evidence → Metric → Writer 顺序执行 |
| `Workflow` | 静态 DAG 编排 | 基于 `workflow.go`，节点依赖图 |
| `Plan` | 复杂任务先规划再执行 | Planner 出计划 → 按步骤分派 → 必要时 Replan |
| `Peer` | 多 Agent 平等并行 | 各 Profile 并行产出，再合并 |
| `Debate` | 需要对抗观点 | 多 Agent 互相质疑，多轮收敛 |
| `Committee` | 需要多人投票 | 多 Agent 各出结论，按投票 / 仲裁定稿 |
| `Deep` | 深度研究类长任务 | 多轮迭代检索 + 综合 |

`CoordinatorFactory.Create(type)` 是统一入口，外层只关心传哪种类型，不关心实现细节。

### 10.4 TaskState：跨 Agent 的共享黑板

`internal/eino/agent/task_state.go` 是所有 Coordinator 都依赖的状态容器，关键字段：

```go
type TaskState struct {
    // 任务元信息
    ConversationID, MessageID, UserID, UserMessage string
    StockCode, CompanyName, TimeRange string
    DocTypes []string
    // 跨步骤状态
    Plan                 *ExecutionPlan   // Planner 产出的执行计划
    EvidenceSet          []EvidenceItem   // EvidenceCollector 收集的证据
    MetricTable          []MetricItem     // MetricExtractor 抽取的财务指标
    IntermediateFindings []string         // 阶段性中间结论
    Summary              string           // 最终回答
    Citations            []Citation       // 引用列表
    // 可观测性
    StepTraces  []StepTrace               // 每一步的执行轨迹
    Errors      []string
    Status      TaskStatus                // pending / running / completed / failed / replan
    CurrentStep int
    NeedReplan  bool                      // 是否需要重新规划
    RetryCount  int
    // 断点续传
    CheckPointID       string
    LastCheckPointStep string
    CreatedAt, UpdatedAt time.Time
}
```

**写入入口都是语义化方法**（`UpdateStatus / AddStepTrace / AddEvidence / AddCitation / AddError / MarkReplan`），
每个方法内部统一刷 `UpdatedAt`，方便后续做乐观锁或增量同步。

### 10.5 TaskState 怎么感知节点执行状态（事件→状态翻译）

TaskState 自己不感知节点，**由 Coordinator 在事件循环里主动写入**。完整链路：

```
Eino ADK 子 Agent 执行 step
        ↓ 产出 AgentEvent
AsyncIterator[*adk.AgentEvent]    （Eino 原生事件通道）
        ↓ iter.Next() 阻塞拉取
Coordinator.Execute 里的 for 循环
        ↓ 翻译为语义化方法调用
TaskState.AddStepTrace / AddError / AddFinding / CreateCheckpoint
        ↓ 任务完成后
ConversationService.SaveContext → Postgres JSONB
```

事件类型与 TaskState 字段的映射（以 `supervisor_coordinator.go` 为最完整样本）：

| Eino 事件 | TaskState 更新 |
|---|---|
| `event.Err != nil` | `UpdateStatus(Failed)` + `AddError(...)` |
| `event.Output.MessageOutput` | `AddStepTrace(...)` + `CurrentStep++` |
| `event.Action.TransferToAgent` | `AddFinding("转移到 Agent: X")` |
| `event.Action.Interrupted`（在 StatefulRunner 里） | `CreateCheckpoint(...)` + `SaveState(...)` |
| 循环正常结束 | `UpdateStatus(Completed)` + `Summary = finalResult` |

**为什么用 Pull 模式而不是 Observer**：Eino ADK 已经提供 AsyncIterator，
让 Coordinator 当"事件翻译器"是最简单且类型安全的方案——
单 goroutine 写 = 天然并发安全，调试时事件流显式可见。

### 10.6 StatefulRunner 与断点续传

`stateful_runner.go` 在 Eino `adk.Runner` 之上包了一层：

- 自动维护 `CheckPointID`
- 收到 `event.Action.Interrupted` → 立即 `SaveState` 落 `CheckPointStore`
- `Resume(ctx, taskState, interruptID, resumeData)` 可以从中断点继续
- `CheckPointStore` 是接口（`SaveState / LoadState / ClearState`），默认实现 `InMemoryCheckPointStore`

⚠️ **必须诚实指出**：默认 `CheckPointStore` 是内存实现，**还没接 Postgres**，
所以崩溃在中断点之间会丢状态。Postgres 的 `conversation_contexts` 表
只在任务完整结束时由 `ConversationService` 统一写。

### 10.7 当前 Multi-Agent 的真实定位

最稳妥的表达：

> 我做的是一个 **Profile / Coordinator / TaskState / Persistence** 四层抽象的 Multi-Agent 系统。
> 架构层面已经摸到生产门口（解耦、可插拔、可观测字段齐全），
> 但工程化层面（trace 真实采集、CheckPoint 落库、业务效果）还有明显差距。
> 我比谁都清楚哪里强、哪里弱。

---

## 11. 持久化：三表设计 + JSONB 黑板

### 11.1 表结构（PostgreSQL）

`internal/repository/postgres_conversation_store.go` 维护三张表：

| 表 | 主要字段 | 用途 |
|---|---|---|
| `conversations` | `id / user_id / title / created_at / updated_at` | 会话元数据 |
| `messages` | `id / conversation_id (FK) / role / content / created_at` | 历史消息 |
| `conversation_contexts` | `conversation_id (FK PK) / context (JSONB) / updated_at` | TaskState 上下文（整体覆盖） |

外键全部 `ON DELETE CASCADE`，删会话时上下文和消息一起清理。

### 11.2 为什么 TaskState 用 JSONB 而不是关系建模

| 维度 | JSONB | 关系建模 |
|---|---|---|
| Schema 演进 | TaskState 经常加字段（NeedReplan / MetricTable...），无需改表 | 每加一个字段都要 migration |
| 嵌套结构 | Plan / Evidence / Citations 天然嵌套 | 需要拆 5~6 张子表 |
| 查询粒度 | 整体加载使用 | 适合分页/局部查询，但本场景不需要 |
| 写入策略 | 整体覆盖 UPSERT（单条总量不大） | 增量 UPDATE，逻辑复杂 |

**写策略**：每次任务结束统一 `INSERT ... ON CONFLICT (conversation_id) DO UPDATE`，
整体覆盖比 `jsonb_set` 增量更可靠也更简单。

### 11.3 两级持久化策略

```
┌──────────────────────────────────────────────┐
│ 正常 step 完成 → 只更新内存 TaskState（不落盘）│
├──────────────────────────────────────────────┤
│ 节点被中断   → StatefulRunner 立即 SaveState  │
│              → CheckPointStore（默认内存）    │
├──────────────────────────────────────────────┤
│ 任务整体结束 → ConversationService.SaveContext│
│              → Postgres conversation_contexts │
└──────────────────────────────────────────────┘
```

**强一致点**：中断点（human-in-the-loop / 资源不够）会立即落盘。
**弱一致点**：正常 step 不落盘，靠最终一次写入。

⚠️ 当前 `CheckPointStore` 默认 InMemory，**没接 Postgres**，这是已知工程债。

---

## 12. 工程债与离生产的真实差距

不要回避——主动讲反而显得有判断力。我自己评估：
**架构层 7/10，工程化层 3/10**，平均 3.5/10。

### 12.1 三个致命差距

| 差距 | 现状 | 生产标准 |
|---|---|---|
| **可观测性** | `StepTrace` 字段齐全但 `LatencyMS / TokenIn / TokenOut / CostUSD` 还没真正埋点；只有 `logs/app.log` | OTel trace + Prometheus metric + Grafana 看板 |
| **可靠性** | `CheckPointStore` 默认 InMemory；没有幂等去重；没有分级超时；没有 Graceful Shutdown | Postgres 持久化 + `request_id` 去重 + per-tool/per-llm 超时 + SIGTERM 保存 |
| **业务效果** | Agent 任务完成率 11.8%，工具参数准确率 5.9% | 收窄场景的生产 Agent 通常 60%+（Anthropic Computer Use OSWorld 22% 是 SOTA） |

### 12.2 五个中等差距

| 差距 | 现状 | 备注 |
|---|---|---|
| Prompt 版本管理 | 硬编码在 `profiles.go` | 缺 git 版本号 + A/B 灰度 |
| 多模型路由 | 仅 Ark + skeleton fallback | 缺按任务/成本/可用性路由 |
| Token / Cost 统计 | 字段预留但未采集 | per-user / per-day 预算未做 |
| 多租户隔离 | 仅 `user_id` 字段查询 | 缺行级安全 / 向量 namespace |
| 评估自动化 | 手动跑 Python 脚本 | 缺 CI 集成 + 回归阻断发布 |

### 12.3 已识别的 4 个具体代码债

| # | 位置 | 问题 | 修复方向 |
|---|---|---|---|
| 1 | `supervisor_coordinator.go` | 只有 Supervisor 写完整 StepTrace | 抽到 BaseCoordinator 统一处理 |
| 2 | `pipeline_coordinator.go` / `plan_coordinator.go` | 没写 StepTrace | 同上 |
| 3 | `stateful_runner.go` | `CheckPointStore` 默认 InMemory | 实现 PostgresCheckPointStore |
| 4 | `StepTrace.StartTime == EndTime` | `LatencyMS` 永远是 0 | 需要在 step 开始点埋时间戳 |

### 12.4 演进方向：把 event→state 映射抽到 TaskState.Reduce

> "如果重做，我会从 'Coordinator 手动翻译' 演进到 'TaskState 内置 EventReducer'：
>
> ```go
> func (s *TaskState) Reduce(event *adk.AgentEvent) {
>     switch {
>     case event.Err != nil:     s.applyError(event)
>     case event.Output != nil:  s.applyStep(event)
>     case event.Action != nil:  s.applyAction(event)
>     }
> }
> ```
>
> 这样：8 个 Coordinator 不用各自写一遍翻译逻辑；
> 单元测试只测 Reduce 即可，不用拉起整条 Eino 链路；
> 未来加 event-sourcing 时，重放就是 `fold(Reduce, events)`。"

---

## 13. 评估：这个项目最值得主动讲的部分

### 13.1 为什么评估很重要

金融问答不能只看“像不像对”，因为：

- 数值可能错
- 单位可能错
- 时间维度可能错
- 来源和章节可能错
- 没有证据时应该拒答，而不是瞎猜

所以这个项目一个很好的点是：**不是停在 demo，而是补了评估。**

### 13.2 RAG 评估分两阶段

#### 第一阶段

早期自动评测结果较高，`eval_results.json` 里准确率达到 `95%`。

#### 第二阶段

后来发现这个结果偏乐观，于是补了更严格的 `v2` 和人工复判。

### 13.3 人工复判结果

人工复判口径更接近“人类看答案是否真的答对”，结果是：

- **端到端人工准确率**：`15 / 40 = 37.5%`
- **剔除请求错误后的模型准确率**：`15 / 34 = 44.1%`

按题型看：

- 事实抽取：`4 / 11 = 36.4%`
- 总结类：`3 / 8 = 37.5%`
- 对比类：`2 / 7 = 28.6%`
- 定位类：`1 / 7 = 14.3%`
- 不可回答类：`5 / 7 = 71.4%`

### 13.4 这组结果说明什么

可以这样解读：

- 系统在**不可回答时拒答**这件事上相对更稳
- 在**事实抽取和总结**上有一定能力，但还不稳定
- 在**对比类、定位类**上明显偏弱
- 系统还没有达到成熟上线标准，但已经暴露出真实瓶颈

### 13.5 v2 自动评测为什么不能直接当最终成绩

`eval_analysis_v2.md` 给出的结论很关键：

- 数值题容易被抽取器误判
- 总结类被关键词规则压得太低
- 请求失败和真实拒答有污染
- retrieval metrics 还没完全接好

所以最好的表达不是“系统只有 30% 成功率”，而是：

> v2 的价值主要在于它暴露了问题，但它本身也还在校准中；目前更可信的结果是结合人工复判来理解系统真实能力边界。

---

## 14. Agent 评估：维度设计不错，但当前结果偏弱

> 注：当前 Agent 评估是在旧版"单 Agent 循环"上跑出来的结果，
> 新版 Multi-Agent Coordinator 还没有跑同口径评估——这是要补的工作。

### 14.1 Agent 评估维度

当前 Agent 评估脚本覆盖了：

- 工具选择准确率
- 工具参数准确率（`stock_code` / `time_range` / `doc_type` / `compare`）
- 多轮记忆（slot retention / cross-turn reference / session isolation）
- 拒答能力
- 复杂任务完成率

### 14.2 当前 Agent 结果（旧版）

- `overall_accuracy ≈ 11.8%`
- `tool_selection_accuracy ≈ 23.5%`
- `stock_code_arg_accuracy ≈ 5.9%`
- `session_isolation_accuracy = 100%`
- `unsupported_refusal_rate ≈ 76.5%`
- `task_completion_rate ≈ 11.8%`

### 14.3 怎么解释这组结果

不能简单讲成"Agent 不行"，更准确的说法是：

- 评估维度已经搭起来了
- 会话隔离和拒答意识有一些正向信号
- 但工具选择、参数生成、超时稳定性仍是明显瓶颈
- 一些 case 还受请求超时影响，`tool_call_count` 也出现过 `0`
- 新版 Multi-Agent 上线后还要重新跑同口径，对比 Pipeline / Plan / Supervisor 哪种 Coordinator 对哪类任务效果最好

横向对比业界（让数字有坐标系）：

| 系统 | 任务完成率 | 备注 |
|---|---|---|
| Anthropic Computer Use（OSWorld） | 22% | SOTA，仍被认为 "prod-not-ready" |
| Devin / Cursor Composer（收窄场景） | 60~80% | 极致单场景优化 |
| stock_rag（旧版，宽场景） | 11.8% | 场景宽 + 工具描述弱 |

**结论**：Agent 完成率是行业普遍难题，不是项目独有问题。
但收窄场景（比如"只做财报数值抽取"）做到 80%+ 是可达的，比做 8 个场景每个 12% 强。

---

## 15. 项目的优点、缺点、风险和技术债

### 15.1 优点

1. **架构抽象层次清晰**
   - Profile / Coordinator / TaskState / Persistence 四层解耦；
   - 同一组 Profile 可以塞进 8 种 Coordinator 跑不同任务复杂度。

2. **RAG 主链路完整**
   - query / stream / retriever / prompt / citation 都已接通。

3. **统一模型调用入口**
   - 统一并发、超时、排队，这一点很适合技术面试。

4. **基于 Eino ADK 而不是自己造轮子**
   - 曾自定义了一层 Agent 抽象，后来重构为复用 Eino ADK 的 `Runner` / `AgentEvent` / `Interrupted`，体现"知道何时拥抱框架"的判断力。

5. **状态与持久化设计完整**
   - TaskState 字段（Plan / Evidence / Metrics / Citations / StepTraces / CheckPoint / NeedReplan）和生产 Agent 的 state shape 一致；
   - 三表 + JSONB 的设计为后续 schema 演进留了空间。

6. **评估意识较强**
   - 从自动评测走到 v2 严格评测和人工复判，能更真实地谈系统效果。

### 15.2 缺点 / 不足

1. **业务效果还不够强**
   - 人工复判后 RAG 端到端 37.5%，Agent 完成率 11.8%。

2. **评测器仍在校准**
   - 数值抽取、总结类匹配、retrieval 指标接线都还不够成熟。

3. **可观测性字段预留但未真正采集**
   - StepTrace 已有 `LatencyMS / TokenIn / TokenOut / CostUSD / TraceID`，但实际值都是 0；
   - 需要接 OTel 才算真观测。

4. **CheckPointStore 默认 InMemory**
   - 崩了就丢，没接 Postgres。

5. **Coordinator 实现一致性不够**
   - 只有 `SupervisorCoordinator` 写完整 StepTrace，`Pipeline / Plan` 没写。

6. **仍有 fallback / skeleton 痕迹**
   - 工程稳定性保障，但要主动说明效果区分。

7. **时延偏高**
   - v2 评估中平均 `17.7s`，P95 `39.3s`。

### 15.3 技术债

- 更精细的 retrieval metrics 还没完全接上
- rerank / chunk / prompt ablation 还缺完整实验
- citation 结构化校验仍可加强
- StepTrace 字段填全 + OTel 接入
- PostgresCheckPointStore 替换 InMemory
- 把 event→state 翻译抽到 TaskState.Reduce 统一处理
- 新版 Multi-Agent 还没跑同口径评估

---

## 16. 如果面试官问"这个项目成功吗"

最稳妥的回答：

> 如果以"已经能稳定上线、效果很好"来定义成功，那它现在还不算成功，因为人工复判后的真实准确率还不高。
> 但如果以"技术路线是否跑通、架构抽象是否合理、瓶颈是否被识别、评估是否建立"来定义，它是阶段性成功的。
> 因为我把 RAG 主链路、citation、stream、Multi-Agent Coordinator、统一 LLMClient、TaskState 持久化和评估闭环都搭起来了，也明确知道短板在数值抽取、对比题、定位题、Agent 工具参数生成、可观测性采集和 CheckPoint 持久化上。

---

## 17. 3 分钟项目讲解稿

> 我做的是一个面向股票投研场景的 Go + Eino ADK RAG/Multi-Agent 系统。这个场景的特点是年报、公告、新闻信息分散，人工检索成本高，对数值、时间和来源的准确性要求很高。
>
> 系统架构上，我把 Agent 部分做成了 **Profile / Coordinator / TaskState / Persistence 四层解耦**：7 个预定义 AgentProfile（Planner / EvidenceCollector / MetricExtractor / AnalystWriter / Risk / Comparison / Verifier）定义"谁来做"；8 种 Coordinator（Supervisor / Pipeline / Workflow / Plan / Peer / Debate / Committee / Deep）定义"怎么协作"；所有跨步骤状态都汇聚到一个 TaskState 黑板对象，最后以 JSONB 形式持久化到 Postgres 的三表结构。
>
> TaskState 不主动感知节点状态——它是被 Coordinator 在事件循环里主动写入的：Coordinator 调 Eino ADK 的 `agent.Run` 拿到 `AsyncIterator[*AgentEvent]`，然后把事件翻译成 `AddStepTrace / AddError / AddFinding / CreateCheckpoint` 这些语义化方法调用。这是个 Pull 模式，单 goroutine 写 = 天然并发安全。
>
> RAG 主链路支持同步和流式查询，按 stock_code / doc_type / time_range 过滤，返回带 citations 的答案。Retriever 是可演进的 hybrid：优先向量，未命中回退仓库匹配、再回退样本文档，最终不命中就返回空 citation 而不是伪造。
>
> 工程上我比较重视统一 LLMClient——所有模型请求先进有界队列，通过 `maxConcurrency / maxQueueSize / 超时` 控制进入模型层的活跃请求数，因为真正瓶颈是模型吞吐而不是 HTTP goroutine。
>
> 评估上我没只报漂亮的自动分数。人工复判后 RAG 端到端 37.5%，Agent 完成率 11.8%。横向对比 Anthropic Computer Use 在 OSWorld 也只有 22%，所以这是行业普遍难题。我比谁都清楚哪里强、哪里弱——架构层 7 分，工程化层 3 分，最致命的差距是可观测性还没真正采集、CheckPointStore 还是 InMemory，这是接下来要补的。

---

## 18. Ken 风格追问清单 + 标准回答

### Q1：为什么要做这个项目？它解决什么问题？

**标准回答：**

> 股票投研里最典型的痛点是信息分散和检索成本高。很多问题不是没有答案，而是答案散落在年报正文、财务报表附注、公告和新闻里，人工定位很慢。这个项目的目标就是把检索、引用和回答组织起来，让系统能先找到证据，再基于证据回答，而不是直接生成一段看起来像对的话。

### Q2：为什么用 Go，不用 Python？

**标准回答：**

> 这个项目我想强调的不只是模型调用，而是一个可服务化的 AI 工程系统。Go 比较适合做 API 服务、并发控制、流式输出和队列调度；而且我项目里有统一 LLMClient、bounded queue、超时和取消管理，这些用 Go 实现比较自然。Python 更适合算法实验，但这个项目我更想体现工程实现能力。

### Q3：为什么要引入 Eino ADK？

**标准回答：**

> Eino ADK 在这里主要是 Multi-Agent 编排层，帮我复用了 `Runner / AgentEvent / AsyncIterator / Interrupted / tool calling` 这些原生抽象。它的价值不在于"自动提升效果"，而在于让我不用自己造 Agent 运行时——我曾经自己写过一版（`agent.go`，现在标 Deprecated），后来重构为基于 Eino ADK，省下来的精力都投入到上层的 Coordinator 编排和 TaskState 设计上。这也是"何时拥抱框架"的判断。

### Q4：你的 RAG 主链路具体是怎么走的？

**标准回答：**

> 主链路是 QueryService 先接请求，然后自动补全股票代码，再把请求交给 retriever 做上下文检索，之后组装 Prompt，调用统一 LLMClient 生成答案，最后返回 answer 和 citations。流式接口和非流式接口复用同一套查询准备逻辑，只是在输出阶段一个是一次性返回，一个是逐 chunk 回调。

### Q5：检索到底做了什么？是纯向量，还是 hybrid？

**标准回答：**

> 当前检索器是一个可演进的 hybrid retriever。优先走向量检索，如果配置了 embedder 和 vectorStore，就先做 query embedding 和 pgvector 搜索；如果向量没命中，再回退到文档仓库匹配和本地样本文档匹配。它还支持 `stock_code`、`doc_type` 和 `time_range` 过滤，并在 docType 缺失时做动态降级。严格说它已经具备 hybrid 架构，但更正式的检索对比实验和 rerank 仍是下一步优化重点。

### Q6：citation 是怎么来的？

**标准回答：**

> citation 来自检索结果本身。每个 chunk 在向量库和检索链路里都带有标题、页码、章节、来源 URL 等元数据，生成回答时一起返回。现在它已经能做“答案 + 引用来源”的返回，但更严格的 citation 正确性评估和结构化命中分析还需要继续加强。

### Q7：你怎么控制 hallucination？

**标准回答：**

> 我主要做了三层约束：第一层是检索约束，尽量让回答建立在真实 chunk 上；第二层是 Prompt 约束，明确写了只能基于资料回答、证据不足就说明、不要编造事实；第三层是评估约束，专门加了不可回答类问题，去测试它在资料缺失时能不能拒答。从结果看，拒答是系统当前相对稳的一块。

### Q8：为什么第一版 95%，后来只有 37.5%？

**标准回答：**

> 第一版自动评测偏宽松，尤其在数值题、总结题和请求失败处理上会高估系统效果。后来我做了更严格的 v2 和人工复判，虽然分数更低，但我认为更真实。对我来说，这不是“项目变差了”，而是评估更诚实了，也更能指导下一步优化。

### Q9：那你觉得项目成功吗？

**标准回答：**

> 如果按最终产品标准看，它现在还不算成功，因为准确率和稳定性离上线要求还有差距。  
> 但如果按技术验证标准看，我认为它是阶段性成功的：RAG 主链路、citation、stream、统一 LLMClient、Agent 循环和评估闭环都已经建立起来了，而且我清楚知道短板在哪里。

### Q10：为什么不可回答类做得相对更好？

**标准回答：**

> 因为 Prompt 里明确约束了“证据不足就说明”，而且金融场景本身很适合把“不知道”视为正确行为。相对来说，不可回答类判断逻辑也更明确，不像数值抽取和总结类那样容易被评测器误伤。所以拒答能力会更稳定一些。

### Q11：为什么定位类这么差？

**标准回答：**

> 定位类对 citation 和章节结构要求更高，不仅要知道答案，还要准确指向章节名或 section title。当前系统更多是“回答文本里提到正确章节”，而不是“citation 已经严格结构化命中章节”，所以这块结果偏弱，也说明 citation 和 chunk 结构化还要继续加强。

### Q12：你的 Agent 和普通 RAG 的差别是什么？

**标准回答：**

> 普通 RAG 是"一次检索 + 一次回答"；我的是 Multi-Agent——把任务交给 Coordinator，Coordinator 按协作模式（Supervisor 动态路由 / Pipeline 流水 / Plan 先规划再执行）派工给不同 AgentProfile 完成子任务，所有中间产出（Plan / Evidence / Metrics / Citations）汇聚到一个共享的 TaskState 黑板，最后由 AnalystWriter 综合输出。结构上是真多 Agent，但业务效果（任务完成率 11.8%）距离生产还有差距。

### Q13：你为什么要把 Profile 和 Coordinator 解耦？

**标准回答：**

> 因为"谁来做"和"怎么协作"是两个正交维度。我有 7 个 Profile（Planner / EvidenceCollector / MetricExtractor / AnalystWriter / Risk / Comparison / Verifier）和 8 种 Coordinator（Supervisor / Pipeline / Workflow / Plan / Peer / Debate / Committee / Deep）。解耦的好处是：同一组 Profile 可以塞进不同 Coordinator 跑不同复杂度的任务——简单查询用 Pipeline，复杂任务用 Plan，需要多视角时用 Debate / Committee；新增 Profile 不用改 Coordinator，反之亦然。这是 LangGraph、Eino 这类生产框架共同的设计哲学。

### Q14：TaskState 怎么感知到节点的执行状态？

**标准回答：**

> TaskState 自己不感知，是被 Coordinator 在事件循环里主动写入的。Coordinator 调 Eino ADK 的 `agent.Run()` 拿到一个 `AsyncIterator[*AgentEvent]`，`for iter.Next()` 阻塞拉取每个 step 的事件，事件分四类：错误、输出、转移、中断；Coordinator 把每类事件翻译成 TaskState 的 `AddStepTrace / AddError / AddFinding / CreateCheckpoint` 调用，TaskState 内部统一刷 `UpdatedAt`。这是 Pull 模式 + 事件驱动的混合，单 goroutine 写 = 天然并发安全。
>
> 我得诚实讲，目前只有 SupervisorCoordinator 写了完整 StepTrace，Pipeline 和 Plan 没写，这是个一致性 bug。如果重做我会把 event→state 映射抽到 TaskState.Reduce 方法里，让所有 Coordinator 复用。

### Q15：会话状态和断点续传怎么做？

**标准回答：**

> 两级持久化策略。第一级是任务结束时的整体持久化：`ConversationService.SaveContext` 把整个 TaskState 序列化成 JSON 整体覆盖 UPSERT 到 Postgres 的 `conversation_contexts` 表（JSONB 字段），加载时直接 `FromJSON` 还原。第二级是中断点强一致：StatefulRunner 监听 `event.Action.Interrupted`，立即调 `CheckPointStore.SaveState`，恢复时用 `ResumeWithParams(checkPointID, params)` 从断点继续。
>
> 但要诚实——默认 `CheckPointStore` 是 `InMemoryCheckPointStore`，还没接 Postgres，所以崩在两个 checkpoint 之间会丢。这是已知工程债。

### Q16：8 种 Coordinator 哪些真跑过？

**标准回答：**

> Supervisor、Pipeline、Plan 这三个是事件循环写得最完整的，跑过真实任务；Workflow / Peer / Debate / Committee / Deep 我做了实现但还没在生产任务集上验证。所以面试时我只会重点讲那三个，其他的我会承认"实现了但还没系统跑过"——这是真实情况，藏着掖着面试官真去翻代码会发现。

### Q17：工具失败了怎么办？

**标准回答：**

> 当前主要靠两层：第一，工具异常会作为 `event.Err` 进入事件循环，Coordinator 把错误信息写入 TaskState.Errors，由上层 Agent 决定是否重试或换 Profile；第二，TaskState 有 `RetryCount` 和 `NeedReplan` 字段，PlanCoordinator 在多次失败后会触发 `MarkReplan` 重新规划。但要诚实——还没有完整的指数退避和 per-tool 熔断，这是生产化要补的。

### Q18：为什么要做统一 LLMClient，而不是业务代码直接调模型？

**标准回答：**

> 因为真正的瓶颈不在 HTTP 并发，而在模型层吞吐。统一 LLMClient 可以把所有请求先收进有界队列，再用 `maxConcurrency` 控制进入模型层的活跃请求数，同时统一处理超时、取消、流式和统计。如果业务代码各自直接调模型，就很难做全局配额和调度。

### Q19：如果 1 万请求进来，但模型只能稳定处理 100 并发，怎么办？

**标准回答：**

> 让请求先经过 admission + bounded queue。队列有最大长度，超出就拒绝；进入队列后由调度器控制活跃槽位，始终只让最多 100 个请求进入推理层。等待过久的请求会超时剔除，用户取消的请求也会被回收。核心是"控制进入模型层的活跃请求数"，而不是盲目扩 goroutine。

### Q20：为什么 TaskState 用 JSONB 而不是关系建模？

**标准回答：**

> 三个原因：第一，TaskState 字段经常演进（加 MetricTable、加 NeedReplan），JSONB 不用改表结构；第二，Plan / Evidence / Citations 都是嵌套结构，关系建模要拆 5~6 张子表；第三，TaskState 是整体加载使用，写入策略是 UPSERT 整体覆盖，比 `jsonb_set` 增量更新更可靠也更简单。代价是失去字段级查询能力，但本场景不需要。

### Q21：你这个 Agent 距离生产还有多远？

**标准回答：**

> 客观说有相当距离。我自评架构层 7 分、工程化层 3 分。最致命的三块：第一，可观测性几乎为零——StepTrace 字段齐全但 `LatencyMS / TokenIn / Cost` 都没真正埋点，生产必须接 OTel；第二，`CheckPointStore` 默认 InMemory，崩了就丢；第三，业务效果，Agent 完成率 11.8%，对比 Anthropic Computer Use 在 OSWorld 也只有 22%，所以这是行业普遍问题。
>
> 我的判断是架构不用大改，缺的是工程化深度。如果让我演进，优先级是：① 接 OTel + 填全 StepTrace 字段；② Checkpoint 落 Postgres；③ 把场景从"金融全域"收窄到"财报数值抽取"，单场景做到 80%+。

### Q22：当前项目最大的技术短板是什么？如果继续优化你优先做什么？

**标准回答：**

> 短板分三类：业务效果（数值/对比/定位偏弱）、可观测性（trace 没采集）、可靠性（CheckPoint 没落库 + 无幂等去重）。
>
> 优化优先级：P0 接 OTel + 把 StepTrace 字段填全，因为没观测就没法判断后面优化是否有效；P1 实现 PostgresCheckPointStore，让中断点真持久化；P2 把 event→state 翻译抽到 TaskState.Reduce，解决 Pipeline / Plan 没写 StepTrace 的一致性 bug；P3 再做业务效果——收窄场景做单场景优化，比追加 Profile 更有 ROI。

### Q23：如果让你一句话评价这个项目，你会怎么说？

**标准回答：**

> 这是一个把 Multi-Agent 主链路、Profile/Coordinator 解耦、TaskState 共享黑板、citation/stream 和统一 LLM 调度都跑通的金融事实约束场景 RAG 项目；架构层接近生产标准，工程化和业务效果都还有明确的下一步路径。

---

## 19. 你自己熟悉项目时，最该看的文件

建议按下面顺序读代码：

1. `README.md`
   - 先建立全局视图和接口认知

2. `cmd/server/main.go`
   - 看依赖装配顺序：Repo / Embedder / VectorStore / LLMClient / ToolRegistry / ProfileRegistry / CoordinatorFactory / Services

3. `internal/api/router.go`
   - 看系统有哪些入口接口

4. `internal/service/query_service.go` + `agent_service.go` + `conversation_service.go`
   - 看 RAG / Agent / 持久化的编排逻辑

5. `internal/eino/retriever/retriever.go`
   - 看检索顺序、动态降级、vector search、repo search、fallback

6. `internal/eino/prompt/rag_prompt.go`
   - 看 Prompt 约束和上下文拼装

7. `internal/concurrency/concurrency.go` + `llm_client.go`
   - 看队列、并发、超时、取消，以及统一模型请求入口

8. `internal/eino/model/chat.go`
   - 看 Ark 真模型和 skeleton fallback

9. **`internal/eino/agent/coordinator.go`**
   - 看 Coordinator 接口 + 8 种枚举 + Factory

10. **`internal/eino/agent/profiles.go`**
    - 看 7 个预定义 AgentProfile

11. **`internal/eino/agent/task_state.go`**
    - 看 TaskState 字段和语义化写入方法

12. **`internal/eino/agent/supervisor_coordinator.go`**
    - 看事件循环：AsyncIterator → 翻译 → TaskState 写入（最完整样本）

13. **`internal/eino/agent/stateful_runner.go`**
    - 看断点续传：Interrupted → CheckPoint → Resume

14. **`internal/repository/postgres_conversation_store.go`**
    - 看三表结构、JSONB 整体覆盖、外键级联

15. `internal/tools/*.go`
    - 看 Agent 工具能力（与 Profile.AvailableTools 对应）

16. `eval/manual_rejudge.md` + `eval/eval_analysis_v2.md`
    - 看真实评估结果和问题定位

17. `eval/agent_evaluate.py` + `eval/agent_eval_results.json`
    - 看 Agent 的评估维度和当前短板

---

## 20. 你自己背项目时，可以记住的关键词

### 架构关键词

- Go 服务化 + Eino ADK
- Profile / Coordinator / TaskState / Persistence 四层
- 7 个 Profile + 8 种 Coordinator
- Pull 模式事件翻译（AsyncIterator → AddStepTrace）
- TaskState 共享黑板（Plan / Evidence / Metrics / Citations / StepTraces）
- StatefulRunner + CheckPointStore（断点续传）
- 三表 + JSONB 整体覆盖（conversations / messages / conversation_contexts）
- HybridRetriever + pgvector
- unified LLMClient + bounded queue + maxConcurrency
- citation / stream

### 优点关键词

- 架构抽象清晰、可插拔
- 主链路完整
- 复用 Eino ADK 而不是造轮子
- 统一并发控制
- TaskState 字段齐全（含可观测字段预留）
- 有评估闭环 + 人工复判

### 缺点关键词

- 可观测性字段预留但未采集
- CheckPointStore 默认 InMemory
- 只有 Supervisor 写完整 StepTrace
- Agent 完成率 11.8%
- 时延偏高（P95 39s）
- 仍有 skeleton/fallback 痕迹

---

## 21. 最后给你的面试提醒

### 要主动讲的

- 这是金融事实约束场景，不是泛聊天
- Profile / Coordinator 解耦是核心设计点
- TaskState 是 Pull 模式被 Coordinator 主动写入（不是 Observer）
- 复用 Eino ADK 而不是自己造（旧 `agent.go` 已 Deprecated）
- 你做了人工复判，敢报真实数字
- 你清楚架构 vs 工程化的差距在哪

### 不要夸大的

- 不要只拿早期 `95%` 自动评测说事
- 不要说 8 种 Coordinator 都跑过（只有 Supervisor / Pipeline / Plan 测过）
- 不要说 StepTrace 已经在采集真 latency / token / cost
- 不要说 CheckPoint 已经落 Postgres
- 不要把 fallback 环境结果包装成真实模型效果

### 最稳的整体表述

> 这个项目的价值不在于"已经做到多高准确率"，而在于我把金融场景下的 Multi-Agent 系统按 Profile / Coordinator / TaskState / Persistence 四层抽象搭起来了——架构层接近生产门口，工程化层和业务效果都有明确的下一步路径，而且我能说清楚每一层"做到了什么、距离生产差什么、下一步怎么做"。
