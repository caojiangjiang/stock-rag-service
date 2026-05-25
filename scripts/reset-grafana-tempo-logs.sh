#!/usr/bin/env bash
# 重置 Grafana Tempo trace-to-logs 配置（解决 Logs for this span 加载全部日志）
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> 重启 Grafana（会重新加载 provisioning 并覆盖 Tempo 数据源）"
docker compose --profile monitoring up -d --force-recreate grafana

echo "==> 等待 Grafana 就绪..."
for i in $(seq 1 30); do
  if curl -sf http://localhost:3000/api/health >/dev/null 2>&1; then
  echo "Grafana is up."
  break
  fi
  sleep 2
done

echo ""
echo "验证 Tempo 数据源 trace-to-logs 配置："
echo "  打开 Grafana → Connections → Data sources → Tempo → Trace to logs"
echo "  应看到：Filter by trace ID = Enabled，Use custom query = 未勾选"
echo ""
echo "验证 Loki 查询（在 Explore 手动测一条 trace_id）："
echo '  {job="stock_rag", trace_id="<32位hex>"}'
echo ""
echo "若仍不对，可清空 Grafana 持久化卷后重来："
echo "  docker compose --profile monitoring stop grafana"
echo "  docker volume rm \$(docker volume ls -q | grep grafana_data)"
echo "  docker compose --profile monitoring up -d grafana"
