---
title: 视频任务与实时语音
description: "异步视频生成任务的生命周期、媒体资产与 Qwen Omni 实时会话"
category: media
order: 3
---

# 视频任务与实时语音

## 视频任务

创建视频任务必须提供 `Idempotency-Key`，最长 128 字符。同一次业务意图固定使用同一个 key：

```sh
curl "$GATEWAY_BASE_URL/v1/media/jobs" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: video-order-example-001' \
  -d '{"model":"wan2.6-t2v","operation":"video.generate","prompt":"清晨海面上的帆船，镜头缓慢推进","size":"1280*720","duration":5}'
```

返回 **HTTP 202** 和任务本体。同账号、相同 key、相同创建 Token 与归一化请求体返回同一个任务；请求体或 Token 不同返回 409。

| 参数 | 取值 |
| --- | --- |
| `prompt` / `negative_prompt` | 必填 / 可选 |
| `duration` | 2–15 秒，默认 5 |
| `size` | `1280*720`、`720*1280`、`1920*1080`、`1080*1920` |
| `seed` | 可选 |

### 任务与资产管理

| 操作 | 路径 |
| --- | --- |
| 查询任务 | `GET /v1/media/jobs/:id` |
| 请求取消 | `POST /v1/media/jobs/:id/cancel` |
| 下载本地资产 | `GET /v1/media/assets/:id` |

以上均需**原创建账号及原创建 Token**。即使同一账号的另一个 Token，也不能读取任务或资产。取消请求不等于取消成功：供应商只允许取消尚在排队的任务，网关继续查询最终状态。

- 任务执行状态、`asset_status` 与 `settlement_status` 分别表达上游任务、资产落地和账务结果
- 超时、提交租约过期或供应商返回 `UNKNOWN` 时进入 `unknown` / `pending_reconciliation`，不会自动再次生成或退款
- 视频资产下载后存入本地目录，单文件上限 512 MiB
- 下载地址须通过公有网络地址校验，不能携带凭证
- 资产有效期 **7 天**，下载始终需要 Token 鉴权；过期后 API 拒绝下载，后台 worker 负责清理

## Qwen Omni 实时会话

入口为带网关 Token 的 WebSocket 握手：

```http
GET /v1/realtime?model=qwen3.5-omni-flash-realtime&max_turns=4&max_output_tokens=1024&max_seconds=120
Authorization: Bearer YOUR_GATEWAY_TOKEN
Upgrade: websocket
```

支持 `qwen3.5-omni-flash-realtime`、`qwen3.5-omni-plus-realtime` 及对应日期版本。模型须已在渠道和账号权限范围中启用，模型在握手时固定，不能通过 `session.update` 切换。

返回握手头 `X-OneAPI-Realtime-Protocol: qwen-omni-ws-manual-v1`、`X-OneAPI-Realtime-Session` 和 `X-OneAPI-Realtime-Budget-Quota`。这是明确的 **Qwen 手动 WebSocket 协议子集**，不能直接假定兼容所有 OpenAI realtime 客户端。浏览器原生 WebSocket 无法设置 Authorization 头，本轮未实现浏览器临时凭证交换，也不接受 URL 查询参数携带长效 Token；请使用能设置握手头的服务端或原生客户端。

### 手动轮次

网关先向上游设置 `turn_detection:null`、Tina 音色、text/audio 输出及输出 token 限制。客户端按以下顺序提交音频轮次：

```json
{"type":"session.update","session":{"turn_detection":null,"voice":"Tina","max_tokens":1024}}
{"type":"input_audio_buffer.append","audio":"BASE64_AUDIO_CHUNK"}
{"type":"input_audio_buffer.commit"}
{"type":"response.create"}
```

`BASE64_AUDIO_CHUNK` 必须替换为合法音频编码，并与 session 的输入音频格式一致。可重复 append 后 commit；等待该轮 `response.done` 后再发起下一轮。

允许的客户端事件：`session.update`、`input_audio_buffer.append/commit/clear`、`input_image_buffer.append`、`response.create/cancel`，以及 `conversation.item.create` 的 `function_call_output`。函数定义仅支持客户端执行的 function 工具，最多 16 个；网关不执行这些工具。额外付费转写、搜索、服务端 MCP 工具、自动 VAD 均不启用。

可用音色为 Tina、Ethan；输出模态为 text/audio；音频格式允许 pcm、pcm16、wav。图片 append 的编码内容上限为 256 KiB。未知 session 设置会被拒绝。

| 资源 | 默认值与上限 |
| --- | --- |
| `max_turns` | 默认 4，范围 1–16 |
| `max_output_tokens` | 默认 1024，范围 1–4096；后续 session 设置不能突破 |
| `max_seconds` | 默认 120，范围 1–120 秒 |
| WebSocket 单帧 | 512 KiB |
| 累计客户端输入 / 上游输出 | 8 MiB / 64 MiB |
| 上游握手 / 写入超时 | 各 10 秒 |
| 并发 | 复用渠道现有并发限制，断开时释放 |

### 预算与计费

会话在连接上游前预留完整预算：每轮保守预算按 262144 输入 token 和 65536 输出 token、当前价格及折扣计算，再乘 `max_turns`。`max_output_tokens` 只用于截断输出，不用于缩小保守预留。额度不足会在连接前被拒绝。

`response.done` 按真实 `response.id` 只计费一次，并保存输入、输出 token 及完整模态明细。概述性规则：

- 各轮用量是该轮总量，网关逐轮求和，不按累计值做差
- 相同 ID 的冲突用量、缺失用量、意外自动生成、超界或生成中断会结束会话并保留预留等待对账
- 已确认完成的轮次即时结算套餐窗口；关闭会话时幂等补结算已记录未完成的轮次
- 零额度轮次仍代表一次实际请求，未知轮次不能因断线而自动释放请求次数

### 会话查询

```http
GET /v1/realtime/sessions/SESSION_ID
Authorization: Bearer YOUR_GATEWAY_TOKEN
```

管理员查询 `GET /api/realtime/session/:id` 可查看会话状态、价格快照、逐轮用量、ledger 与对账审计，不包含供应商 Key 或原始音视频内容。异常断开或进程故障后的会话进入 `pending_reconciliation`；worker 将超过 5 分钟仍 active 的记录标记为待对账，不自动退款。

## 相关文档

- [多模态网关总览](./overview)
- [图像、语音与重排接口](./image-audio)
- [账务结算与巡检](./settlement-probe)
