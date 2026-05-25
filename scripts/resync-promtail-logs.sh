#!/usr/bin/env bash
# 重置 Promtail 采集位点并重新推送 logs/app.log（修复 trace 时间与 Loki 时间戳不一致）
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> 重建 Promtail（清空 positions，重新读取 app.log 并解析 JSON timestamp）"
docker compose --profile monitoring stop promtail
docker compose --profile monitoring rm -f promtail 2>/dev/null || true

# 新容器会从 offset 0 开始读；旧 positions 在容器内 /tmp/positions.yaml
docker compose --profile monitoring up -d --force-recreate promtail

echo "==> 等待 Promtail 重新读取 app.log..."
sleep 12

if docker logs stock_rag_promtail 2>&1 | tail -3 | grep -q "error creating promtail"; then
  echo "ERROR: Promtail 启动失败，请检查 configs/promtail/promtail.yml"
  docker logs stock_rag_promtail 2>&1 | tail -10
  exit 1
fi

TID="${1:-c22a0fc4d49b35f3d29eaf833cae78d3}"
echo "==> 验证 trace $TID 在 span 时间窗口内是否可查（11:11:59 ~ 11:12:27 UTC）"
COUNT=$(curl -sf -G "http://localhost:3100/loki/api/v1/query_range" \
  --data-urlencode "query={job=\"stock_rag\"} |= \"$TID\"" \
  --data-urlencode "start=2026-05-25T11:11:59.000000000Z" \
  --data-urlencode "end=2026-05-25T11:12:27.000000000Z" \
  --data-urlencode "limit=50" | python3 -c "import sys,json; r=json.load(sys.stdin).get('data',{}).get('result',[]); print(sum(len(x.get('values',[])) for x in r))")

echo "命中 $COUNT 条日志"
if [ "$COUNT" = "0" ]; then
  echo "仍无日志：请确认 logs/app.log 含该 trace，或发一条新请求后再试"
  exit 1
fi

echo "==> 重建 Grafana Tempo 数据源"
docker compose --profile monitoring up -d --force-recreate grafana
echo "完成。请强刷 Grafana 后重试 Logs for this span"
