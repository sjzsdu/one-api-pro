#!/usr/bin/env bash
# diagnose_503.sh 诊断「当前分组 default 下对于模型 X 无可用渠道」503 根因
# Diagnose the "no available channel for group/model" 503 on one-api-pro.
# 版本: v0.0.28
# 日期: 2026-11-24
# 作者: Codex
#
# 用法 / Usage:
#   chmod +x diagnose_503.sh
#   # 在 one-api-pro 服务器上运行（网关所在机器）:
#   ./diagnose_503.sh
#   # 或指定数据库路径:
#   ./diagnose_503.sh /path/to/one-api.db
#
# 只读脚本：不修改任何数据。把完整输出复制回来即可。
# 最小化依赖：curl；sqlite3（若 DB 是 MySQL 则脚本会跳过 DB 部分，请手动执行文末 SQL）。

set -u

GW="${GW:-https://oneapi.wnbgj.com}"
MODELS="qwen-plus deepseek-v4-pro deepseek-v4-flash cosyvoice-v3-flash wan2.7-image"
GROUP="default"

hr() { echo; echo "============================================================"; echo "  $1"; echo "============================================================"; }

hr "0. 环境 / ENV"
echo "date        : $(date)"
echo "hostname    : $(hostname)"
echo "whoami      : $(whoami)"
echo "GW          : $GW"
echo "—— 相关环境变量（值已脱敏，只看是否设置）——"
for v in SQL_DSN LOG_SQL_DSN MEMORY_CACHE_ENABLED SYNC_FREQUENCY REDIS_ENABLED REDIS_ADDR; do
  val="${!v-}"
  if [ -n "$val" ]; then
    case "$v" in
      *DSN|*ADDR) echo "$v = <set> ($(printf '%s' "$val" | cut -c1-12)…)" ;;
      *) echo "$v = $val" ;;
    esac
  else
    echo "$v = <unset>"
  fi
done

hr "1. 线上二进制版本 / BINARY VERSION"
echo "--- 进程 ---"
ps aux | grep -iE 'one-api|oneapi' | grep -v grep || echo "(no matching process)"
echo "--- 二进制文件候选 ---"
for f in /opt/one-api/one-api /opt/one-api-pro/one-api-pro ./one-api ./one-api-pro /usr/local/bin/one-api; do
  [ -f "$f" ] && echo "$f : $(stat -c '%y %s bytes' "$f" 2>/dev/null || stat -f '%Sm %z bytes' "$f" 2>/dev/null)"
done
echo "--- /api/status ---"
curl -sS --max-time 10 "$GW/api/status" | head -c 500; echo
echo "--- 启动日志里的版本行（若日志可读）---"
for log in /opt/one-api/oneapi.log /opt/one-api-pro/one-api-pro.log /var/log/one-api.log ./oneapi.log; do
  if [ -f "$log" ]; then
    echo "[$log]"
    grep -iE 'started|version|reconcil|memory cache|channels synced' "$log" | tail -20
  fi
done

hr "2. 网关行为探测 / BEHAVIOR (需 ONEAPI_API_KEY)"
if [ -n "${ONEAPI_API_KEY-}" ]; then
  K="$ONEAPI_API_KEY"
  for m in chat $MODELS; do
    code=$(curl -sS --max-time 15 -o /tmp/diag503_resp -w '%{http_code}' "$GW/v1/chat/completions" \
      -H "Authorization: Bearer $K" -H 'Content-Type: application/json' \
      -d "{\"model\":\"$m\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"max_tokens\":5}")
    printf "model=%-22s HTTP %s  " "$m" "$code"
    head -c 160 /tmp/diag503_resp; echo
  done
else
  echo "ONEAPI_API_KEY 未设置，跳过。可 export 后重跑。"
fi

hr "3. SQLite 数据库直查 / DB INSPECTION"
DB_PATH="${1:-}"
if [ -z "$DB_PATH" ]; then
  echo "未指定 DB 路径，尝试常见位置…"
  for p in /opt/one-api/one-api.db /opt/one-api/data/one-api.db ./one-api.db ./data/one-api.db; do
    [ -f "$p" ] && DB_PATH="$p" && break
  done
fi
if command -v sqlite3 >/dev/null 2>&1 && [ -n "$DB_PATH" ] && [ -f "$DB_PATH" ]; then
  echo "DB = $DB_PATH"
  echo "--- channels（关键：group/models/status 的原始值，含隐藏字符用 quote() 显示）---"
  sqlite3 "$DB_PATH" "SELECT id, quote(group), status, quote(models), name FROM channels ORDER BY id;" 2>&1
  echo
  echo "--- abilities 里 default 分组的记录数 ---"
  sqlite3 "$DB_PATH" "SELECT count(*) FROM abilities WHERE \`group\`='default';" 2>&1
  echo "--- abilities 里 default + 目标模型 ---"
  for m in $MODELS; do
    n=$(sqlite3 "$DB_PATH" "SELECT count(*) FROM abilities WHERE \`group\`='default' AND model='$m';" 2>&1)
    echo "  default/$m : $n"
  done
  echo
  echo "--- 完全复现选路 SQL（MemoryCacheEnabled=false 走的路径）---"
  for m in $MODELS; do
    n=$(sqlite3 "$DB_PATH" "SELECT count(*) FROM channels WHERE status=1 AND (models LIKE '%$m%' OR models='*') AND \`group\` LIKE '%$GROUP%';" 2>&1)
    echo "  candidates($GROUP/$m) = $n"
  done
  echo
  echo "--- group 字段是否有隐藏字符（hex）---"
  sqlite3 "$DB_PATH" "SELECT id, hex(\`group\`), hex(models) FROM channels ORDER BY id;" 2>&1
else
  echo "sqlite3 不可用或 DB 未找到（DB_PATH='${DB_PATH}'）。"
  echo "若线上是 MySQL，请手动执行文末 SQL，或把 DB 路径作为第一个参数传入。"
fi

hr "4. 结论提示 / HINTS"
cat <<'EOF'
请把输出按此对照：
- 若【2】里 chat 通、真实模型名 503，且【3】里 channels 的 group/models 完全正常：
    · 若 abilities default 记录数=0 但候选 SQL>0  → 缓存索引未构建/未刷新（重启或修 SyncChannelCache）
    · 若候选 SQL=0                              → channels 数据有隐藏字符或 group 不匹配（看 hex）
    · 若二进制时间早于代码修复                    → 线上是旧版本，需重新部署
- 若【3】拿不到 DB，请把【1】【2】输出 + MySQL SQL 结果贴回。
EOF

hr "附：MySQL 手动查询 SQL（若用 MySQL 请复制执行）"
cat <<'EOF'
SELECT id, `group`, status, models, name FROM channels ORDER BY id;
SELECT `group`, count(*) FROM abilities GROUP BY `group`;
SELECT * FROM abilities WHERE `group`='default' AND model IN ('qwen-plus','deepseek-v4-pro','deepseek-v4-flash');
SELECT count(*) FROM channels WHERE status=1 AND (models LIKE '%qwen-plus%' OR models='*') AND `group` LIKE '%default%';
SELECT id, HEX(`group`), HEX(models) FROM channels ORDER BY id;
EOF

echo
echo "诊断结束。"
