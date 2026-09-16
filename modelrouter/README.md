# Cluster router

`cluster` is the only intelligent model-routing strategy. Configure it with:

```env
MODEL_ROUTER_STRATEGY=cluster
EMBEDDING_PROVIDER=onnx
EMBEDDING_MODEL=jina-v2-code
```

At startup, cluster routing downloads the selected Weave-compatible,
offline-evaluation bundle (`CLUSTER_ARTIFACT_VERSION`, default `v0.75`) to
`~/.one-api-pro/cluster-artifacts`, and downloads the configured ONNX
embedder when necessary. Set `CLUSTER_ARTIFACT_DIR` to use a reviewed local
or internal artifact mirror.

The bundle contains 768-dimensional centroids, per-cluster quality probes,
and operational costs. Runtime embeds once, selects the nearest clusters,
then selects the available candidate with the best offline quality/cost score.
Simple translation, chat, summary, and rewrite requests are cost-capped by
`CLUSTER_SIMPLE_MAX_ALPHA` (default `0.20`); normal requests use
`CLUSTER_NORMAL_MAX_ALPHA` (default `0.65`); complex requests retain the
artifact's quality preference.

Only models present in the artifact are selectable. Add locally reviewed
bootstrap priors through `CLUSTER_MODEL_OVERRIDES_PATH`; an override copies a
known model's features and applies an explicit quality delta. It is not an
evaluation result and should be replaced with measured evidence when
available.

`EMBEDDING_MODEL_PATH`, `EMBEDDING_TOKENIZER_PATH`, `EMBEDDING_CACHE_DIR`,
and `ONNXRUNTIME_LIBRARY` are optional advanced settings. `EMBEDDING_PROVIDER=api`
uses an OpenAI-compatible embedding endpoint instead of local ONNX inference.
