[English](./README.md) | [简体中文](./README-cn.md)

# kubecrux

> 面向平台团队的多集群 Kubernetes 控制台，覆盖平台运维、应用交付、可观测性、访问控制与 AI 辅助分析。

kubecrux 是一个完整的全栈平台控制台项目，后端基于 Go，前端基于 React + Ant Design，文档站点基于 Docusaurus 并随仓库一起维护。它的目标不只是资源浏览器，而是逐步演进为统一的多集群控制平面。

## 项目亮点

- 支持直连集群与 agent 连接模式的多集群 Kubernetes 控制台
- 平台管理覆盖集群、节点、命名空间、工作负载、网络、存储、CRD 与 Helm
- 交付工作台覆盖应用、构建模板、工作流模板、发布包、执行任务、审批策略、镜像仓库与主数据目录
- 监控工作台覆盖告警、事件、通知策略、自愈策略和值班协同
- 会话优先的 AI 工作台覆盖通用聊天、根因分析、性能分析、巡检、MCP 证据采集、工具与技能装配以及 Agent Runtime provider 执行
- 可插拔 AI Agent Runtime 以 Hermes 作为首个外部 provider，同时由 kubecrux 统一控制 capability、tool binding、skills、预算、审计和 `AnalysisArtifact` 输出
- 虚拟化工作台覆盖 KubeVirt 与 Proxmox VE 资产、虚拟机生命周期、镜像、规格、控制台、指标、操作与同步任务
- Docker 工作台覆盖 Docker 主机、Compose 项目、服务、端口映射、模板与 runner 回调操作
- 包含权限菜单、审计日志、操作日志、公告与设置中心的控制平面能力
- 提供 Docker、Docker Compose、原生 Kubernetes YAML 与 Helm 的单项目部署资产

## 架构概览

仓库采用模块化单体后端与基于路由注册的前端壳层结构。

### 后端

