# 模型、Provider 与参数指南

模型清单以线上 `/api/model/options` 为准；下列内容来自 one-api-pro v0.0.28 代码库，用于理解能力边界和选参。

## Provider 概览

| Provider | 上游 Base URL（服务端渠道配置） | 主要能力 |
| --- | --- | --- |
| `ali`（阿里云灵积/百炼，type 17） | `https://dashscope.aliyuncs.com`（可选地域/Workspace 端点） | chat / realtime / image / TTS / ASR / rerank / embedding / video |
| `alibailian`（百炼兼容模式，type 49） | `https://dashscope.aliyuncs.com/compatible-mode` | OpenAI 兼容 chat / embedding / image |
| `doubao`（火山方舟） | `https://ark.cn-beijing.volces.com/api/v3` 或 `/api/plan` | chat / TTS(seed-tts) / embedding |
| `openai` / `openai_compatible` / `azure` | 渠道自配 | 标准 OpenAI 协议全模态 |
| 其它 30+ provider | 渠道自配 | gemini / zhipu / deepseek 等，多为 chat |

上游 Key 和 Base URL 只由服务端渠道配置持有。客户端统一用网关 `sk-` token 调 `/v1/*`。

## 目录清单位置（代码库内）

- `relay/modelcatalog/catalog.go` — 精选模型清单（AsOf 2026-11-21）
- `relay/adaptor/provider/ali/constants.go` — ali 全量模型列表（约 220 项）
- `relay/adaptor/provider/doubao/adaptor.go` — doubao 模型列表
- `model/model_capability.go` — 内置能力声明（builtinModelCapabilities）
- `relay/operation_capabilities.go` — 适配器实际能力上限（RuntimeOperationModes）

## 按操作分类的常用模型

| 操作 | 代表模型 | 备注 |
| --- | --- | --- |
| chat.generate | `gpt-5.1`、`qwen3.7-max/plus`、`doubao-seed-2.1-pro/lite`、`deepseek-v4` 系、`glm-5.3`、`kimi-k3.1-pro` | 视觉 chat 用 compatible 渠道的 qwen3-vl 等 |
| image.generate | `wan2.7-image`、`wan2.7-image-pro`、`wan2.6-t2i`、`qwen-image` 系 | `wan2.6-t2i` 见下方特殊说明 |
| image.edit | `wan2.7-image`（或其编辑别名） | 1–9 张参考图，不支持 mask |
| audio.speech | `cosyvoice-v3-flash`、`cosyvoice-v3-plus`、`qwen-audio-3.1-tts-flash`、`seed-tts-2.0` | `voice` 必填 |
| audio.transcribe | `qwen3-asr-flash`、`whisper-1` | 原生 JSON 或 OpenAI multipart |
| embedding.create | `text-embedding-3-small`、`qwen3-vl-embedding` 系、`doubao-embedding-vision` | 注意多模态向量需带 usage |
| rerank.score | `gte-rerank-v2` | documents 1–500 |
| video.generate | `wan2.6-t2v` | 异步 jobs + 幂等 key |
| chat.generate(realtime) | `qwen3.5-omni-flash-realtime`、`qwen3.5-omni-plus-realtime` | WebSocket 手动轮次 |

已实现的原生家族支持 `-YYYY-MM-DD` / `-YYYYMMDD` 日期后缀；任意自定义后缀不获得原生能力。

## 特殊模型与参数

### wan2.6-t2i（旧万相文生图）
- 上游走 DashScope 异步 `text2image/image-synthesis`，网关内部每 2 秒轮询任务（最多 20 次）后返回同步 JSON。
- 客户端照常用 `/v1/images/generations`；`size` 里的 `x` 由网关转 `*`，支持 `negative_prompt`；`n` 上限 4。

