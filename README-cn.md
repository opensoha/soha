<h1 align="center">soha</h1>

<p align="center">
  <strong>面向现代平台团队的一体化 Kubernetes 平台控制台。</strong>
</p>

<p align="center">
  在一个权限感知的控制平面中完成集群运维、应用交付、故障分析与运行时管理。
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.23-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://react.dev/"><img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=111111"></a>
  <a href="https://ant.design/"><img alt="Ant Design" src="https://img.shields.io/badge/Ant%20Design-6-1677FF?logo=antdesign&logoColor=white"></a>
  <a href="https://kubernetes.io/"><img alt="Kubernetes" src="https://img.shields.io/badge/Kubernetes-client--go-326CE5?logo=kubernetes&logoColor=white"></a>
  <a href="https://www.postgresql.org/"><img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-18.4-4169E1?logo=postgresql&logoColor=white"></a>
  <a href="./docs/"><img alt="Docs" src="https://img.shields.io/badge/Docs-Docusaurus-3ECC5F?logo=docusaurus&logoColor=white"></a>
</p>

<p align="center">
  <a href="#概览">概览</a>
  · <a href="#为什么选择-soha">为什么选择 soha</a>
  · <a href="#功能特性">功能特性</a>
  · <a href="#快速开始">快速开始</a>
  · <a href="#部署">部署</a>
  · <a href="#贡献">贡献</a>
</p>

<p align="center">
  <a href="./README.md">English</a> | <a href="./README-cn.md">简体中文</a>
</p>

## 概览

Soha 是一个面向平台团队的全栈控制平面，用于管理 Kubernetes 以及周边运行时基础设施。项目将 Go API 服务、React + Ant Design 控制台和仓库内 Docusaurus 文档整合为一个可部署单元。

Soha 的目标不只是资源浏览器。它把集群运维、应用交付、可观测性、运行证据、访问控制、AI 调查、虚拟化和 Docker 运维连接到同一个控制台中。

## 为什么选择 soha

- **一个项目，一个运行时**：需要紧凑部署时，可以用一个应用容器同时交付 API、控制台和文档。
- **面向操作员的工作流**：资源列表、作用域动作、YAML、事件、指标、日志和长耗时操作记录都是一等能力。
- **权限感知的设计**：菜单、路由、按钮、API 授权、审计日志与作用域授权是相互对齐但边界清晰的控制点。
- **Agent-ready 架构**：远程集群、AI provider、Docker 操作和持久化执行任务都可以通过 token 保护的 runner claim/callback 路径运行。
- **为持续演进而设计**：平台、交付、可观测性、AI、虚拟化和 Docker 工作台共享同一个模块化单体后端与路由驱动的前端壳层。

## 功能特性

| 领域 | Soha 提供的能力 |
| --- | --- |
| 平台运维 | 多集群资产、节点、命名空间、工作负载、网络、存储、CRD、Helm、YAML、日志、事件、指标与操作入口。 |
| 应用交付 | 应用、服务、容器、构建模板、工作流模板、发布包、执行任务、审批、发布、镜像仓库与交付记录。 |
| 可观测性 | 监控总览、告警资产、告警事件、通知策略、自愈策略、值班路由、排班、升级策略与事件流。 |
| AI 工作台 | 会话式聊天、根因分析、性能分析、巡检复盘、MCP 证据采集、工具集、技能与 provider 执行。 |
| Agent Runtime | 远程集群模式、runner claim/callback API、执行心跳、任务取消、Docker 操作回调与 provider 无关的 AI 执行。 |
| 虚拟化 | KubeVirt 与 Proxmox VE 连接、虚拟机生命周期、镜像和规格目录、控制台、指标、操作与同步任务。 |
| Docker 工作台 | Docker 主机资产、Compose 项目、服务、端口映射、模板、单容器启动与 token 保护的 runner 操作。 |
| 访问与系统 | 用户、角色、用户组、策略、作用域授权、菜单、设置、公告、审计日志与操作日志。 |

## 架构

Soha 采用模块化单体后端与基于路由注册的前端壳层。

```text
浏览器控制台
      |
      v
React 18 + Vite + Ant Design
      |
      v
Gin API Server
      |
      +--> 应用服务
      +--> 策略引擎
      +--> Repository
      +--> Kubernetes / Agent / Docker / Virtualization / MCP 集成
      |
      v
PostgreSQL + Kubernetes 集群
```

