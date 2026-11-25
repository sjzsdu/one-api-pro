---
title: 文档首页
description: "One API Pro 文档：快速开始、部署、渠道与多模态、订阅计费、集群、API 参考与更新日志"
layout: home

hero:
  name: One API Pro
  text: 企业级 AI API 网关
  tagline: 统一管理文本、图像、语音与视频模型 —— 渠道路由、订阅计费、多节点集群开箱即用
  actions:
    - theme: brand
      text: 5 分钟快速体验
      link: /start/quick-tour
    - theme: alt
      text: 多模态能力
      link: /media/overview
    - theme: alt
      text: GitHub
      link: https://github.com/sjzsdu/one-api-pro

features:
  - icon: 🚀
    title: 快速开始
    details: 项目简介、架构总览与 5 分钟体验路径，先跑起来再深入。
    link: /start/overview
    linkText: 进入快速开始
  - icon: 📦
    title: 安装与更新
    details: Docker 单实例、docker-compose、源码编译、环境变量与升级、备份恢复。
    link: /install/docker-deploy
    linkText: 查看部署方式
  - icon: 🎛️
    title: 多模态与媒体
    details: 文生图、图像编辑、语音合成与识别、重排、异步视频任务、实时语音，共用一套 Token 与账务。
    link: /media/overview
    linkText: 了解多模态网关
  - icon: 🔀
    title: 渠道
    details: 新建渠道、模型目录与映射、路由策略、连通性测试与余额自动更新。
    link: /channel/overview
    linkText: 配置渠道
  - icon: 🧩
    title: 模型别名与模型池
    details: 个人与全局别名，最多 8 个加权候选，同一会话固定到同一模型与渠道。
    link: /user/model-alias
    linkText: 使用模型别名
  - icon: 🎟️
    title: 订阅（Token Plan）
    details: 套餐设计、计费规则、升降级、管理员配置与订阅管理。
    link: /subscription/overview
    linkText: 了解订阅
  - icon: 💰
    title: 定价
    details: 模型价格、分组折扣、套餐定价、充值业务与支付通道配置。
    link: /pricing/model-price
    linkText: 查看定价规则
  - icon: 🎁
    title: 兑换码
    details: 兑换码生成与兑换流程、额度计算规则与后台管理。
    link: /redemption/overview
    linkText: 使用兑换码
  - icon: 👤
    title: 用户与令牌
    details: 注册登录、个人仪表盘、Access Token、订单与 Chat Playground。
    link: /user/registration
    linkText: 用户指南
  - icon: 🌐
    title: 去中心化集群
    details: 节点管理、配置同步、节点健康与多节点部署。
    link: /decentralization/overview
    linkText: 集群部署
  - icon: 🔌
    title: API 参考
    details: 鉴权机制、OpenAI / Anthropic 兼容接口、管理 API 与错误码表。
    link: /api/README
    linkText: 查看 API
  - icon: 🗂️
    title: 数据结构
    details: 用户、令牌、渠道、定价、订单、订阅等表结构与字段说明。
    link: /schema/overview
    linkText: 阅读数据结构
  - icon: 📝
    title: 更新日志
    details: 版本索引、每个版本的功能变更与升级注意事项。
    link: /changelog/index
    linkText: 查看最新版本
  - icon: 🛠️
    title: 贡献与其他
    details: 开发环境搭建、编码与提交规范、新增 Provider 与支付通道、常见问题排查。
    link: /contribute/dev-setup
    linkText: 参与贡献

---

## 当前版本重点

**v0.0.29** 带来了三组能力，都已写入文档：

- **模型别名池**：个人与全局别名、加权选型、会话亲和与池内故障切换 → [模型别名与模型池](/user/model-alias)
- **原生多模态网关**：`wan2.6-t2i`、Wan2.7 图像编辑、`seed-tts-2.0`、`qwen3-asr-flash`、`gte-rerank-v2`、`wan2.6-t2v` 与 Qwen Omni 实时语音 → [多模态网关总览](/media/overview)
- **推理作业与对账**：额度预留 → 真实用量结算，未知结果保留待对账，三类管理员对账接口 → [账务结算与巡检](/media/settlement-probe)

## 常见入口

| 我想… | 去哪里 |
| --- | --- |
| 用 Docker 起一个实例 | [Docker 单实例部署](/install/docker-deploy) |
| 接入一个新供应商 | [新建渠道](/channel/add-channel)、[Provider 全清单](/channel/provider-list) |
| 用 OpenAI SDK 直接调用 | [OpenAI 兼容接口](/api/openai-compatible) |
| 生成图片、语音或视频 | [图像、语音与重排接口](/media/image-audio)、[视频任务与实时语音](/media/video-realtime) |
| 配套餐与收款 | [套餐业务配置](/subscription/plan-settings)、[支付配置](/pricing/payment-settings) |
| 查某个报错 | [错误码表](/api/error-code)、[故障排查](/misc/troubleshooting) |
