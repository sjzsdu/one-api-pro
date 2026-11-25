<!--
gateway-multimodal.md 多模态文档已迁移至文档站分类 / Multimodal docs moved to the doc-site category
版本: v0.0.29
日期: 2026-11-25
作者: Doubao
-->
# 多模态网关文档已迁移

本文档已拆分为文档站「多模态与媒体」分类，原文保留在 git 历史中。请维护以下文件：

| 页面 | 文件 |
| --- | --- |
| 多模态网关总览 | `docs/media/overview-zh.md` / `overview-en.md` |
| 图像、语音与重排接口 | `docs/media/image-audio-zh.md` / `image-audio-en.md` |
| 视频任务与实时语音 | `docs/media/video-realtime-zh.md` / `video-realtime-en.md` |
| 账务结算与巡检 | `docs/media/settlement-probe-zh.md` / `settlement-probe-en.md` |

文档站通过 `docs/doc-site/scripts/sync-docs.mjs` 只同步 `docs/<分类>/<slug>-zh|en.md`，放在 `docs/` 根目录的文件不会上线。
