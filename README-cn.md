[English](./README.md) | [简体中文](./README-cn.md)

# kubecrux

> 面向平台团队的多集群 Kubernetes 控制台，覆盖平台运维、应用交付、可观测性、访问控制与 AI 辅助分析。

kubecrux 是一个完整的全栈平台控制台项目，后端基于 Go，前端基于 React + Ant Design，文档站点基于 Docusaurus 并随仓库一起维护。它的目标不只是资源浏览器，而是逐步演进为统一的多集群控制平面。

## 项目亮点

- 支持直连集群与 agent 连接模式的多集群 Kubernetes 控制台
- 平台管理覆盖集群、节点、命名空间、工作负载、网络、存储、CRD 与 Helm
- 交付域覆盖应用、工作流、发布、镜像仓库与主数据目录
- 面向告警、事件、根因分析与调查工作流的可观测性与 AI 工作台方向
- 包含权限菜单、审计日志、操作日志、公告与设置中心的控制平面能力
- 提供 Docker、Docker Compose、原生 Kubernetes YAML 与 Helm 的单项目部署资产

## 架构概览

仓库采用模块化单体后端与基于路由注册的前端壳层结构。

### 后端

- `cmd/server`: API 服务入口
- `cmd/agent`: 远程集群 agent 入口
- `internal/api`: 路由、处理器、中间件、请求解析与响应封装
- `internal/application`: 用例编排与面向前端的平台视图模型
- `internal/policy`: RBAC、ABAC 与作用域计算
- `internal/infrastructure`: 配置、数据库、Redis、Kubernetes、informer、agent、日志、Swagger、MCP
- `internal/repository`: 持久化访问层
- `internal/bootstrap`: 依赖装配、迁移、初始化与启动流程

### 前端

- `web`: 当前唯一的活跃 React 18 + TypeScript 5 + Vite 控制台
- `web/src/routes`: 路由注册与路由元数据
- `web/src/layouts`: 控制台壳层布局
- `web/src/features`: 路由级业务模块
- `web/src/components`: 共享 UI 组件与复用部件
- `web/src/services`: API 访问辅助
- `web/src/stores`: 认证、偏好与平台作用域状态
- `web/src/theme/semi-theme.ts`: 共享主题 token 与 CSS 变量基线

### 文档

- `docs`: Docusaurus 文档站点，承载架构、开发、API 与运维文档

## 技术栈

### 后端

- Go 1.23
- Gin
- PostgreSQL
- Redis
- Kubernetes `client-go`

### 前端

- React 18
- TypeScript 5
- Vite 6
- React Router 6
- TanStack Query 5
- Zustand 5
- Ant Design 6
- Tailwind CSS 4

### 文档

- Docusaurus 3

## 目录结构

```text
.
├── cmd/                 # server 与 agent 入口
├── configs/             # 后端与 agent 配置
├── deploy/              # k8s 清单与 Helm 部署资产
├── docs/                # Docusaurus 文档站点
├── internal/            # 后端分层实现
├── migrations/          # SQL 初始化与迁移
├── web/                 # 当前活跃前端应用
├── AGENTS.md            # 工程规范与仓库记忆
├── Dockerfile           # 单项目镜像构建入口
├── Makefile             # 常用开发、构建、部署命令
└── docker-compose.yaml  # kubecrux 与 PostgreSQL 的本地 compose 入口
```

## 当前能力范围

当前产品重点包括：

- 平台管理：集群、节点、命名空间、工作负载、网络、存储、CRD、Helm
- 应用交付：应用、构建模板、工作流模板、发布、仓库连接
- 可观测性：监控、告警、通知、事件、AI 辅助工作流
- 控制平面：用户、角色、用户组、策略、菜单、公告、审计日志、设置

## 快速开始

### 环境要求

- Go 1.23+
- Node.js 20+
- Docker 与 Docker Compose，用于本地基础设施或部署验证
- PostgreSQL 16，用于本地后端开发

### 1. 初始化本地 PostgreSQL

```bash
make init
```

这会通过根目录 `docker-compose.yaml` 启动 `pgsql` 容器，并等待 PostgreSQL 就绪。

### 2. 启动前后端开发环境

```bash
make
```

默认 `make` 会同时启动 Go API 与 Vite 前端。后端默认读取 [configs/config.yaml](./configs/config.yaml)，如需覆盖，使用 `KC_CONFIG_FILE=/abs/path/to/config.yaml`。

### 3. 按需分别启动前后端

```bash
make dev-api
make dev-web
```

前端默认运行在 `http://localhost:5173`，并将 `/api` 代理到 `http://localhost:8080`。

### 4. 启动文档站点（可选）

```bash
cd docs
npm install
npm run dev
```

文档站点默认运行在 `http://localhost:3000/docs/`。

### 5. 启动远程 agent（可选）

```bash
go run ./cmd/agent
```

默认 agent 配置文件为 [configs/agent.config.yaml](./configs/agent.config.yaml)。如需覆盖，使用 `KC_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml`。

## 常用命令

```bash
make
make init
make dev-api
make dev-web
make dev-docs
make build
make test-api
make test-web
```

## 部署

主镜像与本地 compose 入口现在直接位于仓库根目录。

- [Dockerfile](./Dockerfile): 单项目镜像构建入口，内嵌 API、SPA 与 docs
- [docker-compose.yaml](./docker-compose.yaml): 本地完整栈启动文件
- [configs/config.yaml](./configs/config.yaml): 本地开发与容器镜像默认使用的应用配置
- [deploy/k8s/kubecrux-single-project.yaml](./deploy/k8s/kubecrux-single-project.yaml): 原生 Kubernetes 清单
- [deploy/helm/kubecrux](./deploy/helm/kubecrux): Helm Chart

推荐命令：

```bash
make deploy-image
make deploy-compose-up
make deploy-compose-config
make deploy-helm-lint
```

也可以直接执行：

```bash
docker build -t kubecrux:single-project .
docker compose -f docker-compose.yaml up -d --build
helm lint deploy/helm/kubecrux
```

## 文档

主要文档位于 [docs](./docs/)。

- [工程规范](./AGENTS.md)
- [架构总览](./docs/architecture/index.md)
- [应用交付](./docs/architecture/application-delivery.md)
- [权限体系](./docs/architecture/authorization.md)
- [监控与告警](./docs/architecture/monitoring-and-alerting.md)
- [配置说明](./docs/operations/configuration.md)
- [部署说明](./docs/operations/deployment.md)
- [Agent Runtime](./docs/operations/agent-runtime.md)
- [MCP](./docs/operations/mcp.md)

## 贡献

欢迎提交 issue 与 pull request。对于较大的改动，建议先对照 [AGENTS.md](./AGENTS.md)，尤其注意以下规则：

- 后端分层边界
- 前端路由与主题归属
- 作用域与授权语义
- 非平凡改动对应的记忆与文档更新

## 项目状态

kubecrux 目前处于持续开发中。平台管理、交付、可观测性与 AI 模块仍在持续收敛，不同模块成熟度并不完全一致。当前工程基线与范围决策以 [AGENTS.md](./AGENTS.md) 为准。

## 许可

当前仓库顶层尚未提供 `LICENSE` 文件。在对外分发、复用或开源发布前，建议先明确许可证。
