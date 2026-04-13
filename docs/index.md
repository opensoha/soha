---
layout: home

hero:
  name: kubecrux
  text: 多集群 Kubernetes 平台控制台
  tagline: 为需要统一身份、权限、集群访问、发布、告警路由和 AI 巡检能力的平台团队而构建。
  actions:
    - theme: brand
      text: 查看架构
      link: /architecture/
    - theme: alt
      text: 本地开发
      link: /development/local-development
    - theme: alt
      text: API 总览
      link: /api/overview

features:
  - title: 平台视图而不是原始 Kubernetes 对象
    details: kubecrux 输出聚合后的工作负载、基础设施、审计和事件视图，避免前端直接承接 Kubernetes 原始复杂度。
  - title: 已收敛的后端架构
    details: Go 后端按 API、application、policy、infrastructure、repository 和 bootstrap 层组织成模块化单体。
  - title: 功能已进入真实闭环
    details: 当前仓库已经包含访问控制管理、发布中心 MVP、告警路由与分发、存储资源视图以及平台原生 AI 巡检能力。
  - title: 文档与代码同步演进
    details: 文档站由 VitePress 构建，和仓库中的代码、目录和架构一起维护。
---

## 为什么是 kubecrux

kubecrux 不是对 Kubernetes Dashboard 的简单包装。它是一个位于集群 API 之上的平台控制面，提供：

- 多集群接入与健康感知
- 聚合的工作负载与基础设施视图
- 统一的审计中心与事件中心
- 基于 RBAC + ABAC 的访问控制
- 应用构建与发布中心
- 告警路由与通知通道
- 平台原生 AI 巡检任务与巡检记录
- 面向 agent 模式和 MCP 适配器的稳定扩展点

## 仓库结构

- `web`: 基于 Vite + React + TypeScript 的前端控制台
- `cmd` + `internal`: 基于 Go 的模块化单体后端与 agent 运行时
- `configs`: 后端与 agent 的配置文件
- `docs`: 使用 VitePress 构建、并与代码骨架同步维护的文档站

## 快速开始

### 后端

```bash
docker compose up -d postgres redis
go run ./cmd/server
```

### 前端

```bash
cd web
npm install
npm run dev
```

当前活跃前端已经收敛为 `web/` 下的一套 Vite SPA，目录重点如下：

- `src/main.tsx`: Query Client 与 `BrowserRouter` 启动
- `src/routes/index.tsx`: lazy route 注册表
- `src/routes/meta.ts`: 侧边栏分组与面包屑元信息
- `src/layouts/app-layout.tsx`: Semi Design 控制台壳层
- `src/features/*`: 按平台、交付、可观测性、权限、系统、设置分组的页面模块
- `src/services/api-client.ts`: `/api/v1` 客户端与 token refresh 重试逻辑
- `src/stores/*`: 认证、平台范围、偏好等持久化状态

### 文档站

```bash
cd docs
npm install
npm run docs:dev
```

## 本地依赖

### PostgreSQL

- host: `localhost`
- port: `5432`
- database: `kubecrux`
- username: `pgsql`
- password: `pgsql`

### Redis

- host: `localhost`
- port: `6379`
- password: none

## 当前前端能力面

- 平台：概览、集群、工作负载、网络、存储、扩展、Helm
- 交付：应用、工作流、发布、镜像仓库
- 可观测性：监控、告警、通知、值班、事件、AI 助手
- 控制面：权限管理、系统管理、设置、内嵌文档

## 推荐阅读入口

- 仓库级工程记忆主文件：项目根目录 `agents.md`
- [架构入口](/architecture/)
- [AI Copilot](/architecture/ai-copilot)
- [应用交付](/architecture/application-delivery)
- [监控与告警](/architecture/monitoring-and-alerting)
- [权限模型](/architecture/authorization)
- [配置说明](/operations/configuration)
- [MCP](/operations/mcp)
