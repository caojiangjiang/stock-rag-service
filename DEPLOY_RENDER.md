# Stock RAG Service - Render 免费部署指南

## 🚀 快速部署

### 前提条件
1. GitHub 账号
2. Render 账号（免费注册：https://render.com）
3. 代码已推送到 GitHub 仓库

### 部署步骤

#### 方法 1：使用 Render Dashboard（推荐）

1. **推送代码到 GitHub**
```bash
cd /Users/caojiangjiang/Downloads/trae_projects/job/projects/stock_rag
git init
git add .
git commit -m "Add Render deployment configuration"
git remote add origin https://github.com/YOUR_USERNAME/stock_rag.git
git push -u origin main
```

2. **登录 Render**
   - 访问 https://dashboard.render.com
   - 使用 GitHub 账号登录

3. **创建新服务**
   - 点击 "New +" 按钮
   - 选择 "Web Service"

4. **配置服务**
   - Connect your GitHub repository
   - 设置以下配置：
     - **Name**: `stock-rag-api`
     - **Region**: Oregon (免费)
     - **Branch**: `main`
     - **Runtime**: Docker
     - **Instance Type**: Free

5. **设置环境变量**
   
   在 Render Dashboard 的 Environment 页面添加以下变量：

   ```bash
   # 数据库配置（使用 Supabase）
   DB_HOST=db.uvbojcqbfobmqrjturza.supabase.co
   DB_PORT=5432
   DB_USER=postgres
   DB_PASSWORD=qinghuaUNIVER123
   DB_NAME=postgres
   DB_SSLMODE=require
   
   # API 密钥
   ARK_API_KEY=your_ark_api_key
   ARK_MODEL=doubao-seed-2-0-pro-260215
   
   # JWT 密钥
   JWT_SECRET=generate_a_random_string_here
   
   # LLM 配置
   LLM_MAX_QUEUE_SIZE=100
   LLM_MAX_CONCURRENCY=10
   LLM_MAX_WAIT_TIME_MS=30000
   ```

6. **部署**
   - 点击 "Create Web Service"
   - 等待构建和部署完成（约 2-3 分钟）
   - 访问 `https://stock-rag-api.onrender.com/health` 验证

#### 方法 2：使用 render.yaml（自动化部署）

1. **确保 render.yaml 已创建**（已完成）
2. **推送代码到 GitHub**
3. **在 Render Dashboard 中**
   - 点击 "New +"
   - 选择 "Blueprint"
   - Connect your GitHub repository
   - Render 会自动检测 `render.yaml` 文件
   - 点击 "Apply" 开始部署

---

## 📊 Render 免费套餐限制

| 限制 | 说明 |
|------|------|
| **休眠** | 15 分钟无活动后会休眠 |
| **冷启动** | 休眠后首次请求需 30 秒 + 启动 |
| **带宽** | 每月 100GB 带宽 |
| **实例** | 只能有 1 个免费实例 |
| **数据库** | PostgreSQL 90 天后过期 |

---

## 🗄️ 数据库配置

### 推荐：使用 Supabase（免费托管 PostgreSQL）

1. **创建 Supabase 项目**
   - 访问 https://supabase.com
   - 创建新项目
   - 获取数据库连接信息

2. **启用 pgvector 扩展**
   在 Supabase SQL Editor 中运行：
   ```sql
   CREATE EXTENSION vector;
   CREATE EXTENSION pg_trgm;
   ```

3. **配置环境变量**
   ```
   DB_HOST=db.your_project.supabase.co
   DB_PORT=5432
   DB_USER=postgres
   DB_PASSWORD=your_password
   DB_NAME=postgres
   DB_SSLMODE=require
   ```

---

## 🔧 常用 Render CLI 命令

### 安装 Render CLI
```bash
brew install render-cli
```

### 登录
```bash
render login
```

### 查看服务状态
```bash
render services list
```

### 查看日志
```bash
render logs --service=stock-rag-api
```

### 重启服务
```bash
render deploy --service=stock-rag-api
```

---

## 🐛 故障排查

### 构建失败
```bash
# 本地测试 Docker 构建
docker build -t stock-rag .
docker run -p 8080:8080 --env-file .env stock-rag
```

### 数据库连接失败
1. 检查 Supabase 是否允许所有 IP
   - 在 Supabase Dashboard → Settings → Database
   - 确保 "Connection Pooling" 已启用
   - 检查 SSL 要求

2. 测试连接
```bash
psql "postgresql://postgres:password@db.host.supabase.co:5432/postgres?sslmode=require"
```

### 服务无法启动
1. 检查环境变量是否完整
2. 查看 Render 日志
   ```bash
   render logs --service=stock-rag-api --tail=100
   ```

---

## 🔄 更新部署

### 自动部署
推送到 GitHub main 分支会自动触发部署

### 手动部署
```bash
git add .
git commit -m "Update code"
git push origin main
```

---

## 💰 成本优化

### 当前配置：完全免费
- ✅ Render Free Tier: $0
- ✅ Supabase Free Tier: $0
- ✅ 火山引擎 ARK: 按量付费（免费额度）

### 如需升级
- Render Paid: $7/月起
- Supabase Pro: $25/月起

---

## 📝 重要注意事项

1. **免费实例会休眠**
   - 15 分钟无请求会自动休眠
   - 首次请求会有冷启动延迟（30秒+）
   - 适合开发/测试，不适合生产

2. **数据库过期**
   - Render PostgreSQL 免费版 90 天过期
   - 建议使用 Supabase 替代

3. **带宽限制**
   - 每月 100GB，超过需付费
   - 监控使用量：Render Dashboard → Usage

4. **API 密钥安全**
   - 不要将密钥提交到 GitHub
   - 使用 Render Environment Variables

---

## 🔗 访问服务

部署成功后，服务地址：
```
https://stock-rag-api.onrender.com
```

健康检查：
```
https://stock-rag-api.onrender.com/health
```

API 文档（如已实现）：
```
https://stock-rag-api.onrender.com/api/docs
```

---

## ✅ 部署清单

- [ ] 代码推送到 GitHub
- [ ] Render 账号已注册
- [ ] Supabase 项目已创建
- [ ] pgvector 扩展已启用
- [ ] 环境变量已配置
- [ ] 服务已部署
- [ ] 健康检查通过
- [ ] 功能测试完成

---

## 🆘 获取帮助

- Render 文档：https://render.com/docs
- Supabase 文档：https://supabase.com/docs
- 项目 Issues：https://github.com/YOUR_USERNAME/stock_rag/issues
