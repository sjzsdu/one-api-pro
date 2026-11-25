---
title: Model Aliases and Pools
description: "Personal and global model aliases, weighted selection, session affinity and failover"
category: user
order: 15
---

# Model Aliases and Pools

Configure personal aliases under **Settings → Personal → Model aliases and pools**. A super admin can switch to **Global aliases** and publish a pool visible to all users; the global entry is only visible to super admins, while admins and regular users can only configure personal aliases. Existing super-admin personal aliases are never published automatically.

One alias holds at most **8** distinct concrete models, each with a weight from 1 to 1000. New sessions pick by weight; this is a **session allocation ratio**, not an exact ratio of tokens or spend, and it never reads provider purchase quota. Legacy single-target configs keep working.

Resolution order: a real available model name first, then personal aliases, then global aliases. A personal entry with the same name overrides the global one, and an invalid personal entry never silently falls back to global.

## Calling

Keep sending the alias, and always pass the same identifier for one conversation:

```http
POST /v1/chat/completions
Authorization: Bearer sk-…
Content-Type: application/json
X-Session-ID: conversation-123

{"model":"chat-tool","messages":[{"role":"user","content":"hello"}]}
```

`X-Session-ID` is capped at 128 bytes. Sessions are isolated per account, alias and personal/global scope. Without the header, the gateway matches the longest prefix of the structured message history; a hit keeps the original session, and only a miss picks by weight.

Session binding has a default idle TTL of 3600 seconds, configurable 60–86400; a successful call refreshes it. Redis mode uses shared atomic bindings; without Redis it uses a bounded in-process cache, and bindings are not shared across processes.

## Automatic recognition without client changes

Chat Completions, Messages and Responses clients that always send the full message history need no extra header. Routing order: explicit `X-Session-ID` → longest existing message prefix → weighted selection. Explicit sessions neither read nor write the auto prefix index; the two are isolated.

Matching works on parsed message structure, so JSON field order and whitespace do not affect identity, while array order, roles, tool calls, media content and tool definitions stay significant. Responses uses the raw structured `input` rather than flattening media or tool items into text. Per-turn generation options such as `stream`, `temperature` and `max_tokens` never participate in identity.

Only prefixes with at least **32 non-whitespace user text characters** take part; system prompts, tool descriptions and media data never pad the count. A shared long system prompt plus "hello" therefore cannot merge different conversations. Insufficient history, a miss, truncation or compression causes reassignment. Identical long prefixes may still be treated as one session, so automatic recognition cannot replace an explicit conversation ID.

The prefix index stores only hashes, expiry and a session routing reference — never message bodies; after a failover, the old prefix follows the same reference to the new model. Each account/alias/scope/protocol keeps at most 1024 records, each lookup inspects at most the latest 128 eligible message boundaries, and the TTL follows the session config. The local index is further limited to 256 namespaces and 8192 total records; Redis shares the index, while multiple processes without Redis do not share recognition results.

The settings test dialog still sends a short test message, so clearing the session ID cannot validate history-based matching; verify with real conversations that keep appending full history.

The same session is pinned to the same model and successful channel as far as possible, which helps reuse provider prompt caches but **does not guarantee a cache hit**. Caching also depends on identical prompt prefixes, tool definitions, and the provider's cache scope and lifetime.

## Permissions and pricing

A global alias can be added to a token allowlist as a model; its candidates must still belong to the caller's current group. A token may also allow only one concrete candidate, in which case only that candidate is usable. Personal aliases always require permission on the target model; a personal config sharing a global alias name cannot inherit the alias's authorization.

Each request re-filters group, token and known tool/vision/context capability limits; unknown capabilities are never assumed verified. After a candidate is removed or a permission revoked, old sessions reselect an available candidate. Cost and plan are recomputed on the final actual model, and logs keep both the business alias and the actual upstream model.

## Failover

Pool failover is optional. Connection failures, timeouts, 429, 529 and upstream 5xx trigger bounded retries: first another healthy channel for the same model, then an untried model in the pool. Failed attempts return pre-deducted quota synchronously; the successful attempt settles under its own price and plan. Parameter, permission, balance or refund failures never trigger cross-model retries.

Exhausted candidates cool down for 30 seconds, isolated by group and pool. When all candidates are cooling, the gateway returns a retryable HTTP 503. After a switch, the same session keeps using the new model and does not bounce back when the original recovers. Each request tries at most 8 models, with per-model channel retry limits.

Once response data has been emitted or consumed by a protocol converter, transparent retry or appended error JSON is no longer possible. The first multi-model pool release supports JSON Chat Completions, Completions, Claude Messages and Responses; other formats keep single-target alias support. Provider session references such as `previous_response_id` and `conversation` cannot be safely migrated by the current Responses converter, so pools reject them and require the full chat history.

## Management API

| Purpose | Endpoint |
| --- | --- |
| Personal config | `GET/PUT /api/user/model_aliases` |
| Effective config for current user | `GET /api/user/model_aliases/effective` |
| Super-admin global config | `GET/PUT /api/model_alias/global` |
| Super-admin global candidates | `GET /api/model_alias/global/targets` |

```json
{
  "aliases": [{
    "alias": "chat-tool",
    "targets": [{"model": "deepseek", "weight": 3}, {"model": "glm", "weight": 1}],
    "failover_enabled": true,
    "session_ttl_seconds": 3600
  }]
}
```

The legacy `target_model` field remains accepted; when `targets` is provided it wins, and the response's `target_model` is the first candidate.

## Related

- [Multimodal gateway overview](../media/overview)
- [Access Token](./access-token)
- [Chat Playground](./chat)
