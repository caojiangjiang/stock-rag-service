# Stock RAG 项目分析与优化计划

> 更新日期：2026-06-04  
> 当前基线：`go test ./...` 全量通过

## 1. 项目现状

`stock_rag` 是一个股票投研 RAG / Agent 系统，核心由 Go 后端、静态 Web 前端、PostgreSQL + pgvector、Redis、Eino Agent、Ark 模型接入和 Prometheus / OTel / Grafana 观测栈组成。

当前代码已经具备这些生产化基础：

| 模块 | 当前状态 |
|------|----------|
| HTTP API | `/api/chat`、`/api/chat/stream`、`/api/agent/*`、`/documents`、`/health/*`、`/metrics` |
| RAG 检索 | pgvector + 关键词/混合检索；支持 stock_code、doc_type、time_range |
| Agent | Supervisor 默认执行器，支持 `AGENT_EXECUTOR=react` ReAct 执行器 |
| 协调器 | 支持自动选择和 `COORDINATOR_TYPE` 显式切换；规则可由 `configs/coordinator_rules.yaml` 配置 |
| 工具防护 | ToolGuard 已提供超时、重试、熔断和降级 JSON |
| 认证授权 | JWT、refresh token、黑名单、admin/user RBAC；导入接口需要 admin |
| 缓存与记忆 | Query 语义缓存、Chat 精确缓存、短期/中期/长期记忆结构 |
| 可观测性 | Prometheus 指标、OTel trace、结构化日志、Grafana/Loki/Tempo 配置 |
| 质量基线 | 当前全量单测通过 |

## 2. 已发现的优化点

| 优先级 | 优化点 | 风险 / 收益 |
|--------|--------|-------------|
| P0 | CI 缺失 | 本地测试通过无法自动保障，容易让编译或行为回归进入主干 |
| P0 | 文档与代码状态漂移 | README、优化计划、路由清单曾落后于当前代码，影响交接和部署 |
| P0 | 生产 secret 策略 | 未配置 `JWT_SECRET` 时仍会使用默认值，生产部署应强制失败或显式保护 |
| P1 | RAG 评测集不足 | 检索融合、rerank、缓存策略缺少可重复评估标准 |
| P1 | 输入校验分散 | ✅ 已落地第一版：Chat/RAG/Agent 入口统一长度、time_range/doc_type、stock_code、TopK 校验 |
| P1 | 外部 URL 工具 SSRF 风险 | ✅ 已落地第一版：网页抓取工具阻断 localhost、私有网段、链路本地、多播和未指定地址 |
| P1 | 安全审计基础薄弱 | ✅ 已落地第一版：认证与 Agent 关键动作输出结构化 audit 日志 |
| P1 | 大批量导入同步化 | 导入和向量化容易阻塞请求，应引入任务状态与异步处理 |
| P2 | 缓存策略细化 | 精确缓存/语义缓存 TTL、容量和 key 粒度还可按 mode / stock / time_range 调优 |
| P2 | 记忆写回策略 | 中长期记忆需要遗忘策略、摘要质量评估和用户级隔离校验 |
| P2 | 安全审计增强 | 导入、删除会话等更多关键操作审计；可查询审计表；统一日志脱敏 |
| P3 | 部署标准化 | Kubernetes / Render / Compose 配置需要统一探针、资源限制和环境变量说明 |

## 3. 本次已执行优化

