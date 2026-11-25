#!/usr/bin/env bash
# diagnose_docker_503.sh — 诊断 Docker 版 one-api-pro 的 503「无可用渠道」
# 在【宿主机】执行（不是容器内），通过 docker inspect/exec/cp 收集证据。
# 只读：不修改容器、不修改数据库。
#
# 用法:
#   export ONEAPI_API_KEY='sk-...'          # 可选，用于行为探测
#   ./diagnose_docker_503.sh [容器名或ID]
#   ./diagnose_docker_503.sh 62fdc7186643
#
# 依赖: docker, python3 (用于查 SQLite); curl (可选)

set -u
CNAME="${1:-62fdc7186643}"
GW="${GW:-https://oneapi.wnbgj.com}"
MODELS="qwen-plus deepseek-v4-pro deepseek-v4-flash cosyvoice-v3-flash wan2.7-image"
TMPD=$(mktemp -d /tmp/diag503.XXXXXX)
trap 'rm -rf "$TMPD"' EXIT

hr() { echo; echo "============================================================"; echo "  $1"; echo "============================================================"; }

hr "0. 容器 / 镜像元信息（版本铁证）"
docker inspect "$CNAME" --format '
container_id : {{.Id}}
image        : {{.Image}}
created      : {{.Created}}
status       : {{.State.Status}} ({{.State.Health.Status}})
started_at   : {{.State.StartedAt}}
' 2>&1 || { echo "docker inspect 失败，检查容器名"; exit 1; }
IID=$(docker inspect "$CNAME" --format '{{.Image}}' 2>/dev/null)
echo "--- 镜像构建信息 ---"
docker image inspect "$IID" --format '
image_id     : {{.Id}}
image_created: {{.Created}}
image_tags   : {{json .RepoTags}}
' 2>&1
echo "--- 容器内当前时间 ---"
docker exec "$CNAME" date 2>&1 || true

hr "1. 容器内二进制（确认运行的是哪个构建）"
docker exec "$CNAME" sh -c '
  echo "--- /app/one-api-pro ---"
  ls -l /app/one-api-pro 2>/dev/null || echo "(not found)"
  md5sum /app/one-api-pro 2>/dev/null || echo "(no md5sum)"
  echo "--- 内嵌版本号 (ldflags common.Version) ---"
  strings /app/one-api-pro 2>/dev/null | grep -oE "v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?" | sort -u | head -10 || echo "(no strings)"
  echo "--- 进程 ---"
  ps aux 2>/dev/null | grep -i one-api | grep -v grep || echo "(no ps)"
' 2>&1 || echo "docker exec 失败"

hr "2. 关键环境变量（缓存模式）"
docker inspect "$CNAME" --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -iE 'MEMORY_CACHE|SYNC_FREQ|SQLITE|SQL_DSN|REDIS|LOG_DIR' \
  | sed -E 's/(DSN|PASSWORD|ADDR)=.*/\1=<set>/' || echo "(无匹配)"
echo "--- 容器内 SQLITE_PATH 实际值 ---"
docker exec "$CNAME" sh -c 'echo "SQLITE_PATH=${SQLITE_PATH:-<unset>}"; echo "MEMORY_CACHE_ENABLED=${MEMORY_CACHE_ENABLED:-<unset>}"' 2>&1 || true

hr "3. 拷贝数据库到宿主机（含 WAL）"
DBSRC=$(docker exec "$CNAME" sh -c 'echo "${SQLITE_PATH:-/app/data/one-api-pro.db}"' 2>/dev/null | tr -d '\r\n')
echo "DB in container: $DBSRC"
docker cp "$CNAME:$DBSRC" "$TMPD/one-api.db" 2>&1 || echo "docker cp 主库失败"
for suffix in -wal -shm; do
  docker cp "$CNAME:${DBSRC}${suffix}" "$TMPD/one-api.db${suffix}" 2>/dev/null || true
done
ls -la "$TMPD" 2>&1

