<!--
gateway-multimodal.md 多模态网关调用与运维说明 / Multimodal gateway operations and API guide
版本: v0.0.28
日期: 2026-11-23
作者: Codex
-->

# 多模态网关调用与运维

网关按“操作 + 执行模式”选择渠道，模型分组用于发现和授权。模型目录出现某项能力，并不代表所有供应商协议都已实现；请求最终使用目录声明能力与渠道适配器实际能力的交集。

## 当前支持范围

| 形态 | 操作 | 执行模式与入口 | 说明 |
| --- | --- | --- | --- |
| 文本、视觉、音频理解对话 | `chat.generate` | sync / stream，`POST /v1/chat/completions` | 沿用现有供应商适配器；具体输入依模型声明能力 |
| 向量 | `embedding.create` | sync，`POST /v1/embeddings` | 包括已有多模态向量适配 |
| 文生图 | `image.generate` | sync，`POST /v1/images/generations` | 沿用已有图像渠道 |
| 语音合成 | `audio.speech` | sync，`POST /v1/audio/speech` | 沿用已有 TTS 适配 |
| 语音识别 | `audio.transcribe` | sync，`POST /v1/audio/transcriptions` | 原 OpenAI multipart 协议；新增百炼 `qwen3-asr-flash` 原生协议 |
| 重排 | `rerank.score` | sync，`POST /v1/rerank` | 首个原生适配为百炼 `gte-rerank-v2` |
| 图片编辑 | `image.edit` | sync，`POST /v1/images/edits` | 首个原生适配为百炼 Wan2.7；支持参考图片消息，不支持 mask |
| 视频生成 | `video.generate` | async，`POST /v1/media/jobs` | 首个模型为百炼 `wan2.6-t2v` |
| 双向实时语音对话 | `chat.generate` | realtime，`GET /v1/realtime` WebSocket | 首个协议为 Qwen Omni 手动轮次子集，见下文 |

已有音频翻译、内容审核接口继续受适配器协议和模型声明约束。图片变体、视频编辑、Fun-ASR 长录音异步任务、跨供应商统一实时协议、WebRTC 和对象存储后端不在此次实现范围。

百炼渠道配置 API 根地址，例如北京 `https://dashscope.aliyuncs.com` 或新加坡 `https://dashscope-intl.aliyuncs.com`，使用对应地域的 API Key。渠道中的模型名与映射目标必须是该地域实际可用的模型。不要把 `/compatible-mode/v1` 作为原生能力渠道根地址。渠道 Key 仅由服务端发送给供应商。 旧百炼原生 chat 转换只支持文本消息，网关会拒绝 image/audio/video 输入以避免静默丢失；视觉等多模态 chat 请使用已支持对应协议的 compatible 渠道。

## 模型分组、映射与权限

模型页及 Token、套餐、模型别名、价格设置共用分类数据；渠道模型勾选也按相同分类展示。能力来源依次体现为人工覆盖、供应商目录、内置声明、名称推断或别名；无法识别的模型归入“未分类”。名称推断用于发现，人工覆盖用于修正实际能力。

已实现的百炼原生家族支持合法 `-YYYY-MM-DD` / `-YYYYMMDD` 日期后缀，调度使用家族能力，发给上游的模型 ID 保留原值；这不保证供应商已开通某个日期版本。任意自定义后缀不会自动取得原生协议能力。Qwen Omni realtime 仅开放已经实现的 WebSocket 协议，不能据此调用普通 chat 接口。

能力与渠道目录使用最多 30 秒的本地缓存，本进程修改目录或渠道配置时失效；其他实例的配置变更依赖 TTL 收敛。账号权限仍在每次请求中校验，不缓存跨账号授权结果。

账号可用模型选项：

```http
GET /api/model/options
Authorization: Bearer ACCOUNT_ACCESS_TOKEN
```

此接口按账号权限过滤，不向普通账号返回渠道、供应商或映射后的内部模型 ID。管理员可用 `?scope=all` 获取完整管理范围。别名只展示全部可见目标共同支持的操作与模式，避免一个目标不能执行时仍被路由过去。

