---
title: 图像、语音与重排接口
description: "文生图、图片编辑、语音合成、语音识别与重排的调用方式与限制"
category: media
order: 2
---

# 图像、语音与重排接口

同步原生接口的调用示例见下。请先替换网关地址与占位 Token；媒体 URL 应为供应商能够访问的**公有 HTTPS 地址**。

```sh
export GATEWAY_BASE_URL='https://gateway.example'
export GATEWAY_TOKEN='YOUR_GATEWAY_TOKEN'
```

## 重排

```sh
curl "$GATEWAY_BASE_URL/v1/rerank" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"gte-rerank-v2","query":"网关支持哪些音频能力","documents":["支持语音识别与语音合成","数据库迁移说明"],"top_n":1,"return_documents":true}'
```

- 首个原生适配为百炼 `gte-rerank-v2`
- 支持 1–500 篇文档，`top_n` 不得超过文档数
- 按 token 计量

## 语音识别

```sh
curl "$GATEWAY_BASE_URL/v1/audio/transcriptions" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"qwen3-asr-flash","audio":"https://media.example/sample.wav","language":"zh","response_format":"json"}'
```

- 两套协议并存：OpenAI multipart（`file` 上传）与百炼 `qwen3-asr-flash` 原生 JSON（`audio` 传公有 URL 或 data URI）
- Qwen ASR 单个音频最长 5 分钟
- 原生接口当前仅支持 JSON 转写结果
- 上传文件单件上限 10 MiB
- 计量单位按**秒**；旧 OpenAI 兼容 ASR 保留按转写 token 计费的原有语义，不能套用 Qwen3-ASR 的秒单价

## 图片编辑

```sh
curl "$GATEWAY_BASE_URL/v1/images/edits" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"wan2.7-image","prompt":"将背景改为浅蓝色","images":["https://media.example/source.png"],"n":1,"response_format":"url"}'
```

- 首个原生适配为百炼 Wan2.7 图像编辑
- 支持 1–9 张参考图，每次输出 1–4 张
- 支持单张 `image` 的 multipart 上传（单文件上限 10 MiB）
- **不支持** mask 与 4K 编辑
- 模型名请替换为渠道配置的实际 Wan2.7 编辑模型或其公开别名

## 文生图、语音合成与向量

这三类沿用已有适配器，入口与 OpenAI 兼容协议一致：

| 能力 | 入口 | 备注 |
| --- | --- | --- |
| 文生图 | `POST /v1/images/generations` | 首个原生模型 `wan2.6-t2i`；按实际输出张数计量 |
| 语音合成 | `POST /v1/audio/speech` | 首个原生模型 `seed-tts-2.0`；字符型按每百万字符，token 计价型按 token |
| 向量 | `POST /v1/embeddings` | 含 DashScope 原生多模态向量；total-only usage 按输入 token 计费 |

请求体遵循对应协议与模型声明能力。渠道模型勾选、账号分组、套餐限制与 Token 白名单在服务端共同校验。

## 账务提示

同步 typed 接口通过响应头 `X-Inference-Id` 返回持久账务记录 ID。网关在调用供应商前预留额度，收到真实用量后结算；供应商是否执行或用量不确定时保留预留，避免自动重试产生重复费用。

只有本地校验尚未发送请求，或供应商明确拒绝执行时才自动退款。传输中断、408/5xx、成功响应读取失败、解析失败或超出结果上限均视为**执行结果未知**，保留预留并返回 `outcome_unknown`，不自动重发。

- JSON 图片结果上限 64 MiB，超限会被明确检测，不以截断数据触发退款
- 图片多返回时按实际张数结算，额外费用无法落定时保留待对账
- 多模态向量在付费 token 计价下缺 usage 时保留预留，返回 `usage_pending` / HTTP 503

结算与对账的完整流程见[账务结算与巡检](./settlement-probe)。

## 相关文档

- [多模态网关总览](./overview)
- [视频任务与实时语音](./video-realtime)
- [OpenAI 兼容接口](../api/openai-compatible)