- `cmd/server`: API 服务入口
- `cmd/agent`: 远程集群 agent 与本地 runner 入口
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
- `web/src/theme/app-theme.ts`: 共享主题 token 与 CSS 变量基线

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
├── docs/                # Docusaurus 文档站点
├── internal/            # 后端分层实现
├── migrations/          # SQL 初始化与迁移
├── web/                 # 当前活跃前端应用
├── AGENTS.md            # 工程规范与仓库记忆
├── chart/               # Helm Chart
├── Dockerfile           # 单项目镜像构建入口
├── Makefile             # 常用开发、构建、部署命令
├── deployment.yaml      # 原生 Kubernetes 清单
└── docker-compose.yaml  # kubecrux 与 PostgreSQL 的本地 compose 入口
```

## 当前能力范围

当前产品重点包括：

- 平台管理：集群、节点、命名空间、工作负载、网络、存储、CRD、Helm
- 应用交付：应用、应用服务与容器、构建模板、工作流模板、发布包、执行任务、执行日志与产物、审批策略、发布、仓库连接
- 可观测性：监控、告警、通知策略、自愈策略、值班路由、排班、升级策略、事件
- AI 工作台：会话式调查、根因、性能、链路与巡检复盘分析、巡检自动化、MCP 数据源、工具与技能注册、分析模板、自动化策略和 Agent Runtime provider 选择
- 虚拟化：KubeVirt 与 Proxmox VE 连接、虚拟机、镜像、规格、控制台与指标、操作和同步
- Docker 工作台：主机资产、通过虚拟化适配器快速创建主机、Compose 项目、单容器启动、服务、端口、模板、操作记录与 agent runner 回调
- 控制平面：用户、角色、用户组、策略、菜单、公告、审计日志、设置

## 当前工作台入口

- k8s工作台：`/`、`/clusters`、`/workloads/**`、`/network/**`、`/storage/**`、`/extensions/**`、`/helm/**`
- 交付工作台：`/applications`、`/application-management`、`/build-templates`、`/delivery/release-bundles`、`/delivery/execution-tasks`、`/delivery/approval-policies`、`/workflow-templates`、`/release-board`、`/registries`
- 监控工作台：`/monitoring-workbench/**`
- AI 工作台：`/ai-workbench`、`/ai-workbench/chat`、`/ai-workbench/root-cause`、`/ai-workbench/performance`、`/ai-workbench/inspection`、`/ai-workbench/tool-settings`、`/ai-workbench/model-settings`
- 虚拟化工作台：`/virtualization/**`
- Docker 工作台：`/docker/**`
- 系统、访问控制与设置：`/system/**`、`/access/**`、`/settings/**`

`/api/v1/modules` 会根据 `configs/config.yaml` 中的 `modules.*.enabled` 返回模块状态。路由可见性、后端可见菜单与后端权限校验是三道不同门槛，必须保持一致。

## 快速开始

### 环境要求

- Go 1.23+
- Node.js 20+
- Docker 与 Docker Compose，用于本地基础设施或部署验证
- PostgreSQL 16，用于本地后端开发

### 1. 初始化本地开发依赖

```bash
make init
```

这会执行 `go mod tidy`、安装 `web` 和 `docs` 的 npm 依赖，然后通过根目录 `docker-compose.yaml` 启动 `pgsql` 容器并等待 PostgreSQL 就绪。
同时会启动一个本地 `k3s server` 调试集群，把 kubeconfig 写到 `./.dev/k3s/kubeconfig.yaml`，默认开发配置会把它注册为 `local-k3s`。如果用于 KubeVirt 实验，底层 Linux 节点仍需暴露 `/dev/kvm`；Proxmox VE 可以作为 KubeVirt VM 或外部宿主机通过 API 接入，但不能作为 k3s 内的普通 Pod/workload 部署。

如需在 KubeVirt 内运行 Proxmox VE 实验 VM：

```bash
make init-pve-vm
```

安装完成后通过 VNC 控制台完成 PVE ISO 安装，再执行 `make pve-vm-boot-root` 切回根盘启动。PVE API 默认暴露为 `https://127.0.0.1:8006`。

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
make init-cluster-kubevirt
make init-kubevirt
make init-cdi
make deploy-pve-mock
make init-pve-vm
```

## 工程规范

- 后端 transport 层保持轻量。Handler 只负责解析、绑定和错误映射；应用服务负责编排、权限、作用域、审计、操作日志和视图模型组装。
- 不要在 API handler 中直接执行构建、发布、Docker、Compose 或虚拟机控制命令。持久化工作应排入 execution、Docker 或 virtualization operation，再通过 runner claim、status、callback、cancel、retry 路径完成。
- 前端实现只进入 `web`。`old_web` 和 `web_pro_backup` 仅作参考。使用原生 `antd` 与 `@ant-design/icons`，不要重新引入 Semi Design 或第二套设计系统。
- 路由变更必须同步更新 `web/src/routes/index.tsx`、`web/src/routes/meta.ts`，必要时同步权限目录或后端菜单种子，并补充相关测试。
- 权限可见性不等于授权。前端可以隐藏不可用按钮，但后端服务必须执行显式 permission key 校验。
- 平台 API 返回 kubecrux 聚合 DTO，不直接返回原始 Kubernetes 对象，YAML 或明确透传接口除外。空 namespace 表示 namespaced 资源的全命名空间聚合；集群级资源应忽略 namespace 过滤。
- 优先后端聚合与 informer/cache 读取，避免浏览器按 namespace 扇出或反复 live query。
- AI 调查以 `/ai-workbench` 作为会话优先的规范入口。旧的 `/ai-observe/**`、`/chat` 与历史 AI 工作台路径只应作为兼容跳转保留。
- AI Agent Runtime 必须让页面、自动化策略和业务模块只依赖 kubecrux provider/capability/tool/skill 合约。Hermes 只是 claim/callback API 背后的 provider runner，agent 输出必须转换回 `AnalysisArtifact`。
- Docker Engine 与 Compose 执行属于 agent runner 职责。API 只持久化期望状态和操作记录，并暴露 token 保护的 claim、runner-status 与 callback 路径。
- KubeVirt 与 PVE 实验需要真实虚拟化运行时。macOS Docker Desktop 可验证控制面路径，但不能代表生产级 KVM 环境。

## 常见问题

- 只加菜单但没有同步 permission key、后端种子菜单和路由元数据，会导致导航不一致。
- 浏览器按 namespace 发起多次请求通常说明缺少或应扩展后端聚合接口。
- 不能把模块可见性当成安全边界；禁用模块、菜单可见性和权限校验解决的是不同问题。
- 直接返回原始 Kubernetes 对象会让前端变脆，并把基础设施 schema 泄漏成平台合约。
- execution task 已完成但 build、release 或 Docker 业务记录仍停在 `queued`，会产生状态分裂；callback handler 必须回填关联记录。
- 新增 AI 工具、skills 或外部 agent provider 时缺少预算、超时、权限、脱敏和 callback 边界，会导致会话分析不可控。
- 直接编辑生成的 docs build 输出或前端生成物通常不是正确目标，应修改源文件。

## 部署

主镜像与本地 compose 入口现在直接位于仓库根目录。

- [Dockerfile](./Dockerfile): 单项目镜像构建入口，内嵌 API、SPA 与 docs
- [docker-compose.yaml](./docker-compose.yaml): 本地完整栈启动文件
- [configs/config.yaml](./configs/config.yaml): 本地开发与容器镜像默认使用的应用配置
- [deployment.yaml](./deployment.yaml): 原生 Kubernetes 清单
- [chart](./chart): Helm Chart

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
helm lint chart
```

## 文档

主要文档位于 [docs](./docs/)。

- [工程规范](./AGENTS.md)
- [架构总览](./docs/architecture/index.md)
- [登录与身份链路](./docs/architecture/login-and-identity.md)
- [应用交付](./docs/architecture/application-delivery.md)
- [AI Copilot](./docs/architecture/ai-copilot.md)
- [权限体系](./docs/architecture/authorization.md)
- [监控与告警](./docs/architecture/monitoring-and-alerting.md)
- [配置说明](./docs/operations/configuration.md)
- [部署说明](./docs/operations/deployment.md)
- [Agent Runtime](./docs/operations/agent-runtime.md)
- [虚拟化实验手册](./docs/operations/virtualization-lab-runbook.md)
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