管理员修改能力：

```http
PUT /api/model/capabilities
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{
  "provider": "ali",
  "model_name": "qwen3-asr-flash",
  "capabilities": {
    "primary_category": "audio",
    "operations": {
      "audio.transcribe": {
        "modes": ["sync"],
        "input_modalities": ["audio"],
        "output_modalities": ["text"]
      }
    }
  }
}
```

`capabilities: null` 恢复自动能力。覆盖独立存储，不会改写供应商发现结果。存在模型映射时，管理目录中的 `catalog_model_name` 指向编辑覆盖的真实目标。人工声明无法启用适配器尚未实现的协议。Token 模型白名单、账号分组、套餐模型限制及渠道能力在服务端共同校验。

`/api` 管理接口使用账号登录会话或账号访问令牌；下文 `/v1` 推理接口使用签发的网关模型 Token，两者不要混用。管理接口沿用 `{success,message,data}` 响应封装；推理接口返回对应协议响应和 HTTP 状态。

## 同步原生接口

以下命令仅作调用示例，先替换网关地址与占位 Token。媒体 URL 应为供应商能够访问的公有 HTTPS 地址。

```sh
export GATEWAY_BASE_URL='https://gateway.example'
export GATEWAY_TOKEN='YOUR_GATEWAY_TOKEN'

curl "$GATEWAY_BASE_URL/v1/rerank" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"gte-rerank-v2","query":"网关支持哪些音频能力","documents":["支持语音识别与语音合成","数据库迁移说明"],"top_n":1,"return_documents":true}'

curl "$GATEWAY_BASE_URL/v1/audio/transcriptions" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"qwen3-asr-flash","audio":"https://media.example/sample.wav","language":"zh","response_format":"json"}'

curl "$GATEWAY_BASE_URL/v1/images/edits" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"wan2.7-image","prompt":"将背景改为浅蓝色","images":["https://media.example/source.png"],"n":1,"response_format":"url"}'
```

图片编辑示例的模型名应替换为渠道配置的实际 Wan2.7 编辑模型或其公开别名。JSON ASR 的 `audio` 和图片编辑的 `images` 支持公有 URL 或媒体 data URI。也可 multipart 上传 ASR 的 `file` 或图片编辑的单个 `image`，单文件上限 10 MiB。Qwen ASR 最长 5 分钟；原生接口当前仅支持 JSON 转写结果。重排支持 1–500 篇文档，`top_n` 不超过文档数。图片编辑支持 1–9 张参考图、每次 1–4 张输出，不支持 mask 或 4K 编辑。

同步 typed 接口通过 `X-Inference-Id` 返回持久账务记录 ID。网关在调用供应商前预留额度，收到真实用量后结算；供应商是否执行或用量不确定时保留预留，避免自动重试产生重复费用。

只有本地校验尚未发送请求，或供应商明确拒绝执行时才自动退款。传输中断、408/5xx、成功响应读取失败、解析失败或超出结果上限均视为执行结果未知，保留预留并返回 `outcome_unknown`，不自动重发。JSON 图片结果上限为 64 MiB，超限会被明确检测，不以截断数据触发退款；图片多返回时按实际张数结算，额外费用无法落定时保留待对账。

## 视频任务与媒体资产

创建视频任务必须提供 `Idempotency-Key`，最长 128 字符。同一次业务意图固定使用同一个 key：

```sh
curl "$GATEWAY_BASE_URL/v1/media/jobs" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: video-order-example-001' \
  -d '{"model":"wan2.6-t2v","operation":"video.generate","prompt":"清晨海面上的帆船，镜头缓慢推进","size":"1280*720","duration":5}'
```

返回 HTTP 202 和任务本体。参数为扁平字段，支持 `negative_prompt`、`seed`；`duration` 为 2–15 秒，默认 5；`size` 为 `1280*720`、`720*1280`、`1920*1080` 或 `1080*1920`。同账号、相同 key、相同创建 Token 与归一化请求体返回同一个任务；请求体或 Token 不同返回 409。