hr "4. 数据库直查（Python sqlite3，只读）"
if [ -f "$TMPD/one-api.db" ]; then
python3 - "$TMPD/one-api.db" "$MODELS" <<'PY'
import sqlite3, sys, binascii
db, models = sys.argv[1], sys.argv[2].split()
con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
def q(sql):
    try: return con.execute(sql).fetchall()
    except Exception as e: return [("ERR", str(e))]

print("--- channels: id | group(raw) | status | models(raw) ---")
for r in q("SELECT id, `group`, status, models FROM channels ORDER BY id"):
    print("  ", r)
print("\n--- channels group 十六进制（查隐藏字符/空格）---")
for r in q("SELECT id, hex(`group`) FROM channels ORDER BY id"):
    print("  ", r)
print("\n--- abilities 按分组计数 ---")
for r in q("SELECT `group`, count(*) FROM abilities GROUP BY `group`"):
    print("  ", r)
print("\n--- abilities: default 分组下的目标模型 ---")
for m in models:
    r = q(f"SELECT count(*) FROM abilities WHERE `group`='default' AND model='{m}'")
    print(f"   default/{m:<22}: {r[0][0] if r else '?'}")
print("\n--- 复现选路 SQL（DB 直查路径, MemoryCacheEnabled=false）---")
for m in models:
    r = q(f"SELECT count(*) FROM channels WHERE status=1 AND (models LIKE '%{m}%' OR models='*') AND `group` LIKE '%default%'")
    print(f"   candidates(default/{m}) = {r[0][0] if r else '?'}")
print("\n--- 版本表（若有）---")
for r in q("SELECT * FROM version LIMIT 5") if q("SELECT count(*) FROM sqlite_master WHERE name='version'")[0][0] else []:
    print("  ", r)
con.close()
PY
else
  echo "数据库拷贝失败，跳过。若用 MySQL，需手动查（见文末 SQL）。"
fi

hr "5. HTTP 行为探测（宿主机 curl）"
if [ -n "${ONEAPI_API_KEY-}" ]; then
  for m in chat $MODELS; do
    body=$(printf '{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":5}' "$m")
    out=$(curl -sS --max-time 12 -w '\nHTTP_CODE:%{http_code}' "$GW/v1/chat/completions" \
      -H "Authorization: Bearer $ONEAPI_API_KEY" -H 'Content-Type: application/json' -d "$body" 2>&1)
    code=$(echo "$out" | grep -o 'HTTP_CODE:[0-9]*' | tail -1 | cut -d: -f2)
    printf "  model=%-22s HTTP %s  " "$m" "${code:-?}"
    echo "$out" | head -c 140 | tr '\n' ' '; echo
  done
else
  echo "ONEAPI_API_KEY 未设置，跳过。export 后重跑可拿到这项。"
fi

hr "6. /api/status（公开信息）"
curl -sS --max-time 10 "$GW/api/status" 2>&1 | head -c 400; echo

hr "判读指南"
cat <<'EOF'
- 若【1】二进制 md5/版本号与本地 HEAD 不一致，或镜像 Created 早于关键修复提交
  → 线上是旧代码，重新 build & push 镜像即可（latest 标签不会自动更新）。
- 若【2】MEMORY_CACHE_ENABLED 未设/为 true，且【4】abilities default 计数=0 但候选SQL>0
  → 缓存索引未构建，重启容器或修 SyncChannelCache。
- 若【4】channels 的 group hex 不是 64656661756c74（=default）
  → 隐藏字符导致匹配失败。
- 若【4】候选SQL=0 且 channels 看着正常 → group/models 值有坑，看 hex。
- 若【5】chat 通、真实名 503，且【4】一切正常 → 极可能是别名池路径绕过了索引，
  需给 GetChannelCandidates 加 DB 兜底。
EOF

hr "附：MySQL 手动 SQL（若 SQL_DSN 指向 MySQL）"
cat <<'EOF'
SELECT id, `group`, status, models FROM channels ORDER BY id;
SELECT `group`, count(*) FROM abilities GROUP BY `group`;
SELECT count(*) FROM channels WHERE status=1 AND (models LIKE '%qwen-plus%' OR models='*') AND `group` LIKE '%default%';
SELECT id, HEX(`group`) FROM channels ORDER BY id;
EOF

echo
echo "诊断结束。把完整输出贴回来即可。"
