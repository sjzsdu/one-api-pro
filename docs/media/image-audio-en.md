---
title: Image, Audio and Rerank Endpoints
description: "Calling image generation, image editing, TTS, ASR and rerank, with their limits"
category: media
order: 2
---

# Image, Audio and Rerank Endpoints

Examples below are for synchronous native endpoints. Replace the gateway address and placeholder token; media URLs must be **public HTTPS addresses reachable by the provider**.

```sh
export GATEWAY_BASE_URL='https://gateway.example'
export GATEWAY_TOKEN='YOUR_GATEWAY_TOKEN'
```

## Rerank

```sh
curl "$GATEWAY_BASE_URL/v1/rerank" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"gte-rerank-v2","query":"which audio operations does the gateway support","documents":["speech recognition and synthesis","database migration notes"],"top_n":1,"return_documents":true}'
```

- First native adapter: DashScope `gte-rerank-v2`
- 1–500 documents; `top_n` must not exceed the document count
- Metered in tokens

## Speech recognition

```sh
curl "$GATEWAY_BASE_URL/v1/audio/transcriptions" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"qwen3-asr-flash","audio":"https://media.example/sample.wav","language":"zh","response_format":"json"}'
```

- Two protocols coexist: OpenAI multipart (`file` upload) and the DashScope `qwen3-asr-flash` native JSON protocol (`audio` as a public URL or data URI)
- Qwen ASR accepts audio up to 5 minutes
- The native endpoint currently returns JSON transcripts only
- Uploaded files are capped at 10 MiB each
- Metered in **seconds**; legacy OpenAI-compatible ASR keeps its original transcript-token billing and must not be priced as Qwen3-ASR per-second

## Image editing

```sh
curl "$GATEWAY_BASE_URL/v1/images/edits" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"wan2.7-image","prompt":"change the background to light blue","images":["https://media.example/source.png"],"n":1,"response_format":"url"}'
```

- First native adapter: DashScope Wan2.7 image editing
- 1–9 reference images, 1–4 outputs per call
- Multipart upload of a single `image` is supported (10 MiB limit)
- **No** mask support and no 4K editing
- Replace the model name with the actual Wan2.7 edit model configured on the channel or its public alias

## Image generation, TTS and embeddings

These reuse existing adapters and follow the OpenAI-compatible protocol:

| Capability | Endpoint | Notes |
| --- | --- | --- |
| Text to image | `POST /v1/images/generations` | First native model `wan2.6-t2i`; billed by output images |
| Text to speech | `POST /v1/audio/speech` | First native model `seed-tts-2.0`; per million characters or per token depending on the model |
| Embeddings | `POST /v1/embeddings` | Includes DashScope native multimodal embeddings; total-only usage is billed on input tokens |

Request bodies follow the protocol and the model's declared capability. Channel model selection, account groups, plan limits and token allowlists are validated together server-side.

## Billing notes

Typed synchronous endpoints return a persistent billing record ID in the `X-Inference-Id` response header. The gateway reserves quota before calling the provider and settles after real usage arrives; if execution or usage is uncertain the reservation is kept, so automatic retries never double-charge.

Refunds happen only when local validation prevented the request from being sent, or when the provider explicitly refused to execute. Transport failures, 408/5xx, failed reads of a successful response, parse failures and results above the limit are treated as **unknown outcome**: the reservation is kept and `outcome_unknown` is returned without automatic retry.

- JSON image results are capped at 64 MiB and are detected explicitly rather than refunded as truncated data
- Extra returned images are settled by actual count; unallocatable extra cost stays pending reconciliation
- Paid multimodal embedding responses missing usage keep the reservation and return `usage_pending` / HTTP 503

See [Settlement and probes](./settlement-probe) for the full settlement and reconciliation flow.

## Related

- [Multimodal gateway overview](./overview)
- [Video jobs and realtime voice](./video-realtime)
- [OpenAI compatible API](../api/openai-compatible)
