---
name: oneapi-wnbgj
description: 连接 oneapi.wnbgj.com（one-api-pro 网关）的模型能力。当需要查询该网关当前可用的模型及其能力、通过 OpenAI 兼容 /v1 端点调用文本对话/生图/图片编辑/语音合成/语音识别/向量/重排/视频生成、或排查模型连接与鉴权问题时使用。关键词：oneapi、oneapi.wnbgj.com、网关模型、查询可用模型、模型能力、生图、TTS、语音合成、embedding、rerank、视频生成。
agent_created: true
---

# OneAPI wnbgj 网关连接

## 用途

提供连接到 `https://oneapi.wnbgj.com` 的标准化流程：先按账号权限发现可用模型及其能力，再按操作类型选用正确的端点和请求格式调用。网关把多家上游（阿里百炼、火山方舟、OpenAI 兼容等）统一成 OpenAI 风格接口，客户端只需一套 Bearer 鉴权。

## 关键前提：两种凭据，绝不可混用

| 凭据 | 环境变量 | 用途 | 请求头 |
| --- | --- | --- | --- |
| 模型 Token（`sk-...`） | `ONEAPI_API_KEY` | 调用 `/v1/*` 推理端点 | `Authorization: Bearer sk-xxx` |
| 账号 Access Token（UUID） | `ONEAPI_ACCESS_TOKEN` | 调用 `/api/*` 管理端点（如模型发现 `/api/model/options`） | `Authorization: Bearer <uuid>` |

- 不要让用户把账号 Access Token 发给 `/v1/*`，也不要把 `sk-` 令牌发给 `/api/*`。
- 脚本从上述环境变量读取凭据，**不内置、不硬编码任何 token**。若未设置，提示用户自行设置后再继续；不要尝试替用户创建 token。
- 可选环境变量 `ONEAPI_BASE_URL`，默认 `https://oneapi.wnbgj.com`。

## 工作流

### Step 1：发现当前可用模型（必须先做）

优先使用带能力标记的账号级接口；`/v1/models` 只返回模型名，没有能力信息。

```sh
# 列出账号可见模型 + 能力（需要 ONEAPI_ACCESS_TOKEN）
python3 <skill_dir>/scripts/oneapi.py models
python3 <skill_dir>/scripts/oneapi.py models --operation audio.speech   # 只看支持 TTS 的

# 无账号 token 时退化为 OpenAI 兼容列表（只需要 ONEAPI_API_KEY）
python3 <skill_dir>/scripts/oneapi.py models --fallback
```

等价的 curl（细节见 `references/api-endpoints.md`）：

```sh
curl -H "Authorization: Bearer $ONEAPI_ACCESS_TOKEN" \
  "https://oneapi.wnbgj.com/api/model/options?operation=image.generate"
```

返回里的 `capabilities.operations` 声明模型支持哪些操作，`gateway_support` 声明网关实际执行模式（sync/stream/async）。**两者取交集**才是真正可用的能力；目录里出现某项能力不代表适配器已实现。

### Step 2：按操作选端点并调用

| 能力操作 | 端点 | 请求要点 |
| --- | --- | --- |
| `chat.generate` | `POST /v1/chat/completions` | `messages` + `model`，支持 `stream` |
| `image.generate` | `POST /v1/images/generations` | `prompt`、`size`、`n`、`response_format` |
| `image.edit` | `POST /v1/images/edits` | `images`（公网 URL 或 data URI）+ `prompt`；**不支持 mask** |
| `audio.speech` | `POST /v1/audio/speech` | `input`、`voice`、`response_format`；返回二进制音频 |
| `audio.transcribe` | `POST /v1/audio/transcriptions` | `audio` 公网 URL 或 multipart `file` |
| `embedding.create` | `POST /v1/embeddings` | `input` 字符串或数组 |
| `rerank.score` | `POST /v1/rerank` | `query` + `documents`(1–500) |
| `video.generate` | `POST /v1/media/jobs` | **异步**；必须带 `Idempotency-Key` 头 |

用 CLI 调用（按操作自动选端点、自动带 `model`）：

```sh
python3 <skill_dir>/scripts/oneapi.py call \
  --operation chat.generate --model gpt-5.1 \
  --params '{"messages":[{"role":"user","content":"你好"}]}'

python3 <skill_dir>/scripts/oneapi.py call \
  --operation audio.speech --model cosyvoice-v3-flash \
  --params '{"input":"你好","voice":"longanhuan_v3","response_format":"wav"}' \
  --out /tmp/speech.wav

python3 <skill_dir>/scripts/oneapi.py call \
  --operation image.generate --model wan2.7-image \
  --params '{"prompt":"山间日出","size":"1080x1440","n":1}'
```

需要任意未封装的端点时用 `request`：

```sh
python3 <skill_dir>/scripts/oneapi.py request --path /v1/audio/translations --data '{"model":"whisper-1",...}'
python3 <skill_dir>/scripts/oneapi.py request --path /api/model/options --admin
```

完整的请求体字段、响应结构、curl 示例见 `references/api-endpoints.md`。

### Step 3：处理特殊模型与不确定结果

- **wan2.6-t2i**：走旧万相异步协议，但网关内部轮询后对客户端仍返回同步 JSON；直接用 `/v1/images/generations`。
- **cosyvoice-v3-flash / qwen-audio-3.1-tts-flash**：`voice` 必填且须与模型匹配；网关把上游音频 URL 下载后以二进制返回。
- **seed-tts-2.0**：`voice` 填火山音色 ID（如 `zh_female_vv_uranus_bigtts`）；上游鉴权由网关适配，客户端仍用统一 `Bearer sk-`。
- **wan2.6-t2v**：异步任务，用 `python3 <skill_dir>/scripts/oneapi.py job create/get/cancel/asset`；同一业务意图固定同一个 `Idempotency-Key`。
- 遇到 `outcome_unknown` / `settlement_pending` / `usage_pending` 或响应头 `x-should-retry:false` + `X-Inference-Id`：**不要自动重试或换模型 fallback**，先用 `GET /v1/inferences/<id>`（原创建 token）查询状态。

各 provider 的模型清单、参数约束与排错见 `references/model-guide.md`。

## 资源

- `scripts/oneapi.py`：模型发现 + 按操作调用 + 通用请求 + 视频任务的 CLI（仅用 Python 标准库）。
- `references/api-endpoints.md`：完整端点、认证、请求/响应结构与 curl 示例。
- `references/model-guide.md`：provider/模型清单、能力说明、特殊参数与排错。