| 操作 | 路径 |
| --- | --- |
| 查询任务 | `GET /v1/media/jobs/:id` |
| 请求取消 | `POST /v1/media/jobs/:id/cancel` |
| 下载本地资产 | `GET /v1/media/assets/:id` |

以上均需原创建账号及原创建 Token。即使同一账号的另一个 Token，也不能读取任务或资产。取消请求不等于取消成功：供应商只允许取消尚在排队的任务，网关继续查询最终状态。

任务执行状态、`asset_status` 与 `settlement_status` 分别表达上游任务、资产落地和账务结果。超时、提交租约过期或供应商返回 `UNKNOWN` 时进入 `unknown` / `pending_reconciliation`，不会自动再次生成或退款。先确认供应商侧执行结果，再处理原任务。

视频资产下载后存入本地目录，单文件上限 512 MiB。下载地址须通过公有网络地址校验，不能携带凭证。资产有效期为 7 天，下载始终需要 Token 鉴权；API 在过期后拒绝下载，后台 worker 负责清理过期文件。

管理员可携外部核查证据处理原任务：

```http
POST /api/inference/jobs/JOB_ID/reconcile
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{"action":"attach_task","provider_task_id":"VERIFIED_EXISTING_TASK_ID","note":"已在供应商控制台核对原请求及任务 ID"}
```

| action | 必要字段与适用条件 |
| --- | --- |
| `attach_task` | `provider_task_id`；只为 unknown 任务绑定已存在的供应商任务，不创建新任务 |
| `confirm_no_charge` | `confirmed_no_charge:true`；只用于已经外部确认未扣费的 unknown 任务，允许退款 |
| `record_usage` | `usage` 数组；只用于资产已经落地的任务，按已核实实际用量结算 |

所有操作要求非空 `note` 并记录管理员审计信息。用量示例：`[{"metric":"video.generated_duration","quantity":5000,"scale":1000,"unit":"second"}]` 表示 5 秒。不要用人工用量掩盖尚未核实的供应商执行状态。

## 价格与账务

当前每个模型共用一组 Input / Output / Cached / PerRequest 价格，尚未提供逐操作独立价格规则。同模型多操作单位不一致或自定义模型单位不明时，价格页面会提示，启用前必须核实。

价格表的 `InputPrice` / `OutputPrice` 是每百万“明确计量单位”的价格。重排以 token、ASR 以秒、视频以秒计量。例如 0.00022 元/秒应配置为每百万秒 220 元，不能直接填 0.00022。`per_request` 按单次调用计价，图片按实际输出张数计量。字符型 TTS 按每百万字符；Qwen3.1 Flash TTS 和 realtime 按 token。旧 OpenAI 兼容 ASR 保留按转写 token 计费的原有语义，不能套用 Qwen3-ASR 的秒单价。零价是有效配置。

价格、折扣及计量规则在请求或任务创建时保存快照。套餐预留同时保存规则、计费类型和窗口基准快照；已经接纳的任务即使在完成前套餐过期，仍按原套餐及接纳时所属窗口结算，新请求不能使用过期套餐。额度整数使用百万分之一元，按固定点规则结算；用量同时保存 `metric / quantity / scale / unit`。成功结算和退款通过持久 ledger 幂等执行。未知执行结果保留预留，管理员应先取得供应商侧证据再对账，避免退款后仍被供应商扣费。

## 普通同步请求查询与对账

文本、原生操作、音频、图片及多模态向量请求使用持久化账务预留。返回 `settlement_pending`、`usage_missing` 或执行结果不确定时，记录 `X-Inference-Id`，先查询原请求，不要自动重新调用供应商：

```http
GET /v1/inferences/INFERENCE_ID
Authorization: Bearer ORIGINAL_CREATING_TOKEN
```

`usage_pending`、`outcome_unknown`、`settlement_pending`、`submission_outcome_unknown` 等不确定执行或账务错误禁止服务端内部供应商及别名 fallback，并附带 `x-should-retry:false` 与 `X-Inference-Id`。标准 SDK 仍应根据错误 code 查询上述原请求状态，不要为同一意图创建新调用。普通同步路径目前不提供通用幂等重放；只有视频 jobs 强制要求并执行 `Idempotency-Key` 约束。

