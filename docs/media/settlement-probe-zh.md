---
title: 账务结算与巡检
description: "价格单位换算、推理预留与结算、管理员对账、worker 配置与模型探测"
category: media
order: 4
---

# 账务结算与巡检

## 价格与计量单位

当前每个模型共用一组 Input / Output / Cached / PerRequest 价格，尚未提供逐操作独立价格规则。同模型多操作单位不一致或自定义模型单位不明时，价格页面会提示，启用前必须核实。

价格表的 `InputPrice` / `OutputPrice` 是每**百万**“明确计量单位”的价格。重排以 token、ASR 以秒、视频以秒计量。例如 0.00022 元/秒应配置为每百万秒 220 元，**不能直接填 0.00022**。

| 计费类型 | 计量口径 |
| --- | --- |
| `per_request` | 按单次调用计价；图片按实际输出张数 |
| 字符型 TTS | 每百万字符 |
| token 型 TTS / realtime | 按 token |
| 重排 | 按 token |
| ASR | 按秒；旧 OpenAI 兼容 ASR 仍按转写 token |
| 视频 | 按秒 |

零价是有效配置。价格、折扣及计量规则在请求或任务创建时保存快照；套餐预留同时保存规则、计费类型和窗口基准快照。已经接纳的任务即使在完成前套餐过期，仍按原套餐及接纳时所属窗口结算，新请求不能使用过期套餐。额度整数使用百万分之一元，按固定点规则结算；用量同时保存 `metric / quantity / scale / unit`。成功结算和退款通过持久 ledger 幂等执行。

## 同步请求的查询与对账

文本、原生操作、音频、图片及多模态向量请求使用持久化账务预留。返回 `settlement_pending`、`usage_missing` 或执行结果不确定时，记录 `X-Inference-Id`，**先查询原请求，不要自动重新调用供应商**：

```http
GET /v1/inferences/INFERENCE_ID
Authorization: Bearer ORIGINAL_CREATING_TOKEN
```

`usage_pending`、`outcome_unknown`、`settlement_pending`、`submission_outcome_unknown` 等不确定执行或账务错误**禁止**服务端内部供应商及别名 fallback，并附带 `x-should-retry:false` 与 `X-Inference-Id`。标准 SDK 仍应根据错误 code 查询上述原请求状态，不要为同一意图创建新调用。普通同步路径目前不提供通用幂等重放；只有视频 jobs 强制要求并执行 `Idempotency-Key` 约束。

文本已成功生成而本地结算失败时，保留原子账务事务和预留，成功结果仍正常交付。非流式响应会附 `X-Inference-Status:settlement_pending`；流式响应只能保证在输出前发送的请求 ID 和禁止重试提示，不能事后修改已发送的响应头。精确费用与用量存入 SQL 待结算队列，worker 每次最多重试 4 条，失败后至少等待 30 秒，不再调用模型。结算和追加审计各设置 2 秒超时。数据库完全不可写时，额外用量与结果可能无法持久化；原预留不退款并记录服务端告警，需要人工核实供应商账单。

该接口严格要求原账号与原创建 Token，仅返回公开模型、账务状态、额度及时间。只有 `settled` 后才返回已经持久保存的结果；流式内容不保证保存，未保存结果不会自动重放。接口不返回内部渠道、价格快照、原始 prompt 或未结算结果。

## 管理员对账

### 普通请求

管理员先检查 `GET /api/inference/settlements/:id`，查看保存的用量、价格快照及审计，然后明确核实供应商侧账务：

```http
POST /api/inference/settlements/INFERENCE_ID/reconcile
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{"action":"settle","total_quota":40000,"usage":{"prompt_tokens":100,"completion_tokens":50},"note":"按供应商请求记录核实实际用量与总费用"}
```

- `total_quota` 是实际总费用，单位为百万分之一元
- 可选 `usage` 为经核实的供应商 usage 对象或原生操作计量数组；省略时使用已保存的观测用量
- token 套餐缺少可用的真实 token 用量时必须补齐，避免只结算费用却遗漏套餐窗口
- 确认供应商没有消费时使用 `action:refund`、`total_quota:0`、`confirmed_no_charge:true` 和证据说明
- 只允许普通请求账务前缀 `text:`、`request:`、`multimodal:`、`audio:`、`image:`；视频任务、实时会话及轮次记录必须走专用对账接口

记录须已标为待处理，或至少 5 分钟未更新以处理进程故障后的遗留预留。对账先以管理员身份及完整请求哈希原子领取，再调用现有幂等结算；相同管理员、相同内容可重试，冲突请求被拒绝。`claimed` 审计状态表示操作尚未完成，应原样重试，不能换金额再次领取。

