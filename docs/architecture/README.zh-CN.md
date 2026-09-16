# one-api-pro 架构分析

本文档基于当前工作区代码生成，重点说明管理面、兼容转发面、渠道路由、Adaptor、计费和 OpenAI Codex OAuth 的边界与运行关系。图中的源码链接按当前工作区行号标注；代码重排后应以函数名重新定位。

## 文档索引

- [总体架构图](./one-api-pro.architecture.html)：系统边界、主要组件、状态存储和外部依赖。
- [文本请求时序图](./request.sequence.html)：从 `/v1/chat/completions` 到上游响应、计费和重试。
- [Codex OAuth 生命周期图](./openaicodex.lifecycle.html)：首次授权、凭证落库、运行时刷新与失败出口。

## 结论摘要

one-api-pro 是一个以 Go/Gin 为核心的多渠道模型网关。它把“用户可见的 OpenAI-compatible API”与“供应商差异”分开：路由中间件选择渠道，`relay/handler` 编排一次请求，`relay/adaptor` 负责协议适配，数据库保存用户、渠道、Token、配额和日志。

核心调用链可以概括为：

```text
客户端 / 管理前端
        ↓
Gin 路由与认证
        ↓
Distribute：分组、模型、优先级、并发、粘性会话
        ↓
Relay：重试、错误拦截、fallback
        ↓
RelayTextHelper：校验 → 预扣 → 转换 → 上游 → 解析 → 后扣
        ↓
Adaptor：OpenAI、OpenRouter、Codex 及其他供应商实现
        ↓
外部模型服务
```

## 组件职责

| 组件 | 主要职责 | 关键入口 |
| --- | --- | --- |
| 路由与认证 | 暴露 `/api` 管理接口和 `/v1` 兼容接口；按用户、管理员、Root 分级鉴权 | `router/api.go`、`router/relay.go`、`middleware/auth.go` |
| 渠道分发 | 根据用户组、请求模型、渠道状态和路由策略选择渠道；维护并发、RPM、sticky session | `middleware/distributor.go`、`channelrouter/` |
| Relay 控制器 | 调用不同 relay helper；处理错误拦截、重试、备用渠道和 fallback | `controller/relay.go` |
| 文本处理器 | 解析请求、模型映射、价格计算、配额预扣、Adaptor 调用和异步后扣 | `relay/handler/text.go`、`relay/handler/helper.go` |
| Adaptor | 实现 `Init`、URL/Header、请求转换、上游调用、响应解析和模型列表 | `relay/adaptor/interface.go`、`relay/adaptor.go` |
| 渠道模型 | 保存 Type、Key、BaseURL、Models、Group、ModelMapping、并发、RPM、fallback 等配置 | `model/channel.go` |
| 管理前端 | 渠道 CRUD、测试、模型刷新及 OpenAI 设备码登录交互 | `web/default-pro/src/views/channel/Channel.vue` |
| OAuth | 创建 flow、处理 callback/poll、PKCE/token exchange、credential 解析与刷新 | `common/openaioauth/`、`controller/openai_oauth.go` |

## 关键运行机制

### 1. 认证与选路

请求先经过认证中间件建立用户身份。`Distribute` 读取用户组和请求模型，得到渠道候选后交给 `channelrouter`；选择结果通过 `SetupContextForSelectedChannel` 写入 Gin context。渠道状态、支持模型、优先级、权重、最大并发、RPM 和粘性会话共同影响最终选择。

### 2. 协议适配

`relay/adaptor.Adaptor` 是供应商扩展的稳定接口。`relay/adaptor.go` 通过各实现包的 `init()` 注册 Adaptor，运行时按 channel ID 查找实现。这样新增供应商通常不需要改变请求主流程，只需实现适配器、注册类型，并补充模型/测试覆盖。

OpenAI Codex 是一个有特殊认证和响应协议的适配器：它把 OpenAI Chat Completions 请求转换成 Codex Responses 请求，处理 SSE、工具调用、reasoning 和 usage，并在凭证临近过期时刷新并回写渠道 Key。

### 3. 计费与错误处理

文本请求在上游调用前根据模型价格、输入 token 和分组折扣预扣配额；上游明确失败或响应解析失败时返还预扣；成功响应由后台任务按 usage 后扣。`controller/relay.go` 的错误拦截链决定是否重试，重试会重新选渠道；在常规渠道均失败且错误可重试时，再进入配置的 fallback 渠道。

### 4. 模型目录

渠道模型既可以来自静态 Adaptor 常量，也可以通过管理 API 刷新。OpenRouter 模型列表有缓存和刷新逻辑；渠道测试根据模型名识别 embedding 模型并使用 `/v1/embeddings` 请求。模型目录变化会影响管理端列表、渠道可用性和请求选路，因此模型常量更新应和上游 smoke test 一起验证。

## 状态、边界与风险

1. 数据库是用户、渠道、Token、配额、日志和凭证的持久化来源；`Channel.Key` 既承载普通 API Key，也可能承载 Codex credential JSON，应始终按敏感信息保护。
2. 路由缓存和 OAuth flow store 都包含进程内状态。当前实现适合单实例或带有粘性流量的部署；多实例部署需要共享 flow 状态、明确 OAuth callback 归属，并评估缓存一致性。
3. Relay 的重试会重新选择渠道并重复调用 Adaptor，因此上游副作用、流式响应已经开始输出、以及计费预扣返还必须保持幂等或可补偿。
4. Adaptor 是最重要的扩展点，但供应商差异仍可能泄露到请求转换、错误码、usage 解析和模型列表中；新增适配器应同时覆盖成功、流式、错误、计费和模型测试。
5. 管理前端负责发起 OAuth 和展示轮询结果，服务端负责 state、token exchange、凭证落库。生产环境需要限制管理员权限、避免把 access/refresh token 放进日志，并为失败 flow 提供重新授权路径。

## 建议的维护检查清单

- 修改路由或认证：同步检查 `router/`、`middleware/` 以及管理前端调用路径。
- 修改渠道选择：检查分组、模型映射、并发、RPM、sticky session、重试和 fallback 的交互。
- 新增或修改 Adaptor：实现完整 `Adaptor` 接口，补充模型列表、请求/响应测试和上游 smoke test。
- 修改计费：同时验证预扣、返还、成功后扣、流式 usage 和错误日志安全。
- 修改 OAuth：验证 PKCE/state、callback、poll TTL、refresh 回写、并发刷新和多实例部署行为。

## 验证记录

本次文档由 Archify 生成并经过 schema/quality 校验；HTML 交付后还应运行 Archify 的 `visual-check`，其结果以生成的 receipt 为准。代码层面的当前验证基线见仓库工作区：Go 定向测试和前端构建已通过；全量 Go 测试仍有既有 `common/image` JPEG 解码测试失败，需要与本架构文档分开处理。