文本已成功生成而本地结算失败时，保留原子账务事务和预留，成功结果仍正常交付。非流式响应会附 `X-Inference-Status:settlement_pending`；流式响应只能保证在输出前发送的请求 ID 和禁止重试提示，不能事后修改已发送的响应头。精确费用与用量存入 SQL 待结算队列，worker 每次最多重试 4 条，失败后至少等待 30 秒，不再调用模型。结算和追加审计各设置 2 秒超时。数据库完全不可写时，额外用量与结果可能无法持久化；原预留不退款并记录服务端告警，需要人工核实供应商账单。

此接口严格要求原账号与原创建 Token，仅返回公开模型、账务状态、额度及时间。只有 `settled` 后才返回已经持久保存的结果；流式内容不保证保存，未保存结果不会自动重放。接口不返回内部渠道、价格快照、原始 prompt 或未结算结果。

文本流已经产生部分输出且供应商给出用量时按真实用量结算；OpenAI 兼容文本流缺少 usage 时沿用适配器的输入 token 与实际输出文本估算，不会仅因缺少 usage 把正常流判成失败。其他适配器已经输出而无法取得或估算用量时，保留预留等待对账。原生多模态向量响应读取失败保留预留；付费 token 计价的多模态向量缺 usage 时保留预留，记录 `multimodal_usage_missing`，返回 `usage_pending` / HTTP 503，不返回 embedding 成功结果。零价模型缺 usage 可结算为 0；费用确定的 `per_request` 模型可正常结算。多模态向量 total-only usage 按输入 token 计费。

管理员检查 `GET /api/inference/settlements/:id`，查看保存的用量、价格快照及审计，然后明确核实供应商侧账务：

```http
POST /api/inference/settlements/INFERENCE_ID/reconcile
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{"action":"settle","total_quota":40000,"usage":{"prompt_tokens":100,"completion_tokens":50},"note":"按供应商请求记录核实实际用量与总费用"}
```

`total_quota` 是实际总费用，单位为百万分之一元。可选 `usage` 为经核实的供应商 usage 对象或原生操作计量数组；省略时使用已保存的观测用量。token 套餐缺少可用的真实 token 用量时必须补齐，避免只结算费用却遗漏套餐窗口。确认供应商没有消费时使用 `action:refund`、`total_quota:0`、`confirmed_no_charge:true` 和证据说明。

只允许普通请求账务前缀 `text:`、`request:`、`multimodal:`、`audio:`、`image:`；视频任务、实时会话及轮次记录必须走专用对账接口。记录须已标为待处理，或至少 5 分钟未更新以处理进程故障后的遗留预留。对账先以管理员身份及完整请求哈希原子领取，再调用现有幂等结算；相同管理员、相同内容可重试，冲突请求被拒绝。`claimed` 审计状态表示操作尚未完成，应原样重试，不能换金额再次领取。

## Qwen Omni 双向实时会话

入口为带网关 Token 的 WebSocket 握手：

```http
GET /v1/realtime?model=qwen3.5-omni-flash-realtime&max_turns=4&max_output_tokens=1024&max_seconds=120
Authorization: Bearer YOUR_GATEWAY_TOKEN
Upgrade: websocket
```

支持 `qwen3.5-omni-flash-realtime`、`qwen3.5-omni-plus-realtime` 及对应日期版本。模型须已在渠道和账号权限范围中启用。模型在握手时固定，不能通过 `session.update` 切换。渠道根地址、地域、上游 Key 均由服务端渠道配置确定。

返回握手头 `X-OneAPI-Realtime-Protocol: qwen-omni-ws-manual-v1`、`X-OneAPI-Realtime-Session` 和 `X-OneAPI-Realtime-Budget-Quota`。这是明确的 Qwen 手动 WebSocket 协议子集，不能直接假定兼容所有 OpenAI realtime 客户端。浏览器原生 WebSocket 无法设置 Authorization 头；本轮未实现浏览器临时凭证交换，也不接受 URL 查询参数携带长效 Token。请使用能够设置握手头的服务端或原生客户端。

