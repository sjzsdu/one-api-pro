---
title: Multimodal Gateway Overview
description: "One entry point for text, embeddings, images, audio, rerank, video and realtime voice"
category: media
order: 1
---

# Multimodal Gateway Overview

> Native multimodal gateway capabilities and limits in the current release.

The gateway selects a channel by **operation + execution mode**, while model groups govern discovery and authorization. A capability shown in the model catalog does **not** mean every provider protocol is implemented: a request runs on the intersection of the **capability declared by the catalog** and the **capability actually supported by the channel adapter**.

## Supported operations

| Form | Operation | Mode & endpoint | Notes |
| --- | --- | --- | --- |
| Text / vision / audio understanding | `chat.generate` | sync / stream, `POST /v1/chat/completions` | Existing provider adapters; inputs depend on declared capability |
| Embeddings | `embedding.create` | sync, `POST /v1/embeddings` | Includes multimodal embedding adapters |
| Image generation | `image.generate` | sync, `POST /v1/images/generations` | Existing image channels |
| Text to speech | `audio.speech` | sync, `POST /v1/audio/speech` | Existing TTS adapters |
| Speech recognition | `audio.transcribe` | sync, `POST /v1/audio/transcriptions` | OpenAI multipart + DashScope `qwen3-asr-flash` native protocol |
| Rerank | `rerank.score` | sync, `POST /v1/rerank` | First native adapter: DashScope `gte-rerank-v2` |
| Image editing | `image.edit` | sync, `POST /v1/images/edits` | First native adapter: DashScope Wan2.7; reference images, no mask |
| Video generation | `video.generate` | async, `POST /v1/media/jobs` | First model: DashScope `wan2.6-t2v` |
| Realtime voice | `chat.generate` | realtime, `GET /v1/realtime` WebSocket | First protocol: Qwen Omni manual-turn subset |

**Out of scope for this release**: image variants, video editing, Fun-ASR long-recording async jobs, a cross-provider unified realtime protocol, WebRTC, object-storage backends. Existing audio translation and moderation endpoints remain constrained by adapter protocol and model declarations.

## Natively adapted models

| Model | Operation | Metering |
| --- | --- | --- |
| `wan2.6-t2i` | Text to image | Actual output images (`per_request`) |
| `wan2.7` (image editing) | Image edit | Actual output images |
| `wan2.6-t2v` | Text to video (async) | Generated seconds |
| `seed-tts-2.0` | Text to speech | Character-billed TTS per million characters; token-billed per token |
| `qwen3-asr-flash` | Speech recognition | Seconds |
| `gte-rerank-v2` | Rerank | Tokens |
| `qwen3.5-omni-flash-realtime` / `qwen3.5-omni-plus-realtime` | Realtime voice | Tokens |

For DashScope channels, set the API root for your region — Beijing `https://dashscope.aliyuncs.com` or Singapore `https://dashscope-intl.aliyuncs.com` — with the matching regional API key. Model names and mapping targets in the channel must be available in that region. **Do not use `/compatible-mode/v1` as the root of a native-capability channel.** Channel keys are only sent from the server to the provider.

The legacy DashScope native chat conversion only supports text messages; the gateway rejects image/audio/video inputs rather than silently dropping them. Use a compatible channel with the corresponding protocol for multimodal chat.

## Capability catalog and manual overrides

The model page, tokens, plans, model aliases and pricing settings share one catalog projection; channel model selection uses the same categories. Capability sources are, in order: manual override → provider catalog → built-in declaration → name inference → alias. Unrecognized models fall into "uncategorized". Name inference is for discovery; manual overrides correct real capability.

Available model options are filtered by account permission and never expose channels, providers or mapped internal model IDs to regular accounts:

```http
GET /api/model/options
Authorization: Bearer ACCOUNT_ACCESS_TOKEN
```

Administrators can pass `?scope=all` for the full management scope. An alias only exposes operations and modes supported by **all** visible targets, so a target that cannot execute is never routed to.

Manual capability override:

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

`capabilities: null` restores automatic capability. Overrides are stored separately and do not rewrite provider discovery results. With model mapping enabled, `catalog_model_name` points to the real target of the override. A manual declaration cannot enable a protocol the adapter does not implement. Token model allowlists, account groups, plan model limits and channel capability are all validated server-side.

## Date suffixes and family capability

Implemented DashScope native families accept valid `-YYYY-MM-DD` / `-YYYYMMDD` date suffixes: routing uses family capability while the upstream model ID is sent unchanged. This does not guarantee the provider enabled a given dated version; arbitrary custom suffixes never gain native protocol capability.

Capability and channel catalogs use a local cache of at most 30 seconds, invalidated immediately by local catalog or channel changes; other instances converge on the TTL. Account permissions are still checked on every request and cross-account authorization is never cached.

## Authentication split

`/api` management endpoints use an account session or account access token; `/v1` inference endpoints use an issued gateway model token. Do not mix them. Management endpoints return `{success,message,data}`; inference endpoints return protocol responses with real HTTP status codes.

## Related

- [Image, audio and rerank endpoints](./image-audio)
- [Video jobs and realtime voice](./video-realtime)
- [Settlement and probes](./settlement-probe)
- [Model aliases and pools](../user/model-alias)
