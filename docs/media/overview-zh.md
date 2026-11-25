---
title: 多模态网关总览
description: "文本、向量、图像、语音、重排、视频与实时语音的统一入口与能力目录"
category: media
order: 1
---

# 多模态网关总览

> 本页说明当前版本的原生多模态网关能力与限制。
>
> Native multimodal gateway capabilities and limits in the current release.

网关按“操作 + 执行模式”选择渠道，模型分组用于发现和授权。模型目录中出现了某项能力，并不代表所有供应商协议都已实现；请求最终使用的是**目录声明的能力**与**渠道适配器实际能力**的交集。

## 支持范围

| 形态 | 操作 | 执行模式与入口 | 说明 |
| --- | --- | --- | --- |
| 文本、视觉、音频理解对话 | `chat.generate` | sync / stream，`POST /v1/chat/completions` | 沿用现有供应商适配器；具体输入依模型声明能力 |
| 向量 | `embedding.create` | sync，`POST /v1/embeddings` | 包括多模态向量适配 |
| 文生图 | `image.generate` | sync，`POST /v1/images/generations` | 沿用已有图像渠道 |
| 语音合成 | `audio.speech` | sync，`POST /v1/audio/speech` | 沿用已有 TTS 适配 |
| 语音识别 | `audio.transcribe` | sync，`POST /v1/audio/transcriptions` | OpenAI multipart 协议 + 百炼 `qwen3-asr-flash` 原生协议 |
| 重排 | `rerank.score` | sync，`POST /v1/rerank` | 首个原生适配为百炼 `gte-rerank-v2` |
| 图片编辑 | `image.edit` | sync，`POST /v1/images/edits` | 首个原生适配为百炼 Wan2.7；支持参考图消息，不支持 mask |
| 视频生成 | `video.generate` | async，`POST /v1/media/jobs` | 首个模型为百炼 `wan2.6-t2v` |
| 双向实时语音 | `chat.generate` | realtime，`GET /v1/realtime` WebSocket | 首个协议为 Qwen Omni 手动轮次子集 |

**不在本轮范围**：图片变体、视频编辑、Fun-ASR 长录音异步任务、跨供应商统一实时协议、WebRTC、对象存储后端。已有音频翻译、内容审核接口继续受适配器协议与模型声明约束。

## 已原生适配的模型

| 模型 | 操作 | 计量单位 |
| --- | --- | --- |
| `wan2.6-t2i` | 文生图 | 按实际输出张数（per_request） |
| `wan2.7`（图像编辑） | 图片编辑 | 按实际输出张数 |
| `wan2.6-t2v` | 文生视频（异步） | 按生成秒数 |
| `seed-tts-2.0` | 语音合成 | 字符型 TTS 按每百万字符；token 计价型按 token |
| `qwen3-asr-flash` | 语音识别 | 按秒 |
| `gte-rerank-v2` | 重排 | 按 token |
| `qwen3.5-omni-flash-realtime` / `qwen3.5-omni-plus-realtime` | 实时语音 | 按 token |

百炼渠道配置 API 根地址，例如北京 `https://dashscope.aliyuncs.com` 或新加坡 `https://dashscope-intl.aliyuncs.com`，使用对应地域的 API Key。渠道中的模型名与映射目标必须是该地域实际可用的模型。**不要把 `/compatible-mode/v1` 作为原生能力渠道根地址。** 渠道 Key 仅由服务端发送给供应商。

旧百炼原生 chat 转换只支持文本消息，网关会拒绝 image/audio/video 输入以避免静默丢失；视觉等多模态 chat 请使用已支持对应协议的 compatible 渠道。

## 能力目录与人工覆盖

模型页、Token、套餐、模型别名、价格设置共用同一份分类数据；渠道模型勾选也按相同分类展示。能力来源依次为：人工覆盖 → 供应商目录 → 内置声明 → 名称推断 → 别名；无法识别的模型归入“未分类”。名称推断用于发现，人工覆盖用于修正实际能力。

账号可见的模型选项按权限过滤，不向普通账号返回渠道、供应商或映射后的内部模型 ID：

```http
GET /api/model/options
Authorization: Bearer ACCOUNT_ACCESS_TOKEN
```

管理员可用 `?scope=all` 获取完整管理范围。别名只展示全部可见目标共同支持的操作与模式，避免某个目标无法执行时仍被路由过去。

管理员修正能力声明：

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

## 日期后缀与家族能力

已实现的百炼原生家族支持合法 `-YYYY-MM-DD` / `-YYYYMMDD` 日期后缀：调度使用家族能力，发给上游的模型 ID 保留原值。这不保证供应商已开通某个日期版本；任意自定义后缀不会自动取得原生协议能力。

能力与渠道目录使用最多 30 秒的本地缓存，本进程修改目录或渠道配置时立即失效；其他实例的配置变更依赖 TTL 收敛。账号权限仍在每次请求中校验，不缓存跨账号授权结果。

## 鉴权区分

`/api` 管理接口使用账号登录会话或账号访问令牌；`/v1` 推理接口使用签发的网关模型 Token，两者不要混用。管理接口沿用 `{success,message,data}` 响应封装；推理接口返回对应协议响应和 HTTP 状态。

## 相关文档

- [图像与语音接口](./image-audio)
- [视频任务与实时语音](./video-realtime)
- [账务结算与巡检](./settlement-probe)
- [模型别名与模型池](../user/model-alias)