网关先向上游设置 `turn_detection:null`、Tina 音色、text/audio 输出及输出 token 限制。客户端按以下顺序手动提交音频轮次：

```json
{"type":"session.update","session":{"turn_detection":null,"voice":"Tina","max_tokens":1024}}
{"type":"input_audio_buffer.append","audio":"BASE64_AUDIO_CHUNK"}
{"type":"input_audio_buffer.commit"}
{"type":"response.create"}
```

`BASE64_AUDIO_CHUNK` 必须替换为合法音频编码，并与 session 的输入音频格式一致。可重复 append 后 commit。等待该轮 `response.done` 后再发起下一轮。

允许的客户端事件包括 `session.update`、`input_audio_buffer.append/commit/clear`、`input_image_buffer.append`、`response.create/cancel`，以及 `conversation.item.create` 的 `function_call_output`。函数定义仅支持客户端执行的 function 工具，最多 16 个；工具调用事件及返回值保持 Qwen 原帧语义。网关不执行这些工具。额外付费转写、搜索、服务端 MCP 工具、自动 VAD 均不启用；`response.create` 不能包含覆盖 session 的 `response` 对象。

可用音色为 Tina、Ethan；输出模态为 text/audio。音频格式允许 pcm、pcm16、wav；结构化 audio 配置允许 PCM/WAV 和 8/16/24/48 kHz。图片 append 的编码内容上限为 256 KiB。网关校验受支持设置，未知 session 设置会被拒绝。

| 资源 | 默认值与上限 |
| --- | --- |
| `max_turns` | 默认 4，范围 1–16 |
| `max_output_tokens` | 默认 1024，范围 1–4096；后续 session 设置不能突破此值 |
| `max_seconds` | 默认 120，范围 1–120 秒 |
| WebSocket 单帧 | 512 KiB |
| 累计客户端输入 / 上游输出 | 8 MiB / 64 MiB |
| 上游握手 / 写入超时 | 各 10 秒 |
| 并发 | 复用渠道现有并发限制，断开时释放 |

会话在连接上游前预留完整预算。每轮保守预算按 262144 输入 token 和 65536 输出 token、当前价格及折扣计算，再乘 `max_turns`；这包含可能反复计费的历史上下文。`max_output_tokens` 是输出截断限制，不用于缩小保守预留。额度不足会在连接前被拒绝；零价模型预算为零。每次 `response.create` 还会先原子预留一笔套餐请求窗口，不能并行生成多个响应。

`response.done` 按真实 `response.id` 只计费一次，并保存输入、输出 token 及完整模态明细。各轮用量是该轮总量，网关逐轮求和，不按累计值做差。相同 ID 的冲突用量、缺失用量、意外自动生成、超界或生成中断会结束会话并保留预留等待对账。确认完成的正常会话只结算实际费用并释放剩余预算。

已知完成轮次会即时结算其套餐窗口；关闭会话时也会幂等补结算已经记录、尚未完成账务的已知轮次，即使后续轮次中断。零额度轮次仍代表一次实际请求；未知轮次不能因断线而自动释放请求次数，否则可通过反复断线绕过套餐限制。该轮在核实供应商执行结果后通过会话对账终结。

账号可凭模型 Token 查询自己的会话：

```http
GET /v1/realtime/sessions/SESSION_ID
Authorization: Bearer YOUR_GATEWAY_TOKEN
```

管理员查询 `GET /api/realtime/session/:id`，可查看会话状态、价格快照、逐轮用量、ledger 和对账审计，不包含供应商 Key 或原始音视频内容。异常断开或进程故障后的会话进入 `pending_reconciliation`；worker 将超过 5 分钟仍 active 的记录标记为待对账，不自动退款。

```http
POST /api/realtime/session/SESSION_ID/reconcile
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{"action":"settle","total_quota":40000,"note":"已按供应商账单核实该会话总费用为 0.04 元"}
```

