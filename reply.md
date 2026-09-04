已完成 FLEX-43、FLEX-44、FLEX-45，三个子任务及 FLEX-42 均已进入 `in_review`。

实现内容：

- 新增 `modelrouter/`：`ModelRouter`/`ModelScorer` 接口、线程安全 registry，以及 `round_robin`、`random`、`scoring` 三种策略；关键词评分覆盖代码、翻译、数学推理、创意和简单对话，无法匹配时回退 random，并可后续插入 LLM/embedding scorer。
- 新增 `MODEL_AUTO_ENABLED`（默认 false）、`MODEL_ROUTER_STRATEGY`（默认 random）和预留的 `SCORING_LLM_PROVIDER` 配置。
- 在 TokenAuth 后、套餐校验和渠道路由前解析 `model: auto`；候选模型取用户分组与令牌模型白名单的交集，选定结果同时写入 context 和可复用请求体，避免下游计费/适配器继续看到 `auto`。
- 新增 `OriginalRequestModel`，并保持 `Distribute()` 直接组合时也能完成 auto 解析。

提交：`846cd73 feat(modelrouter): add automatic model routing`

远端分支：https://github.com/sjzsdu/one-api-pro/tree/agent/builder/e42682fdb193

验证结果：

- `make build`：通过，成功生成前端并编译 `dist/one-api-pro`
- `go test . ./modelrouter ./middleware ./router`：通过
- `go test -race ./modelrouter ./middleware`：通过
- `git diff --check`：通过

`go test ./...` 的本次相关包均通过；全仓仍被既有的 `common/image` 远程 Wikimedia 图片测试阻断（远端返回非图片内容后测试自身发生 nil panic）。前端依赖安装同时报告既有 3 个 npm audit 漏洞（2 moderate、1 high）。

PR 未能创建：当前 `gh` 登录账号为 `sunjuzhong`，对代码所在的 `sjzsdu/one-api-pro` 只有 READ 权限；向 fork 和上游创建 PR 均被 GitHub 拒绝为 `must be a collaborator`。代码和提交已成功推送到上述远端分支。