### 后端

- `cmd/server`: API 服务入口
- `cmd/agent`: 远程集群 agent 与 runner 入口
- 未来 `cmd/**` 入口：同仓库内用于安全上报、worker 等专门负载的子服务入口
- `internal/api`: 领域路由注册文件、处理器、中间件、请求解析与响应封装
- `internal/application`: 用例编排、授权、作用域处理、审计与视图模型
- `internal/policy`: RBAC、ABAC 与作用域计算
- `internal/infrastructure`: 配置、数据库、Kubernetes、informer、agent、日志、Swagger、MCP
- `internal/repository`: 持久化访问层
- `internal/bootstrap`: 依赖装配、迁移、初始化与启动生命周期

当前路由、bootstrap、多 `cmd` 入口和预留安全 ingest 边界约定见 [后端结构](./docs/development/backend-structure.md)。

### 前端

- `web`: React 18 + TypeScript 5 + Vite 控制台
- `web/src/routes`: 路由注册与元数据
- `web/src/layouts`: 控制台壳层
- `web/src/features`: 路由级业务模块
- `web/src/components`: 共享 UI 组件与复用部件
- `web/src/services`: API 访问辅助
- `web/src/stores`: 认证、偏好与平台作用域状态
- `web/src/theme/app-theme.ts`: Ant Design 主题 token 与共享 CSS 变量

### 文档

- `docs`: Docusaurus 文档站点，承载架构、开发、API 与运维文档

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 后端 | Go 1.23、Gin、PostgreSQL、Kubernetes `client-go` |
| 前端 | React 18、TypeScript 5、Vite 6、React Router 6、TanStack Query 5、Zustand 5、Ant Design 6、Tailwind CSS 4 |
| 文档 | Docusaurus 3 |
| 打包部署 | Docker、Docker Compose、原生 Kubernetes YAML、Helm |

## 目录结构

```text
.
├── cmd/                 # server、agent 与未来同仓库服务入口
├── configs/             # 后端与 agent 配置
├── docs/                # Docusaurus 文档站点
├── internal/            # 后端分层与领域模块
├── migrations/          # PostgreSQL 初始化与迁移
├── web/                 # 当前活跃前端应用
├── deploy/              # Docker、Compose、原生 Kubernetes 与 Helm 部署资产
├── Makefile             # 常用开发、构建、部署命令
└── agents.md            # 工程规范与项目记忆
```

## 快速开始

### 环境要求

- Go 1.23+
- Node.js 20+
- Docker 与 Docker Compose
- PostgreSQL 18.4，用于本地后端开发

### 安装依赖并启动本地服务

```bash
make init
```

该命令会安装 Go、前端和文档依赖，并从 `deploy/docker-compose.yaml` 启动本地 PostgreSQL 服务。开发辅助流程也可以启动本地 k3s 调试集群，并把 kubeconfig 写入 `./.dev/k3s/kubeconfig.yaml`。

Compose 栈使用 `postgres:18.4`，并把命名卷挂载到 `/var/lib/postgresql`，以匹配 PostgreSQL 18 的默认数据目录布局。由 PostgreSQL 16 创建的本地数据卷不能只改镜像标签后直接复用；可丢弃的本地数据卷请重建，需要保留的数据请通过 `pg_dump`/`pg_restore` 或 `pg_upgrade` 迁移。

### 启动 API 和控制台

```bash
make
```

默认目标会同时启动 Go API 与 Vite 前端。

- 控制台：`http://localhost:5173`
- API：`http://localhost:8080`
- 配置覆盖：`SOHA_CONFIG_FILE=/abs/path/to/config.yaml`

### 分别启动服务

```bash
make dev-api
make dev-web
make dev-docs
```

### 启动 Agent Runtime

```bash
go run ./cmd/agent
```

默认 agent 配置文件为 [configs/agent.config.yaml](./configs/agent.config.yaml)。可以通过以下方式覆盖：

```bash
SOHA_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml go run ./cmd/agent
```

### 通过 Docker 部署 Hermes Agent Runner

Hermes 作为外部 provider 时，推荐从统一 compose 栈运行 `soha-agent` 派生镜像：镜像继承官方 `nousresearch/hermes-agent`，并内置 soha 的 `cmd/agent` runner，通过 Agent Runtime claim/callback 协议连接控制面。

