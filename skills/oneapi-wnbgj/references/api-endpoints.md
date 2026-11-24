# OneAPI wnbgj 网关 API 端点参考

基于 one-api-pro v0.0.28 代码库与 `docs/gateway-multimodal.md`。所有 `/v1/*` 为 OpenAI 兼容推理接口，`/api/*` 为管理接口，两者凭据不可混用。

## 基础与认证

| 项 | 值 |
| --- | --- |
| Base URL | `https://oneapi.wnbgj.com` |
| 推理前缀 | `/v1` |
| 管理前缀 | `/api` |
| 推理鉴权 | `Authorization: Bearer sk-<token>`（可取账号 Access Token 后从页面创建） |
| 管理鉴权 | `Authorization: Bearer <账号 UUID Access Token>` 或登录 Cookie |
| 指定渠道 | `sk-<key>-<channel_id>`（仅管理员可用，普通用户 403） |
| Anthropic 兼容 | `/v1/messages` 另接受 `x-api-key` |

管理接口统一返回 `{"success":bool,"message":string,"data":...}`；推理接口返回对应协议原生响应。

## 模型发现

### GET /api/model/options（推荐，带能力）

按账号权限过滤，不泄露渠道/供应商/内部模型 ID。管理员加 `?scope=all`。

```sh
curl -H "Authorization: Bearer $ONEAPI_ACCESS_TOKEN" \
  'https://oneapi.wnbgj.com/api/model/options?operation=image.generate'
```

响应 `data[]` 元素：
```json
{
  "model_name": "wan2.7-image",
  "display_name": "Wan 2.7 Image",
  "capabilities": {
    "primary_category": "image",
    "operations": {
      "image.generate": {"modes": ["sync"], "output_modalities": ["image"]},
      "image.edit": {"modes": ["sync"]}
    },
    "source": "builtin"
  },
  "gateway_support": {"image.generate": ["sync"], "image.edit": ["sync"]}
}
```
`capabilities.operations` 是声明能力，`gateway_support` 是适配器实际能力。可用能力取交集。

### GET /v1/models（OpenAI 兼容，无能力字段）
```sh
curl -H "Authorization: Bearer $ONEAPI_API_KEY" https://oneapi.wnbgj.com/v1/models
# {"object":"list","data":[{"id":"gpt-5.1","object":"model","owned_by":"openai",...}]}
```
列表严格按令牌的模型白名单返回。若令牌白名单含 `*`，网关会把它**展开为当前分组实际可调用的具体模型**（不是返回字面 `*`），因此启用 `validate_model=True` 的客户端能精确匹配到 `qwen-plus` 等真实模型名；白名单里显式列出的模型也会保留。若列表为空或不含目标模型，说明该令牌无权或该分组无可用渠道。

其它发现端点：
- `GET /api/user/available_models` — 账号可用模型名数组
- `GET /api/models` — 按渠道分组的模型映射
- `GET /api/channel/model_catalog`（管理员）— 含 `capabilities` / `gateway_support` / `probe_operations`
- `GET /api/channel/defaults`（管理员）— 各渠道类型 BaseURL 与精选模型

## 推理端点

所有示例假设 `export TOKEN="$ONEAPI_API_KEY"`，`GW=https://oneapi.wnbgj.com`。

### chat.generate — POST /v1/chat/completions
```sh
curl "$GW/v1/chat/completions" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"gpt-5.1","messages":[{"role":"user","content":"你好"}],"stream":false}'
```
请求常用字段：`model`、`messages[]`、`stream`、`temperature`、`top_p`、`max_tokens`、`tools`、`tool_choice`、`response_format`、`reasoning_effort`、`seed`、`user`。
响应：`{id,model,object:"chat.completion",created,choices:[{index,message,finish_reason}],usage:{prompt_tokens,completion_tokens,total_tokens}}`。

同族：`POST /v1/completions`、`POST /v1/messages`（Anthropic）、`POST /v1/responses`、`POST /v1/edits`。

### image.generate — POST /v1/images/generations
```sh
curl "$GW/v1/images/generations" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"wan2.7-image","prompt":"山间日出","n":1,"size":"1080x1440","response_format":"url"}'
```
请求：`model`、`prompt`(必填)、`n`、`size`、`quality`、`style`、`response_format`(`url`/`b64_json`)、`seed`、`negative_prompt`。
响应：`{created,data:[{url?,b64_json?,revised_prompt?}]}`。

### image.edit — POST /v1/images/edits
```sh
curl "$GW/v1/images/edits" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"wan2.7-image","prompt":"背景改为浅蓝色","images":["https://media.example/a.png"],"n":1,"response_format":"url"}'
```
请求：`model`、`prompt`、`images`（1–9 个公网 URL 或 data URI）、`size`、`n`(1–4)、`response_format`。**不支持 mask**。也接受 multipart 单 `image` 字段（≤10 MiB）。
响应同 image.generate。

