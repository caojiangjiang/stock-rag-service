# GrafanaCloud 可观测性配置指南

## 📊 架构

```
本地开发 / Render 部署服务 
         ↓ 
 OTLP 埋点生成 Trace + Span（通用兼容）
         ↓ 
 上报到 OTLP 接收端（GrafanaCloud、Jaeger、Signoz、本地 OTel Collector 等）
         ↓ 
 业务数据统一读写 → Supabase Postgres
```

---

## 🚀 步骤 1：注册 GrafanaCloud 账号

1. 访问 https://grafana.com/
2. 点击 "Start free"
3. 注册账号（免费套餐包含：10K metrics, 50GB logs, 10GB traces）

---

## 🚀 步骤 2：创建 Service Account 和 Token

在新版 Grafana 中，API Keys 已整合到 Service Accounts：

1. 在 **Users and access** 页面，找到 **Service accounts** 选项
2. 点击 **Add service account**
3. 设置：
   - **Name**: `stock-rag-traces`
   - **Description**: `用于 Stock RAG 服务的追踪上报`
4. 点击 **Create service account**
5. 在 Service Account 页面，点击 **Add token**
6. 设置：
   - **Name**: `traces-token`
   - **Role**: `Editor`
   - **Expires**: 按需设置（可选）
7. 点击 **Generate token**
8. **复制生成的 Token**（只显示一次！）

---

## 🚀 步骤 3：获取 Tempo OTLP Endpoint

1. 在 Grafana Instance 中点击左侧菜单 **Connections > Data sources**
2. 找到 **Tempo** 数据源（GrafanaCloud 自带）
3. 进入后查看 **OTLP HTTP Endpoint**：
   ```
   https://otlp-gateway-prod-us-central1.grafana.net/otlp/v1/traces
   ```
4. User ID 在页面顶部可以找到

---

## 🚀 步骤 4：配置环境变量

### 本地开发（.env）
```bash
# 本地开发（使用 Jaeger OTLP）:
OTLP_ENDPOINT=http://localhost:4318/v1/traces
# 或者使用 GrafanaCloud:
OTLP_ENDPOINT=https://otlp-gateway-prod-us-central1.grafana.net/otlp/v1/traces
OTLP_USER=your_user_id
OTLP_KEY=your_service_account_token
```

### Render 部署
在 Render Dashboard 的 Environment 页面添加：

| 变量名 | 值 |
|--------|-----|
| `OTLP_ENDPOINT` | `https://otlp-gateway-prod-us-central1.grafana.net/otlp/v1/traces` |
| `OTLP_USER` | 你的 Grafana 用户 ID（caojiangjiang） |
| `OTLP_KEY` | Service Account Token（刚才创建） |

---

## 🚀 步骤 5：本地开发

### 使用本地 Jaeger（可选）
```bash
# 启动包含 Jaeger 的本地开发环境
docker-compose -f docker-compose.dev.yaml up -d

# Jaeger UI: http://localhost:16686
# Jaeger OTLP 端点: http://localhost:4318/v1/traces

# 启动应用
cd /Users/caojiangjiang/Downloads/trae_projects/job/projects/stock_rag
go run cmd/server/main.go --config configs/config.dev.yaml
```

### 使用 GrafanaCloud
```bash
# 设置环境变量（编辑 .env 或直接 export）
export OTLP_ENDPOINT=https://otlp-gateway-prod-us-central1.grafana.net/otlp/v1/traces
export OTLP_USER=your_user_id
export OTLP_KEY=your_service_account_token

# 启动应用
go run cmd/server/main.go --config configs/config.dev.yaml
```

---

## 🚀 步骤 6：查看 Trace

1. 在 Grafana 左侧菜单点击 **Explore**（🔍 图标）
2. 顶部选择 **Tempo** 数据源
3. 使用以下方式搜索：
   - **Search by Trace ID**（从应用日志中获取）
   - **Search by service name**: `stock_rag`
   - **Search by operation**: `LLMClient.handleNonStreamRequest`

---

## 📊 观测数据结构

每次 LLM 调用会记录以下 span 属性：

| 属性 | 说明 |
|------|------|
| `trace_id` | 追踪唯一标识 |
| `span_id` | span 唯一标识 |
| `latency_ms` | 延迟时间（毫秒） |
| `token_in` | 输入 token 数量 |
| `token_out` | 输出 token 数量 |
| `cost_usd` | 成本估算（美元） |
| `request_id` | 请求 ID |
| `request_task_type` | 任务类型（agent/rag/stream） |
| `request_priority` | 优先级 |

---

## 📈 日志输出示例

```
[LLM Trace] request=req-xxx, trace_id=abc123..., span_id=def456..., latency=1250ms, token_in=150, token_out=300, cost_usd=0.00105
```

复制 `trace_id` 在 Grafana Explore 中搜索即可看到完整链路！

---

## 🔄 切换 OTLP 接收端

### 使用本地 Jaeger
```bash
export OTLP_ENDPOINT=http://localhost:4318/v1/traces
# 启动本地环境
docker-compose -f docker-compose.dev.yaml up -d
```

### 使用 GrafanaCloud
```bash
export OTLP_ENDPOINT=https://otlp-gateway-prod-us-central1.grafana.net/otlp/v1/traces
export OTLP_USER=your_user_id
export OTLP_KEY=your_service_account_token
```

### 使用其他 OTLP 兼容系统
支持所有 OTLP 协议的分布式追踪系统：
- ✅ GrafanaCloud (Tempo)
- ✅ Jaeger (OTLP endpoint)
- ✅ Signoz
- ✅ OpenTelemetry Collector
- ✅ Lightstep / New Relic / Datadog（需要对应配置）
- ✅ 其他兼容 OTLP 的系统

---

## 📈 免费套餐限制

| 资源 | 免费额度 |
|------|----------|
| Metrics | 10,000 |
| Logs | 50 GB/month |
| Traces | 10 GB/month |
| Dashboards | 无限制 |

---

## 🔍 调试

### 验证 OTLP 配置是否生效
```bash
# 启动应用时查看日志
go run cmd/server/main.go --config configs/config.dev.yaml

# 你应该看到:
# Using OTLP exporter: http://localhost:4318/v1/traces
# 或
# Using OTLP exporter: https://otlp-gateway-prod-us-central1.grafana.net/otlp/v1/traces
```

### 测试 Trace 上报
```bash
# 发送测试请求
curl -X POST http://localhost:8080/api/v1/query \
  -H "Content-Type: application/json" \
  -d '{"question": "测试问题", "stock_code": "000001"}'

# 查看日志获取 trace_id
grep "trace_id" logs/app.log
```

---

## ✅ 配置清单

- [ ] GrafanaCloud 账号已注册
- [ ] Service Account 已创建，Token 已生成
- [ ] OTLP Endpoint 已获取（GrafanaCloud Tempo 或本地 Jaeger）
- [ ] 环境变量已配置（OTLP_ENDPOINT/OTLP_USER/OTLP_KEY）
- [ ] 应用已重启
- [ ] 测试请求已发送
- [ ] Trace 在 Grafana Explore 中可见
