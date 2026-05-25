#!/usr/bin/env bash
# 检查 Tempo trace-by-id 与 Loki trace 日志关联是否正常
set -euo pipefail

TID="${1:-}"
if [ -z "$TID" ]; then
  echo "用法: $0 <trace_id>"
  echo "示例: $0 f6a6e8240538fe54a6493980cab8e23d"
  exit 1
fi

echo "==> Tempo GET /api/traces/$TID (无时间参数)"
CODE=$(curl -sf -o /tmp/trace.json -w "%{http_code}" "http://localhost:3200/api/traces/$TID" || true)
echo "HTTP $CODE"
if [ "$CODE" != "200" ]; then
  echo "FAIL: Tempo 无法按 trace_id 拉取 trace"
  exit 1
fi

echo "==> Tempo GET /api/traces/$TID?start=...&end=... (模拟 Grafana 旧行为)"
NOW=$(date +%s)
START=$((NOW - 3600))
END=$((NOW + 3600))
CODE2=$(curl -sf -o /dev/null -w "%{http_code}" "http://localhost:3200/api/traces/$TID?start=$START&end=$END" || true)
echo "HTTP $CODE2 (若 404 说明 Grafana traceQuery.timeShiftEnabled 应设为 false)"

echo "==> Loki 行过滤（24h range）"
START_NS=$(python3 -c "import time; print(int((time.time()-86400)*1e9))")
END_NS=$(python3 -c "import time; print(int(time.time()*1e9))")
LINES=$(curl -sf -G "http://localhost:3100/loki/api/v1/query_range" \
  --data-urlencode "query={job=\"stock_rag\"} |= \"$TID\"" \
  --data-urlencode "start=$START_NS" \
  --data-urlencode "end=$END_NS" \
  --data-urlencode "limit=100" | python3 -c "import sys,json; r=json.load(sys.stdin).get('data',{}).get('result',[]); print(sum(len(x.get('values',[])) for x in r))")
echo "命中 $LINES 条"
if [ "$LINES" = "0" ]; then
  echo "WARN: Loki 无日志。确认 LOG_FILE=logs/app.log 且 Promtail 在运行"
  exit 1
fi

echo "OK"
