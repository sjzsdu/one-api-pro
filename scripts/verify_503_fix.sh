#!/usr/bin/env bash
# verify_503_fix.sh 验收 one-api-pro 的 503「无可用渠道」修复是否生效
# Verify that the PostgreSQL "no available channel" 503 fix is effective on the live deployment.
# 版本: v0.0.28
# 日期: 2026-11-25
# 作者: MCode
#
# 用法:
#   export ONEAPI_API_KEY='sk-...'          # 用于 HTTP 行为探测
#   ./verify_503_fix.sh [容器名或ID]         # 默认沿用 diagnose_docker_503.sh 的容器名
#
# 只读脚本：不修改容器、不修改数据库。
# 背景：2026-11-24 线上事故——PostgreSQL 部署下渠道候选查询硬编码 MySQL 反引号，
# 整条 SQL syntax error → 候选恒为空 → 所有模型 503，而 /v1/models 照常返回清单。
# 修复后候选查询按方言生成列名引用，本脚本逐项确认。

set -u
CNAME="${1:-62fdc7186643}"
GW="${GW:-https://oneapi.wnbgj.com}"
GROUP="${GROUP:-default}"
MODELS="${MODELS:-qwen-plus cosyvoice-v3-flash wan2.7-image}"
PGPORT="${PGPORT:-5432}"

hr() { echo; echo "============================================================"; echo "  $1"; echo "============================================================"; }

hr "1. 部署形态 / DEPLOYMENT"
docker inspect "$CNAME" --format 'image={{.Image}} created={{.Created}} started={{.State.StartedAt}}' 2>&1 || {
  echo "docker inspect 失败，请确认容器名：docker ps"; exit 1; }
DSN=$(docker inspect "$CNAME" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null \
  | sed -n 's/^SQL_DSN=//p')
MEMCACHE=$(docker inspect "$CNAME" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null \
  | sed -n 's/^MEMORY_CACHE_ENABLED=//p')
case "$DSN" in
  postgres://*) DBKIND="PostgreSQL" ;;
  "")             DBKIND="SQLite（未设 SQL_DSN）" ;;
  *)             DBKIND="MySQL" ;;
esac
echo "数据库类型    : $DBKIND"
echo "MEMORY_CACHE_ENABLED = ${MEMCACHE:-<未设置，即 false>}"
if [ -n "$DSN" ]; then
  echo "SQL_DSN       = $(printf '%s' "$DSN" | sed -E 's#(://[^:]+):[^@]+@#\1:***@#')"
else
  echo "SQL_DSN       = <未设置>"
fi
echo "线上版本      : $(curl -sS --max-time 10 "$GW/api/status" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"

hr "2. 候选查询方言验证 / CANDIDATE QUERY"
# 直接在数据库上跑新旧两条候选查询：旧写法（反引号）在 PostgreSQL 上必然 syntax error。
if [ "$DBKIND" = "PostgreSQL" ]; then
  echo "--- 旧写法（反引号，修复前的线上行为）---"
  psql "$DSN" -c "SELECT count(*) AS old_query_rows FROM channels WHERE status=1 AND (models LIKE '%qwen-plus%' OR models='*') AND \`group\` LIKE '%$GROUP%';" 2>&1 | head -5
  echo "--- 新写法（双引号，修复后的行为）---"
  psql "$DSN" -c "SELECT count(*) AS new_query_rows FROM channels WHERE status=1 AND (models LIKE '%qwen-plus%' OR models='*') AND \"group\" LIKE '%$GROUP%';" 2>&1 | head -5
  echo
  echo "--- 各目标模型在 $GROUP 分组下的候选渠道数 ---"
  for m in $MODELS; do
    n=$(psql "$DSN" -tAc "SELECT count(*) FROM channels WHERE status=1 AND (models LIKE '%$m%' OR models='*') AND \"group\" LIKE '%$GROUP%';" 2>&1 | tr -d ' ')
    echo "  $GROUP/$m : ${n:-查询失败}"
  done
  echo
  echo "--- 启用渠道的分组与模型（确认分组确实挂了渠道）---"
  psql "$DSN" -c "SELECT id, name, status, \"group\", left(models, 60) AS models FROM channels ORDER BY id;" 2>&1 | head -30
else
  echo "非 PostgreSQL 部署，跳过方言对比查询。"
  echo "若为 MySQL/SQLite，反引号可用，请直接确认分组下存在启用渠道："
  echo "  SELECT id, \`group\`, status, models FROM channels ORDER BY id;"
fi

hr "3. 容器日志中的 SQL 报错 / SQL ERRORS IN LOGS"
docker logs --since 24h "$CNAME" 2>&1 \
  | grep -iE "syntax error at or near|failed to get channel candidates" | tail -10 \
  || true
echo "（修复后应不再出现 failed to get channel candidates）"

hr "4. 线上行为探测 / BEHAVIOR"
if [ -n "${ONEAPI_API_KEY-}" ]; then
  K="$ONEAPI_API_KEY"
  for spec in "chat:/v1/chat/completions:{\"model\":\"qwen-plus\",\"messages\":[{\"role\":\"user\",\"content\":\"回复OK\"}],\"max_tokens\":10}" \
              "tts:/v1/audio/speech:{\"model\":\"cosyvoice-v3-flash\",\"input\":\"测试\",\"voice\":\"longanhuan_v3\",\"response_format\":\"wav\"}" \
              "image:/v1/images/generations:{\"model\":\"wan2.7-image\",\"prompt\":\"a cat\",\"n\":1,\"size\":\"1024x1024\"}"; do
    name="${spec%%:*}"; rest="${spec#*:}"; path="${rest%%:*}"; body="${rest#*:}"
    code=$(curl -sS --max-time 60 -o /tmp/verify503_body -w '%{http_code}' "$GW$path" \
      -H "Authorization: Bearer $K" -H 'Content-Type: application/json' -d "$body")
    printf "  %-6s %-26s HTTP %s  " "$name" "$path" "$code"
    head -c 120 /tmp/verify503_body | tr '\n' ' '; echo
  done
  echo
  echo "  模型清单："
  curl -sS --max-time 20 "$GW/v1/models" -H "Authorization: Bearer $K" \
    | tr ',' '\n' | grep '"id"' | head -30
else
  echo "ONEAPI_API_KEY 未设置，跳过。export 后重跑即可拿到这一项。"
fi

hr "判读指南"
cat <<'EOF'
- 第 2 项 new_query_rows > 0 且旧写法报 syntax error  → 方言问题确认，修复后候选查询即可用。
- 第 4 项三类接口均非 503                → 修复生效。
- 仍为 503 且 new_query_rows = 0         → 该分组确实没有启用渠道，请在后台确认渠道的分组与模型。
- 仍为 503 且 new_query_rows > 0         → 线上二进制仍是旧版本，请确认镜像已重建并重启。
EOF

echo
echo "验收结束。"
