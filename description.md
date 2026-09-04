## User request

你知道model: auto的实现机制吗? 比如trae有auto这个model, 还有https://github.com/workweave/router 这个项目, 通过prompt智能选择合适的模型进行响应; 我也想实现一个这个能力, 你觉得在当前的这套架构体系下, 怎么规划比较合适.

我觉得选择模型可以做成一个接口, 有很多个实现, 简单一点的就是, 直接轮训, 从现有的模型中一次选模型来用, 或者是随机; 而像workweave/router中的那个是相对高级的实现, 我觉得在架构的时候要依赖于接口去做.

## Context

workweave/router 是一个模型路由器，核心特性：
- 基于 Avengers-Pro 论文的集群评分器（cluster scorer），使用本地 ONNX 嵌入模型对每个请求进行评估，自动选择最优模型
- 支持 Anthropic、OpenAI、Gemini 三种 API 协议
- 路由决策在 <50ms 内完成，可降低 40-70% 成本
- 架构：客户端 → Router(:8080) → Cluster Scorer(ONNX) → Provider
- 支持 BYOK（自带密钥），密钥本地加密存储

## 要求

设计并实现一个 model: auto 智能路由能力，要求：
1. **接口化设计**：模型选择逻辑抽象为接口，便于扩展不同策略
2. **基础实现**：轮询（round-robin）、随机选择
3. **高级实现**：基于 prompt 的智能选择（参考 workweave/router 的思路）
4. **架构约束**：所有实现必须依赖接口，而非具体实现