`settle` 的 `total_quota` 是管理员核实后的会话总费用，不是差额。`refund` 只适用于确认整场未发生消费、且无已确认完成轮次的会话，`total_quota` 应为 0。处理会先记录管理员、动作、金额和证据说明，再幂等完成子请求及会话 ledger；重试必须保持动作、金额和说明完全一致。`reconciling` 表示已领取的对账尚待完成，可原样重试。

## 部署与巡检

| 配置 | 默认值与用途 |
| --- | --- |
| `INFERENCE_WORKER_ENABLED` | 默认启用；设 `false` 关闭任务轮询、资产清理和过期会话标记 |
| `INFERENCE_ASSET_DIR` | 默认数据目录下 `media-assets`；保存本地视频资产 |
| `INFERENCE_JOB_TIMEOUT_SECONDS` | 默认 86400，允许 60–604800；到期任务转待对账 |
| `RELAY_PROXY` | 复用网关代理配置，实时上游握手使用该代理 |

worker 每 2 秒领取一批最多 4 个任务，租约 120 秒。上游查询从 5 秒开始；连续暂态失败和持续未推进的排队/运行状态分别按 5、10、20、40、60 秒退避，状态推进时重置查询退避。不可恢复的协议、鉴权或账务错误转待对账并停止自动领取，不退款或重新生成。多实例依靠 SQL 租约协作。API 与 worker 所有实例必须挂载同一个资产目录共享卷，独立容器磁盘会造成资产不可读。本轮仅提供本地文件存储。

管理员模型探测接口为 `POST /api/channel/test_operation/:id`，请求 `{model,operation,confirm_cost:true,input?}`。目录通过独立的 `probe_operations` 声明可探测操作及 `requires_input`，与运行时 `gateway_support` 分开判断。`chat.generate`、`embedding.create`、`rerank.score` 可自动输入探测及批量探测；`audio.transcribe` 必须填写公网音频 URL，`image.edit` 必须填写公网图片 URL 和 prompt，确认费用后才发送。ASR 输入示例为 `input:{audio:"https://media.example/a.wav",response_format:"json"}`，编辑为 `input:{images:["https://media.example/a.png"],prompt:"背景改为浅蓝色",n:1}`。缺少或无效媒体输入时后端不调用供应商。TTS、文生图、视频、realtime 和未声明探测的操作禁用管理探测并引导“使用调用示例验证”；没有探测适配不代表模型不健康。生产启用前核对渠道地域、模型授权、价格单位、共享存储及待对账队列。测试与构建使用假上游，不会验证真实供应商 Key、地域开通状态或生产价格。

探测使用本次渠道和模型的完整适配器配置；实际发送的探测记录管理员、操作、渠道、成功状态及可用的用量审计，不记录媒体 URL 或密钥。管理员探测费用由供应商渠道承担，不走普通用户账本。

本轮实时回归命令：

```sh
go test -race ./relay/session ./model ./controller -run 'TestValidateClientEvent_|TestParseCompletion_|TestInferenceSession_|TestRealtime_' -count=1
```

完整项目验证仍使用 `go build ./...`、`go test ./...` 以及 `web/default-pro` 内 `npm run build`。

## 供应商协议依据

- [百炼文本重排](https://help.aliyun.com/zh/model-studio/text-rerank-api)
- [Qwen ASR 原生 API](https://help.aliyun.com/en/model-studio/qwen-asr-api-reference)
- [Wan 图片生成与编辑](https://help.aliyun.com/en/model-studio/wan-image-generation-and-editing-api-reference)
- [Wan 文生视频与异步任务](https://help.aliyun.com/en/model-studio/legacy-wan-text-to-video-api-reference)、[任务管理](https://help.aliyun.com/en/model-studio/manage-asynchronous-tasks)
- [Qwen 实时协议](https://help.aliyun.com/en/model-studio/realtime)、[客户端事件](https://help.aliyun.com/en/model-studio/client-events)、[服务端事件与用量](https://help.aliyun.com/en/model-studio/server-events)