| 项目 | 变更 |
|------|------|
| 文档纳管 | 调整 `.gitignore`，让 Markdown 与 `docs/` 默认可进入版本管理 |
| README 校准 | 更新当前真实接口、认证要求、旧版 RAG API 开关、推荐下一步 |
| 生产 secret 保护 | `GO_ENV=production/prod` 时强制要求非默认、非 placeholder、长度不少于 32 的 `JWT_SECRET` |
| 环境示例校准 | `.env.example` 默认使用 development，并标注生产 JWT_SECRET 要求 |
| 网页工具修复 | `TypedWebpageFetcher` 先从原始 HTML 提取标题，再提取正文 |
| UTF-8 截断 | 网页内容按 rune 截断，避免中文按字节截断导致乱码 |
| 回归测试 | 新增启动配置与网页抓取工具单测，覆盖 secret 校验、标题提取、正文清洗和中文截断 |
| 入口输入校验 | Chat/RAG/Agent 请求增加统一校验，阻断过长文本、非法股票代码、非法 doc_type/time_range 和异常 TopK |
| SSRF 防护 | `fetch_webpage` 拒绝 localhost、私有 IP、链路本地、多播、未指定地址和非 HTTP(S) URL |
| 审计日志 | Auth 注册/登录/刷新/登出与 Agent 执行/分析/运行输出结构化 `audit=true` 日志 |

## 4. 后续执行计划

### 阶段一：质量闸门与生产保护（P0）

1. 新增 CI workflow：`go test ./...`、`go build ./cmd/server`、`go vet ./...`。
2. 将 `.env.example` 进一步拆成最小本地示例和生产必填项，避免示例值误用。
3. 对 README、部署文档和 API 示例做一次端到端校验。

验收标准：PR 自动跑测试；生产缺少 secret 时启动失败；文档中的 curl 示例可复现。

### 阶段二：RAG 评测与检索调优（P1）

1. 建立 `eval/` 问题集：覆盖年报、公告、指标对比、风险事件、跨期分析。
2. 输出评测脚本：记录 hit rate、citation 命中、answer completeness、P95 延迟。
3. 调参 hybrid 检索、rerank 阈值、TopK 和缓存 key 粒度。
4. 为典型问题保存基线报告，后续优化必须对比基线。

验收标准：每次检索改动能给出量化对比，至少覆盖 30 条代表性问题。

### 阶段三：输入校验与安全审计（P1，第一版已完成）

1. ✅ 抽取通用请求校验：message/query 长度、stock_code 格式、time_range/doc_type 枚举。
2. ✅ 对外部 URL 工具增加 host/IP 过滤策略，降低 SSRF 风险。
3. ✅ 为登录、刷新、登出、Agent 执行等关键操作增加结构化审计日志。
4. 继续补齐导入、删除会话等高风险操作审计。
5. 对日志中的 token、手机号、密码类字段做统一脱敏。

验收标准：恶意 payload 返回 400；结构化日志可按 `audit/action/user_id/result` 查询；后续落审计表后可按 user/action/request_id 查询。

### 阶段四：导入与长任务异步化（P2）

1. 将文档导入拆成 create task、process task、query task status 三步。
2. 优先使用 Redis Stream 或 PostgreSQL job table，避免引入过重依赖。
3. 增加失败重试、幂等 key、进度百分比和错误详情。
4. 在前端展示导入状态。

验收标准：大批量导入不阻塞 `/api/chat`；失败任务可重试且不会重复写入。

### 阶段五：部署与容量规划（P2/P3）

1. 统一 Docker Compose、Render 与未来 Kubernetes 的环境变量说明。
2. 为服务设置 CPU/内存建议、连接池容量建议和 Redis/PostgreSQL 规格建议。
3. 补充 k6 或 vegeta 压测脚本，记录 Chat/RAG/Agent 三类路径性能。
4. 形成 Grafana dashboard 与告警阈值说明。

验收标准：新环境可按文档部署；压测报告能说明当前容量边界。

## 5. 建议 PR 拆分

| PR | 范围 |
|----|------|
| PR-1 | CI + production secret guard + `.env.example` 整理 |
| PR-2 | 请求校验公共包 + Chat/RAG/Agent 接入 |
| PR-3 | RAG 评测集与评测脚本 |
| PR-4 | hybrid/rerank/cache 调优 |
| PR-5 | 文档导入异步任务 |
| PR-6 | 审计日志与日志脱敏 |
| PR-7 | 部署与压测文档 |
