---
title: Settlement and Probes
description: "Price units, inference reservations and settlement, admin reconciliation, worker config and model probes"
category: media
order: 4
---

# Settlement and Probes

## Price and metering units

Each model currently shares one set of Input / Output / Cached / PerRequest prices; per-operation price rules are not yet available. When operations on the same model use different units, or a custom model's unit is unclear, the pricing page warns and you must verify before enabling.

`InputPrice` / `OutputPrice` are prices per **million** explicit metering units. Rerank is metered in tokens, ASR in seconds, video in seconds. For example, 0.00022 CNY per second must be configured as 220 CNY per million seconds — **never enter 0.00022 directly**.

| Billing type | Metering |
| --- | --- |
| `per_request` | Per call; images by actual output count |
| Character-billed TTS | Per million characters |
| Token-billed TTS / realtime | Per token |
| Rerank | Per token |
| ASR | Per second; legacy OpenAI-compatible ASR stays on transcript tokens |
| Video | Per second |

Zero price is a valid configuration. Prices, discounts and metering rules are snapshotted when a request or job is created; plan reservations also snapshot the rule, billing type and window baseline. A job accepted before plan expiry still settles under the original plan and window; new requests cannot use an expired plan. Quota integers are in millionths of a currency unit and settle with fixed-point rules; usage stores `metric / quantity / scale / unit`. Successful settlement and refunds run idempotently through a persistent ledger.

## Looking up and reconciling sync requests

Text, native operations, audio, image and multimodal embedding requests use persistent billing reservations. On `settlement_pending`, `usage_missing` or an uncertain outcome, record `X-Inference-Id` and **query the original request first; never re-call the provider automatically**:

```http
GET /v1/inferences/INFERENCE_ID
Authorization: Bearer ORIGINAL_CREATING_TOKEN
```

`usage_pending`, `outcome_unknown`, `settlement_pending` and `submission_outcome_unknown` **disable** internal provider and alias fallback and come with `x-should-retry:false` plus `X-Inference-Id`. SDKs should query the original request by error code instead of creating a new call for the same intent. Sync requests offer no general idempotent replay; only video jobs enforce `Idempotency-Key`.

If text generation succeeded but local settlement failed, the atomic billing transaction and reservation are kept while the result is still delivered. Non-streaming responses carry `X-Inference-Status:settlement_pending`; streaming can only promise the request ID and no-retry hint sent before output, since already-sent headers cannot be changed. Exact cost and usage go to a SQL settlement queue; the worker retries at most 4 records per pass and waits at least 30 seconds after failure without calling the model again. Settlement and audit appends each have a 2-second timeout. If the database is entirely unwritable, extra usage and results may not persist; the reservation is not refunded and a server alert is logged for manual provider-bill verification.

The endpoint strictly requires the original account and creating token and returns only public model, billing status, quota and timestamps. Only `settled` records return persisted results; streamed content is not guaranteed to be saved and unsaved results are never replayed. It never returns internal channels, price snapshots, raw prompts or unsettled results.

## Admin reconciliation

### Plain requests

Inspect `GET /api/inference/settlements/:id` for stored usage, price snapshot and audit, then verify the provider side explicitly:

```http
POST /api/inference/settlements/INFERENCE_ID/reconcile
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{"action":"settle","total_quota":40000,"usage":{"prompt_tokens":100,"completion_tokens":50},"note":"verified actual usage and total cost against the provider request log"}
```

- `total_quota` is the actual total cost in millionths of a currency unit
- Optional `usage` is a verified provider usage object or native metering array; when omitted, the stored observed usage is used
- Token plans missing real token usage must supply it, otherwise only cost settles and the plan window is skipped
- When the provider confirms no consumption, use `action:refund`, `total_quota:0`, `confirmed_no_charge:true` and an evidence note
- Only prefixes `text:`, `request:`, `multimodal:`, `audio:`, `image:` are accepted; video jobs, realtime sessions and turn records must use their dedicated endpoints

The record must be flagged pending, or untouched for at least 5 minutes to cover leftover reservations after a process failure. Reconciliation is claimed atomically by admin identity plus the full request hash, then runs the existing idempotent settlement; the same admin and same content may retry, conflicting requests are rejected. A `claimed` audit state means the operation is unfinished — retry it verbatim, never re-claim with a different amount.