### 视频任务

```http
POST /api/inference/jobs/JOB_ID/reconcile
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{"action":"attach_task","provider_task_id":"VERIFIED_EXISTING_TASK_ID","note":"已在供应商控制台核对原请求及任务 ID"}
```

| action | 必要字段与适用条件 |
| --- | --- |
| `attach_task` | `provider_task_id`；只为 unknown 任务绑定已存在的供应商任务，不创建新任务 |
| `confirm_no_charge` | `confirmed_no_charge:true`；只用于外部确认未扣费的 unknown 任务，允许退款 |
| `record_usage` | `usage` 数组；只用于资产已经落地的任务，按已核实实际用量结算 |

所有操作要求非空 `note` 并记录管理员审计信息。用量示例：`[{"metric":"video.generated_duration","quantity":5000,"scale":1000,"unit":"second"}]` 表示 5 秒。不要用人工用量掩盖尚未核实的供应商执行状态。

### 实时会话

```http
POST /api/realtime/session/SESSION_ID/reconcile
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{"action":"settle","total_quota":40000,"note":"已按供应商账单核实该会话总费用为 0.04 元"}
```

`settle` 的 `total_quota` 是管理员核实后的会话**总费用**，不是差额。`refund` 只适用于确认整场未发生消费、且无已确认完成轮次的会话，`total_quota` 应为 0。处理会先记录管理员、动作、金额和证据说明，再幂等完成子请求及会话 ledger；重试必须保持动作、金额和说明完全一致。`reconciling` 表示已领取的对账尚待完成，可原样重试。

## 部署配置

| 配置 | 默认值与用途 |
| --- | --- |
| `INFERENCE_WORKER_ENABLED` | 默认启用；设 `false` 关闭任务轮询、资产清理和过期会话标记 |
| `INFERENCE_ASSET_DIR` | 默认数据目录下 `media-assets`；保存本地视频资产 |
| `INFERENCE_JOB_TIMEOUT_SECONDS` | 默认 86400，允许 60–604800；到期任务转待对账 |
| `RELAY_PROXY` | 复用网关代理配置，实时上游握手使用该代理 |

worker 每 2 秒领取一批最多 4 个任务，租约 120 秒。上游查询从 5 秒开始；连续暂态失败和持续未推进的排队/运行状态分别按 5、10、20、40、60 秒退避，状态推进时重置查询退避。不可恢复的协议、鉴权或账务错误转待对账并停止自动领取，不退款或重新生成。多实例依靠 SQL 租约协作。

**API 与 worker 所有实例必须挂载同一个资产目录共享卷**，独立容器磁盘会造成资产不可读。本轮仅提供本地文件存储。

## 模型探测

管理员模型探测接口为 `POST /api/channel/test_operation/:id`，请求 `{model, operation, confirm_cost: true, input?}`。目录通过独立的 `probe_operations` 声明可探测操作及 `requires_input`，与运行时 `gateway_support` 分开判断。

| 操作 | 探测要求 |
| --- | --- |
| `chat.generate`、`embedding.create`、`rerank.score` | 可自动输入探测及批量探测 |
| `audio.transcribe` | 必须填写公网音频 URL |
| `image.edit` | 必须填写公网图片 URL 和 prompt |
| TTS、文生图、视频、realtime | 禁用管理探测，引导使用调用示例验证 |

确认费用后才发送。ASR 输入示例：`input:{audio:"https://media.example/a.wav",response_format:"json"}`；编辑示例：`input:{images:["https://media.example/a.png"],prompt:"背景改为浅蓝色",n:1}`。缺少或无效媒体输入时后端不调用供应商。没有探测适配不代表模型不健康。

探测使用本次渠道和模型的完整适配器配置；实际发送的探测记录管理员、操作、渠道、成功状态及可用的用量审计，不记录媒体 URL 或密钥。管理员探测费用由供应商渠道承担，不走普通用户账本。

控制台内的图像/音频媒体探针调试台可直接发起上述探测，便于在上线前确认模型与渠道组合是否可用。

## 回归命令

```sh
go test -race ./relay/session ./model ./controller -run 'TestValidateClientEvent_|TestParseCompletion_|TestInferenceSession_|TestRealtime_' -count=1
```

完整项目验证仍使用 `go build ./...`、`go test ./...` 以及 `web/default-pro` 内 `npm run build`。

## 相关文档

- [多模态网关总览](./overview)
- [图像、语音与重排接口](./image-audio)
- [视频任务与实时语音](./video-realtime)
- [渠道连通性测试](../channel/channel-test)
