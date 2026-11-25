<!--
DEPLOY.md 文档站部署说明（同域 /docs 子路径）/ Deploying the docs site under /docs on the shared domain
版本: v0.0.29
日期: 2026-11-25
作者: Doubao
-->
# 文档站部署（同域 `/docs` 子路径）

主站与文档站共用 `oneapi.wnbgj.com`，通过路径区分：

| 路径 | 内容 | 文件来源 |
| --- | --- | --- |
| `/` | 产品主站（Vue SPA） | `web/build/default-pro/` 或 `make docker-build` 后的二进制内嵌资源 |
| `/docs/` | 文档站（VitePress） | `docs/doc-site/.vitepress/dist/` |

## 构建

```bash
cd docs/doc-site
npm install
npm run build
# 产物：docs/doc-site/.vitepress/dist
```

构建时 `base` 固定为 `/docs/`（见 `.vitepress/config.mjs`）。同步脚本 `scripts/sync-docs.mjs` 会把源 markdown 里的站内绝对链接（`/start/...`、`/api/...` 等）重写成 `/docs/...`；`/v1/...` 这类接口路径不会被改动。

## 发布

```bash
# 服务器上准备目录：/srv/docs（即 dist 内容）
rsync -az --delete docs/doc-site/.vitepress/dist/ user@host:/srv/docs/
```

## nginx

关键点：`/docs/` 必须优先于主站的 SPA fallback 匹配，否则 `/docs/media/overview` 会被 `index.html` 兜住，表现为“点文档回到首页”。

```nginx
server {
    server_name oneapi.wnbgj.com;

    # 主站：SPA history fallback
    root /srv/web;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    # 文档站：cleanUrls（$uri.html）优先，不要回退到主站 index.html
    location /docs/ {
        root /srv;          # /docs/xxx -> /srv/docs/xxx
        index index.html;
        try_files $uri $uri.html $uri/ =404;
    }

    # 可选：静态资源长缓存
    location ~* ^/docs/assets/ {
        root /srv;
        expires 30d;
        add_header Cache-Control "public, immutable";
    }
}
```

说明：

- 用 `root /srv` + 目录名 `docs`，避免 `alias` 与 `try_files` 组合下 `$uri` 带前缀导致 404。
- 不要给 `location /docs/` 写 `try_files ... /index.html`，那会重新引入“任何文档路径都返回首页”的问题。
- `/docs/` 是一个真正的文档首页（`layout: home`，14 个分类入口卡片 + 版本重点 + 常见入口），内容维护在 `docs/index-zh.md` / `docs/index-en.md`。
- 站点 `base` 由 VitePress 统一处理：源 markdown 保持 `/start/overview` 这类干净路径即可，**只有** `.vitepress/config.mjs` 里的原始 HTML（页脚链接、head 标签）需要手写 `/docs` 前缀。

## 校验

```bash
curl -s -o /dev/null -w "%{http_code} %{size_download}\n" https://oneapi.wnbgj.com/docs/
curl -s https://oneapi.wnbgj.com/docs/media/overview | grep -o "<title>[^<]*</title>"
curl -s -o /dev/null -w "%{http_code}\n" https://oneapi.wnbgj.com/docs/logo.png
```

期望：`/docs/media/overview` 返回 `多模态网关总览 | One API Pro Docs`，而不是主站标题；`/docs/logo.png` 返回 200。
