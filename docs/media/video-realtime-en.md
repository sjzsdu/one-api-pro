---
title: Video Jobs and Realtime Voice
description: "Async video job lifecycle, media assets and Qwen Omni realtime sessions"
category: media
order: 3
---

# Video Jobs and Realtime Voice

## Video jobs

Creating a video job requires an `Idempotency-Key` (max 128 characters). Reuse the same key for the same business intent:

```sh
curl "$GATEWAY_BASE_URL/v1/media/jobs" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: video-order-example-001' \
  -d '{"model":"wan2.6-t2v","operation":"video.generate","prompt":"a sailboat on the sea at dawn, slow dolly in","size":"1280*720","duration":5}'
```

Returns **HTTP 202** with the job body. Same account, same key, same creating token and normalized body return the same job; a different body or token returns 409.

| Parameter | Value |
| --- | --- |
| `prompt` / `negative_prompt` | required / optional |
| `duration` | 2–15 seconds, default 5 |
| `size` | `1280*720`, `720*1280`, `1920*1080`, `1080*1920` |
| `seed` | optional |

### Managing jobs and assets

| Action | Path |
| --- | --- |
| Get job | `GET /v1/media/jobs/:id` |
| Request cancel | `POST /v1/media/jobs/:id/cancel` |
| Download local asset | `GET /v1/media/assets/:id` |

All of the above require the **original account and original creating token**. Another token on the same account cannot read the job or asset. A cancel request is not a confirmed cancel: providers only cancel jobs still queued, and the gateway keeps polling until a final state.

- Execution status, `asset_status` and `settlement_status` describe upstream execution, asset landing and billing separately
- Timeouts, expired submission leases or a provider `UNKNOWN` move the job to `unknown` / `pending_reconciliation`; it is never regenerated or refunded automatically
- Downloaded assets are stored locally, capped at 512 MiB per file
- Download URLs must pass a public-address check and must not carry credentials
- Assets are valid for **7 days**; downloads always require token auth, expired assets are refused and a background worker cleans them up

## Qwen Omni realtime sessions

Entry point is a WebSocket handshake with a gateway token:

```http
GET /v1/realtime?model=qwen3.5-omni-flash-realtime&max_turns=4&max_output_tokens=1024&max_seconds=120
Authorization: Bearer YOUR_GATEWAY_TOKEN
Upgrade: websocket
```

Supported: `qwen3.5-omni-flash-realtime`, `qwen3.5-omni-plus-realtime` and their dated versions. The model must be enabled on the channel and within account permissions; it is pinned at handshake time and cannot be switched via `session.update`.

Response headers include `X-OneAPI-Realtime-Protocol: qwen-omni-ws-manual-v1`, `X-OneAPI-Realtime-Session` and `X-OneAPI-Realtime-Budget-Quota`. This is explicitly a **Qwen manual WebSocket protocol subset** and must not be assumed compatible with every OpenAI realtime client. Native browser WebSocket cannot set an Authorization header, and this release implements neither browser ephemeral credentials nor long-lived tokens in query strings; use a server-side or native client that can set handshake headers.

### Manual turns

The gateway first sets `turn_detection:null`, the Tina voice, text/audio output and output token limits upstream. The client submits audio turns in this order:

```json
{"type":"session.update","session":{"turn_detection":null,"voice":"Tina","max_tokens":1024}}
{"type":"input_audio_buffer.append","audio":"BASE64_AUDIO_CHUNK"}
{"type":"input_audio_buffer.commit"}
{"type":"response.create"}
```

Replace `BASE64_AUDIO_CHUNK` with valid audio matching the session input format. Append multiple chunks then commit; wait for that turn's `response.done` before starting the next.

Allowed client events: `session.update`, `input_audio_buffer.append/commit/clear`, `input_image_buffer.append`, `response.create/cancel`, and `function_call_output` on `conversation.item.create`. Function tools are client-executed only, at most 16; the gateway does not execute them. Paid extra transcription, search, server-side MCP tools and automatic VAD are all disabled.

Voices: Tina, Ethan. Output modalities: text/audio. Audio formats: pcm, pcm16, wav. Appended image content is capped at 256 KiB. Unknown session settings are rejected.

| Resource | Default and limit |
| --- | --- |
| `max_turns` | default 4, range 1–16 |
| `max_output_tokens` | default 1024, range 1–4096; later session settings cannot exceed it |
| `max_seconds` | default 120, range 1–120 s |
| WebSocket frame | 512 KiB |
| Cumulative client input / upstream output | 8 MiB / 64 MiB |
| Upstream handshake / write timeout | 10 s each |
| Concurrency | channel concurrency limits; released on disconnect |

### Budget and billing

The session reserves its full budget before connecting upstream: each turn is conservatively budgeted at 262144 input tokens and 65536 output tokens with current price and discount, multiplied by `max_turns`. `max_output_tokens` truncates output only and does not shrink the reservation. Insufficient quota is rejected before connecting.

`response.done` bills exactly once per real `response.id`, saving input/output tokens and full modality detail. Key rules:

- Each turn's usage is that turn's total; the gateway sums turns rather than diffing cumulative values
- Conflicting usage for the same ID, missing usage, unexpected auto-generation, overruns or interrupted generation end the session and keep the reservation for reconciliation
- Completed turns settle their plan window immediately; closing the session idempotently settles recorded but unfinished turns
- A zero-quota turn still counts as a real request; unknown turns never release request counts on disconnect

### Querying sessions

```http
GET /v1/realtime/sessions/SESSION_ID
Authorization: Bearer YOUR_GATEWAY_TOKEN
```

Administrators can call `GET /api/realtime/session/:id` for session state, price snapshot, per-turn usage, ledger and reconciliation audit — never provider keys or raw audio/video. Sessions after an abnormal disconnect or process failure become `pending_reconciliation`; the worker marks records still active after 5 minutes as pending, without auto-refund.

## Related

- [Multimodal gateway overview](./overview)
- [Image, audio and rerank endpoints](./image-audio)
- [Settlement and probes](./settlement-probe)