### cosyvoice-v3-flash / qwen-audio-3.1-tts-flash（百炼 TTS）
- 网关把 OpenAI 请求转成 DashScope `SpeechSynthesizer`：
  `{"model":"cosyvoice-v3-flash","input":{"text":"...","voice":"longanhuan_v3","format":"wav","sample_rate":24000,"rate":1.1}}`
- `speed` → `rate`（0.5–2.0）；格式 `wav/mp3/pcm/opus`，默认 wav；`voice` 必填且须与模型匹配（常用如 `longwan`/`longyu`/`longfei`/`longanhuan_v3`，以渠道实际支持为准）。
- 上游返回音频 URL，网关下载后以二进制返回（≤32 MiB，禁止私网地址）。
- `cosyvoice-*` 按字符计费；`qwen-audio-3.1-tts-flash` / `plus` 按 token 计费。

### seed-tts-2.0（火山豆包原生 TTS）
- 上游是火山 V3 `https://openspeech.bytedance.com/api/v3/tts/unidirectional`，**与方舟 Base URL 无关**；网关用语音控制台 key + `X-Api-Resource-Id` 鉴权，客户端仍用统一 `Bearer sk-`。
- `voice` 填火山音色 ID（如 `zh_female_vv_uranus_bigtts`）；`speed` → `speech_rate = round((speed-1)*100)`，钳制 [-50,100]；`ogg` → `ogg_opus`。
- 上游 NDJSON 流式返回 base64 音频块，网关累积后返回二进制；`maxInputCharacters` 1024。

### wan2.6-t2v（视频生成）
- 异步任务：`/v1/media/jobs`（202）+ `GET /v1/media/jobs/:id` 轮询 + `GET /v1/media/assets/:id` 下载。
- 同一业务意图固定同一个 `Idempotency-Key`；请求体或 token 变化会返回 409。
- 资产 7 天有效，下载需原账号 + 原创建 token。

### gte-rerank-v2 / qwen3-asr-flash
- 强类型操作，`X-Inference-Id` 返回持久账务记录 ID。
- 结果不确定时返回 `x-should-retry:false`，必须查 `/v1/inferences/:id`，不要自动重试。

## 计费与单位（排错用）

价格表的 Input/Output 是每百万"明确计量单位"的价格，不同操作单位不同：
- 文本：token
- rerank：token；ASR：秒；视频：秒
- TTS：多数按字符，Qwen3.1 Flash TTS 按 token
- 图片：按实际输出张数；`per_request` 按次

若同模型多操作单位不一致，价格页会提示；启用前需核实。零价是有效配置。

## 排错清单

1. **"模型不可用"** → 先 `/api/model/options` 确认账号可见；再确认 token 的 `models` 白名单、账号分组、套餐限制。
2. **能力有但调用失败** → 对照 `capabilities.operations` ∩ `gateway_support`；目录声明 ≠ 适配器实现。
3. **TTS 报 voice/format 错误** → 检查 `voice` 是否与模型匹配、格式是否在允许列表。
4. **图片/ASR 报媒体不可达** → `images`/`audio` 必须是上游能访问的公网 HTTPS URL 或 data URI，不要用内网地址。
5. **`outcome_unknown` / `settlement_pending`** → 查 `/v1/inferences/:id`，禁止重试。
6. **想验证媒体模型是否 work** → 管理端模型目录页有媒体探测调试台（TTS / 生图可填参后直接预览），无需在生产侧试错；也支持 `POST /api/channel/test_operation/:id`（需 `confirm_cost:true`）。

## 管理端能力覆盖（管理员）

```http
PUT /api/model/capabilities
Authorization: Bearer <ADMIN_ACCESS_TOKEN>
Content-Type: application/json

{
  "provider": "ali",
  "model_name": "qwen3-asr-flash",
  "capabilities": {
    "primary_category": "audio",
    "operations": {"audio.transcribe": {"modes": ["sync"], "input_modalities": ["audio"], "output_modalities": ["text"]}}
  }
}
```
`capabilities: null` 恢复自动推断。人工声明无法启用适配器尚未实现的协议。