### audio.speech — POST /v1/audio/speech
```sh
curl "$GW/v1/audio/speech" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"cosyvoice-v3-flash","input":"你好","voice":"longanhuan_v3","response_format":"wav","speed":1.1}' \
  -o /tmp/out.wav
```
请求：`model`、`input`(≤4096 Unicode 字符，seed-tts-2.0 为 1024)、`voice`、`speed`(0.5–2)、`response_format`(`wav`/`mp3`/`pcm`/`opus`/`ogg_opus`)。
响应：二进制音频 + Content-Type；可能带 `X-Inference-Id`。

### audio.transcribe — POST /v1/audio/transcriptions
```sh
curl "$GW/v1/audio/transcriptions" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"qwen3-asr-flash","audio":"https://media.example/s.wav","language":"zh","response_format":"json"}'
```
请求：`model`、`audio`（公网 URL 或 data URI）、`language`、`response_format`(仅 `json`)。也可 multipart 上传 `file`（≤10 MiB）。Qwen ASR 最长 5 分钟。
原生响应：`{"text":"..."}`。

### embedding.create — POST /v1/embeddings
```sh
curl "$GW/v1/embeddings" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"text-embedding-3-small","input":["hello","world"],"encoding_format":"float"}'
```
另支持 `POST /v1/multimodal/embeddings`（DashScope 原生透传，必填 `model`）、`POST /v1/engines/:model/embeddings`。
响应：`{object:"list",data:[{object:"embedding",index,embedding:[]}],model,usage}`。

### rerank.score — POST /v1/rerank
```sh
curl "$GW/v1/rerank" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"gte-rerank-v2","query":"网关支持哪些音频能力","documents":["支持语音识别与语音合成","数据库迁移说明"],"top_n":1,"return_documents":true}'
```
请求：`model`、`query`、`documents`(1–500)、`top_n`(≤文档数)、`return_documents`。
响应：`{model,results:[{index,relevance_score,document}],usage:{total_tokens}}`。

### video.generate — POST /v1/media/jobs（异步，强制幂等）
```sh
curl "$GW/v1/media/jobs" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: video-order-example-001' \
  -d '{"model":"wan2.6-t2v","operation":"video.generate","prompt":"清晨海面上的帆船","size":"1280*720","duration":5}'
```
- 必须带 `Idempotency-Key`（≤128 字符）。同账号 + 同 key + 同 token + 同请求体 → 同一任务；不同则 409。
- 参数：`model`、`operation`、`prompt`、`negative_prompt`、`size`(`1280*720`/`720*1280`/`1920*1080`/`1080*1920`)、`duration`(2–15，默认 5)、`seed`。
- 返回 HTTP 202 + 任务本体。
- 任务操作：`GET /v1/media/jobs/:id`、`POST /v1/media/jobs/:id/cancel`、`GET /v1/media/assets/:id`（资产 7 天有效）。均要求原账号与原创建 token。

### realtime（WebSocket）
```http
GET /v1/realtime?model=qwen3.5-omni-flash-realtime&max_turns=4&max_output_tokens=1024&max_seconds=120
Authorization: Bearer YOUR_GATEWAY_TOKEN
Upgrade: websocket
```
Qwen Omni 手动轮次子集。握手后按顺序发送：`session.update` → `input_audio_buffer.append`(base64) → `input_audio_buffer.commit` → `response.create`，等 `response.done` 再下一轮。音色 Tina/Ethan；音频格式 pcm/pcm16/wav。需能设置握手头的服务端客户端。

## 不确定结果与对账（重要）

同步 typed 接口返回 `X-Inference-Id`；当响应带 `x-should-retry:false` + `X-Inference-Id`，或错误码为 `outcome_unknown` / `settlement_pending` / `usage_pending` / `submission_outcome_unknown` 时：

```http
GET /v1/inferences/<INFERENCE_ID>
Authorization: Bearer <原创建 token>
```

**禁止**自动重试、换模型或别名 fallback。查询结果只在 `settled` 后返回保存的结果；流式内容不保证保存。

视频任务进入 `unknown` / `pending_reconciliation` 时同样先确认上游，不要重建任务。管理员对账端点：`POST /api/inference/jobs/:id/reconcile`、`POST /api/inference/settlements/:id/reconcile`。

## 令牌管理（账号 Access Token 下）

- `GET /api/user/token` — 获取/重置账号 Access Token
- `GET|POST|PUT /api/token/`、`DELETE /api/token/:id` — 管理 sk- 模型令牌
- `GET /v1/dashboard/billing/subscription`、`GET /v1/dashboard/billing/usage` — 账单（OpenAI 兼容）

## 常见错误

| 现象 | 原因与处理 |
| --- | --- |
| 401 / invalid token | 用错凭据，`/v1` 要 `sk-`，`/api` 要账号 token |
| 403 channel forbidden | `sk-xxx-<id>` 指定渠道仅管理员可用 |
| 404 / model not found | 模型未在渠道启用或账号分组无权限；用 `/api/model/options` 复查 |
| 501 api_not_implemented | 该端点为占位（如 files/assistants/images/variations），网关未实现 |
| `outcome_unknown` | 执行结果未知，查 `/v1/inferences/:id`，不要重试 |
| 400 invalid voice / size | 对照 `references/model-guide.md` 的参数约束 |
