---
title: Documentation
description: "One API Pro docs: getting started, deployment, channels and multimodal, subscriptions, clusters, API reference and changelog"
layout: home

hero:
  name: One API Pro
  text: Enterprise AI API Gateway
  tagline: One gateway for text, image, audio and video models — channel routing, subscription billing and multi-node clusters out of the box
  actions:
    - theme: brand
      text: 5-Minute Quick Tour
      link: /en/start/quick-tour
    - theme: alt
      text: Multimodal
      link: /en/media/overview
    - theme: alt
      text: GitHub
      link: https://github.com/sjzsdu/one-api-pro

features:
  - icon: 🚀
    title: Getting Started
    details: Project overview, architecture and a 5-minute path to a running instance.
    link: /en/start/overview
    linkText: Start here
  - icon: 📦
    title: Install & Upgrade
    details: Docker single instance, docker-compose, source builds, environment variables, upgrades and backup.
    link: /en/install/docker-deploy
    linkText: Deployment options
  - icon: 🎛️
    title: Multimodal & Media
    details: Text-to-image, image editing, TTS and ASR, rerank, async video jobs and realtime voice — one token and billing system.
    link: /en/media/overview
    linkText: Explore multimodal
  - icon: 🔀
    title: Channels
    details: Add channels, model catalog and mapping, routing policies, connectivity tests and balance refresh.
    link: /en/channel/overview
    linkText: Configure channels
  - icon: 🧩
    title: Model Aliases & Pools
    details: Personal and global aliases with up to 8 weighted candidates, keeping one session on the same model and channel.
    link: /en/user/model-alias
    linkText: Use model aliases
  - icon: 🎟️
    title: Subscription (Token Plan)
    details: Plan design, billing rules, upgrade and downgrade, admin setup and subscription management.
    link: /en/subscription/overview
    linkText: Learn subscriptions
  - icon: 💰
    title: Pricing
    details: Model prices, group discounts, plan pricing, top-up business and payment channel setup.
    link: /en/pricing/model-price
    linkText: Pricing rules
  - icon: 🎁
    title: Redemption
    details: Redemption code generation, redeem flow, quota calculation and admin management.
    link: /en/redemption/overview
    linkText: Use redemption codes
  - icon: 👤
    title: Users & Tokens
    details: Registration and login, dashboard, access tokens, orders and the Chat Playground.
    link: /en/user/registration
    linkText: User guide
  - icon: 🌐
    title: Decentralized Cluster
    details: Node management, config sync, node health and multi-node deployment.
    link: /en/decentralization/overview
    linkText: Deploy a cluster
  - icon: 🔌
    title: API Reference
    details: Authentication, OpenAI and Anthropic compatible endpoints, management APIs and error codes.
    link: /en/api/README
    linkText: Read the API
  - icon: 🗂️
    title: Data Schema
    details: Table structures and fields for users, tokens, channels, pricing, orders and subscriptions.
    link: /en/schema/overview
    linkText: Browse the schema
  - icon: 📝
    title: Changelog
    details: Version index with feature changes and upgrade notes for every release.
    link: /en/changelog/index
    linkText: See the latest release
  - icon: 🛠️
    title: Contribute & Misc
    details: Dev setup, coding and commit conventions, adding providers and payment channels, troubleshooting.
    link: /en/contribute/dev-setup
    linkText: Contribute

---

## What's new in v0.0.29

Three capability groups, all documented:

- **Model alias pools**: personal and global aliases, weighted selection, session affinity and in-pool failover → [Model Aliases and Pools](/en/user/model-alias)
- **Native multimodal gateway**: `wan2.6-t2i`, Wan2.7 image editing, `seed-tts-2.0`, `qwen3-asr-flash`, `gte-rerank-v2`, `wan2.6-t2v` and Qwen Omni realtime voice → [Multimodal Gateway Overview](/en/media/overview)
- **Inference jobs and reconciliation**: reserve quota, settle on real usage, keep unknown outcomes for reconciliation, with three admin reconcile endpoints → [Settlement and Probes](/en/media/settlement-probe)

## Common entry points

| I want to… | Go to |
| --- | --- |
| Run an instance with Docker | [Docker Deployment](/en/install/docker-deploy) |
| Connect a new provider | [Add a Channel](/en/channel/add-channel), [Provider List](/en/channel/provider-list) |
| Call it with the OpenAI SDK | [OpenAI Compatible API](/en/api/openai-compatible) |
| Generate images, audio or video | [Image, Audio and Rerank](/en/media/image-audio), [Video Jobs and Realtime](/en/media/video-realtime) |
| Sell plans and take payments | [Plan Settings](/en/subscription/plan-settings), [Payment Settings](/en/pricing/payment-settings) |
| Decode an error | [Error Codes](/en/api/error-code), [Troubleshooting](/en/misc/troubleshooting) |