### Video jobs

```http
POST /api/inference/jobs/JOB_ID/reconcile
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{"action":"attach_task","provider_task_id":"VERIFIED_EXISTING_TASK_ID","note":"verified the original request and task ID in the provider console"}
```

| action | Required fields and conditions |
| --- | --- |
| `attach_task` | `provider_task_id`; binds an existing provider task to an unknown job, never creates one |
| `confirm_no_charge` | `confirmed_no_charge:true`; only for unknown jobs externally confirmed uncharged, allows refund |
| `record_usage` | `usage` array; only for jobs whose assets landed, settles on verified usage |

All actions require a non-empty `note` and record admin audit info. Usage example: `[{"metric":"video.generated_duration","quantity":5000,"scale":1000,"unit":"second"}]` means 5 seconds. Never use manual usage to mask an unverified provider execution state.

### Realtime sessions

```http
POST /api/realtime/session/SESSION_ID/reconcile
Authorization: Bearer ADMIN_ACCESS_TOKEN
Content-Type: application/json

{"action":"settle","total_quota":40000,"note":"verified the session total of 0.04 CNY against the provider bill"}
```

For `settle`, `total_quota` is the verified session **total**, not a delta. `refund` applies only when the whole session confirmed no consumption and no completed turns exist; `total_quota` must be 0. The action records admin, action, amount and evidence before idempotently finishing sub-requests and the session ledger; retries must keep action, amount and note identical. `reconciling` means a claimed reconciliation is still in progress and may be retried verbatim.

## Deployment configuration

| Config | Default and purpose |
| --- | --- |
| `INFERENCE_WORKER_ENABLED` | Enabled by default; `false` disables job polling, asset cleanup and stale-session marking |
| `INFERENCE_ASSET_DIR` | Defaults to `media-assets` under the data directory; stores local video assets |
| `INFERENCE_JOB_TIMEOUT_SECONDS` | Default 86400, range 60–604800; expired jobs move to pending reconciliation |
| `RELAY_PROXY` | Reuses the gateway proxy for realtime upstream handshakes |

The worker claims up to 4 jobs every 2 seconds with a 120-second lease. Upstream polling starts at 5 seconds; transient failures and states that stay queued/running back off at 5, 10, 20, 40, 60 seconds, resetting when state advances. Unrecoverable protocol, auth or billing errors move to pending reconciliation and stop automatic claiming, without refund or regeneration. Multiple instances coordinate via SQL leases.

**Every API and worker instance must mount the same shared asset volume**; per-container disks make assets unreadable. Only local file storage is provided in this release.

## Model probes

The admin probe endpoint is `POST /api/channel/test_operation/:id` with `{model, operation, confirm_cost: true, input?}`. The catalog declares probeable operations and `requires_input` via a separate `probe_operations`, evaluated independently from runtime `gateway_support`.

| Operation | Probe requirement |
| --- | --- |
| `chat.generate`, `embedding.create`, `rerank.score` | Auto-input and batch probing |
| `audio.transcribe` | Requires a public audio URL |
| `image.edit` | Requires a public image URL and prompt |
| TTS, image generation, video, realtime | Probing disabled; use the call examples |

Requests are sent only after cost confirmation. ASR input example: `input:{audio:"https://media.example/a.wav",response_format:"json"}`; edit example: `input:{images:["https://media.example/a.png"],prompt:"change the background to light blue",n:1}`. Missing or invalid media input means the provider is never called. A missing probe adapter does not imply the model is unhealthy.

Probes use the channel's and model's full adapter config; sent probes record admin, operation, channel, success and available usage audit — never media URLs or keys. Probe cost is borne by the provider channel and never touches user ledgers.

The in-console image/audio media probe console can issue these probes directly, so you can verify a model and channel combination before go-live.

## Regression command

```sh
go test -race ./relay/session ./model ./controller -run 'TestValidateClientEvent_|TestParseCompletion_|TestInferenceSession_|TestRealtime_' -count=1
```

Full verification remains `go build ./...`, `go test ./...` and `npm run build` under `web/default-pro`.

## Related

- [Multimodal gateway overview](./overview)
- [Image, audio and rerank endpoints](./image-audio)
- [Video jobs and realtime voice](./video-realtime)
- [Channel connectivity test](../channel/channel-test)