```bash
make deploy-hermes-setup
make deploy-hermes-runner-up
```

默认连接同一 compose 网络中的 `http://soha:8080`，并使用 `demo-execution-runner-token`。真实环境请覆盖：

```bash
SOHA_CONTROL_PLANE_URL=http://host.docker.internal:8080 \
SOHA_EXECUTION_RUNNER_TOKEN=replace-with-runtime-token \
make deploy-hermes-runner-up
```

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
make init-kubevirt-lab
make init-virtualization-lab
make pve-docker-up
make pve-docker-status
make deploy-image
make deploy-compose-up
make deploy-hermes-setup
make deploy-hermes-runner-up
make deploy-helm-lint
```

## 部署

Soha 默认按单项目运行时交付：一个应用容器同时提供 API、内嵌 SPA 和文档。

- [deploy/Dockerfile](./deploy/Dockerfile): 多阶段镜像构建
- [deploy/Dockerfile.hermes-agent-runner](./deploy/Dockerfile.hermes-agent-runner): Hermes Agent Runtime runner 镜像
- [deploy/docker-compose.yaml](./deploy/docker-compose.yaml): 包含 PostgreSQL 与可选 Hermes runner 服务的本地完整栈
- [configs/config.yaml](./configs/config.yaml): 默认应用配置
- [configs/config.compose.yaml](./configs/config.compose.yaml): compose 应用容器配置，使用 PostgreSQL service host 且不注入宿主机本地 kubeconfig
- [deploy/deployment.yaml](./deploy/deployment.yaml): 原生 Kubernetes 清单基线
- [deploy/chart](./deploy/chart): Helm Chart

```bash
docker build -f deploy/Dockerfile -t soha:single-project .
docker compose -f deploy/docker-compose.yaml up -d --build
helm lint deploy/chart
```

## 文档

- [工程规范](./agents.md)
- [架构总览](./docs/architecture/index.md)
- [应用交付](./docs/architecture/application-delivery.md)
- [AI Copilot](./docs/architecture/ai-copilot.md)
- [权限体系](./docs/architecture/authorization.md)
- [监控与告警](./docs/architecture/monitoring-and-alerting.md)
- [配置说明](./docs/operations/configuration.md)
- [部署说明](./docs/operations/deployment.md)
- [Agent Runtime](./docs/operations/agent-runtime.md)
- [MCP](./docs/operations/mcp.md)

## 开发原则

- 后端 handler 保持轻量。应用服务负责业务编排、授权、作用域语义、审计、操作日志与前端视图模型。
- 保持中心启动和路由文件轻量。新增领域路由放在 `internal/api/routes` 的领域文件中，新增启动职责放在 `internal/bootstrap` 的关注点文件中，避免继续膨胀单个大文件。
- Go 大文件先按稳定行为域拆分。平台 handler、平台资源 service 和 AI Gateway 已按同包聚焦文件组织，并用单元测试保护执行任务状态流转。
- 长耗时工作必须任务化。构建、发布、Docker、Compose、虚拟机控制和 provider 执行都通过持久化任务与 callback 路径完成。
- 未来内网安全工作台 API 需要区分管理面、客户端和 ingest 边界：`/api/v1/security/**`、`/api/client/v1/**` 和 `/api/ingest/v1/**`。
- 前端实现只进入 `web`。路由、元数据、权限、后端菜单和测试需要保持一致。
- 平台 API 返回 Soha DTO，不直接返回原始 Kubernetes 对象，YAML 或明确透传接口除外。
- 模块可见性、菜单可见性和后端授权是不同边界。
- 不手改生成产物。应修改源文件后重新构建。

## 贡献

欢迎提交 issue 与 pull request。较大改动建议先阅读 [agents.md](./agents.md)，以保持后端分层、前端路由、授权、作用域处理与文档更新一致。

常用验证命令：

```bash
go test ./...
cd web && npm run typecheck && npm run build
cd docs && npm run build
```

## 项目状态

Soha 目前处于持续开发中。平台管理、交付、可观测性、AI、虚拟化与 Docker 工作台仍在一起演进，不同模块成熟度并不完全一致。

## 许可

当前仓库顶层尚未提供 `LICENSE` 文件。如果项目准备正式开源发布，请先补充许可证。
