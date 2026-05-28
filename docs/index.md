---
id: index
slug: /
title: soha 文档
description: 多集群 Kubernetes 平台控制台的架构、开发、API 与运维文档。
---

# soha 文档

soha 是一个多集群 Kubernetes 平台控制台。它不是对原生 Kubernetes Dashboard 的简单封装，而是面向平台团队的统一控制面，覆盖集群接入、工作负载观测、交付编排、权限治理、告警协同和 AI 辅助分析。

## 文档站定位

- 文档站基于 Docusaurus 构建，并与仓库代码一起演进
- 开发态默认地址为 `http://localhost:3000/docs/`
- 生产态默认挂载在同源 `/docs/`

## 你可以从这里开始

- [架构入口](./architecture/index.md)
- [本地开发](./development/local-development.md)
- [API 总览](./api/overview.md)
- [运维配置](./operations/configuration.md)
- [路线图](./roadmap/index.md)

## 当前能力边界

- 平台管理：多集群、节点、命名空间、工作负载、网络、存储、扩展、Helm
- 交付能力：应用、环境、工作流、发布、镜像仓库
- 可观测性：监控、告警、通知、事件、AI 观测分析
- 访问与系统：用户、角色、用户组、策略、菜单、审计、设置

## 仓库结构

- `cmd`：服务端与 agent 入口
- `internal`：API、application、policy、infrastructure、repository 等后端层
- `web`：React 18 + Vite 6 + TypeScript 5 控制台
- `docs`：Docusaurus 文档站
- `configs`：服务端与 agent 配置
- `migrations`：数据库初始化与演进脚本

## 快速启动

### 后端

```bash
docker compose -f deploy/docker-compose.yaml up -d postgres
go run ./cmd/server
```

### 前端

```bash
cd web
npm install
npm run dev
```

### 文档

```bash
cd docs
npm install
npm run dev
```

开发时如果同时启动 `web`，Vite 会把同源 `/docs/` 代理到 `http://localhost:3000/docs/`。

## 推荐阅读

- 仓库级工程规范：根目录 `agents.md`
- [架构入口](./architecture/index.md)
- [应用交付](./architecture/application-delivery.md)
- [监控与告警](./architecture/monitoring-and-alerting.md)
- [权限模型](./architecture/authorization.md)
- [AI Copilot](./architecture/ai-copilot.md)
- [MCP 集成](./architecture/mcp-integration.md)
