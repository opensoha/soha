<h1 align="center">soha</h1>

<p align="center">
  <strong>Soha，解决中小企业运维难题。</strong>
</p>

<p align="center">
  让人手有限的团队，更轻松地管理、维护和保障业务运行。
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.27.4-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://react.dev/"><img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=111111"></a>
  <a href="https://ant.design/"><img alt="Ant Design" src="https://img.shields.io/badge/Ant%20Design-6-1677FF?logo=antdesign&logoColor=white"></a>
  <a href="https://kubernetes.io/"><img alt="Kubernetes" src="https://img.shields.io/badge/Kubernetes-client--go-326CE5?logo=kubernetes&logoColor=white"></a>
  <a href="https://www.postgresql.org/"><img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-18.4-4169E1?logo=postgresql&logoColor=white"></a>
  <a href="https://docs.opensoha.com/"><img alt="Docs" src="https://img.shields.io/badge/Docs-Nextra-111111?logo=nextdotjs&logoColor=white"></a>
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

Soha 面向有自建业务系统、运维人手有限的中小企业，将资源接入、日常维护、应用交付和故障处理组织在同一个权限感知的运维工作台中。本仓库是 Core、Web、Contracts、CLI、Agent、Connectors 与 Skills 的唯一开发源；服务端内嵌同一源码提交构建的 Web 控制台。

Soha 围绕实际运维问题持续演进，覆盖内网与身份接入、Kubernetes、虚拟化、Docker、应用交付、可观测性和 AI 辅助调查。各模块的支持平台、部署条件及实验性边界以[公开文档站](https://docs.opensoha.com/)为准。

## 为什么选择 soha

- **一个运行时**：需要紧凑部署时，可以用单个应用容器同时交付 API 和内嵌控制台。
- **围绕日常运维组织操作**：从资源与应用进入维护、发布和问题调查，保留作用域动作、事件、指标、日志与持久化操作记录。
- **权限感知的设计**：菜单、路由、按钮、API 授权、审计日志与作用域授权是相互对齐但边界清晰的控制点。
- **Agent-ready 架构**：远程集群、AI provider、Docker 操作和持久化执行任务均通过 token 保护的 runner claim/callback 路径运行。
- **按实际需要持续扩展**：内网接入、资源、交付、可观测性和 AI 等能力共享同一模块化后端与前端壳层。

## 功能特性

| 领域 | Soha 提供的能力 |
| --- | --- |
| 内网与身份接入 | 应用目录、身份 Provider、Outpost、站点与网络资源、访问策略，以及通过 App 执行的受管 VPN 和代理接入。 |
| 平台运维 | 多集群资产、节点、命名空间、工作负载、网络、存储、CRD、Helm、YAML、日志、事件、指标与操作入口。 |
| 应用交付 | 应用、服务、容器、构建模板、工作流模板、发布包、执行任务、审批、发布、镜像仓库与交付记录。 |
| 可观测性 | 监控总览、告警资产、告警事件、通知策略、自愈策略、值班路由、排班、升级策略与事件流。 |
| AI 工作台 | 会话式聊天、根因分析、性能分析、巡检复盘、MCP 证据采集、工具集、技能与 provider 执行。 |
| Agent Runtime | 远程集群模式、runner claim/callback API、执行心跳、任务取消、Docker 主机 runtime 代理端点与 provider 无关的 AI 执行。 |
| 虚拟化 | KubeVirt 与 Proxmox VE 连接、虚拟机生命周期、镜像和规格目录、控制台、指标、操作与同步任务。 |
| Docker 工作台 | Docker 主机资产、Compose 项目、容器管理、服务、端口映射、模板、运行时日志、Shell 访问与卷文件浏览。 |
| 访问与系统 | 用户、角色、组织、策略、作用域授权、菜单、设置、公告、审计日志与操作日志。 |

## 架构

Soha 采用模块化单体后端与基于路由注册的前端壳层：

```text
浏览器控制台 (React 18 + Vite + Ant Design)
        |
        v
Gin API Server (cmd/server)
  ├── internal/api            # 领域路由、handler、中间件与响应封装
  ├── internal/application    # 业务编排、授权、作用域处理与审计
  ├── internal/policy         # RBAC、ABAC 与作用域计算
  ├── internal/infrastructure # 配置、数据库、K8s、Agent、MCP 与日志
  └── internal/repository     # PostgreSQL 持久化访问层
```

- **同仓多组件**：Core（`github.com/opensoha/soha`）位于仓库根目录；`contracts/`、`web/`、`cli/`、`agent/`、`connectors/`、`skills/` 均在本仓库维护，按组件独立打标发布（`contracts/v*`、`cli/v*`、`agent/v*`、`connectors/v*`、`skills/v*`，Core 使用 `v*`）。原六个独立组件仓库仅作为只读历史归档保留，新 Issue 与 PR 统一提交至本仓库。
- **Web 运行模式**：支持 `embed`（发行默认，产物暂存于 `internal/staticassets/web/dist`）、`dir` 和 `proxy`。
- **文档站**：源码在 [`opensoha/soha-docs`](https://github.com/opensoha/soha-docs)，独立发布于 [`https://docs.opensoha.com/`](https://docs.opensoha.com/)（服务端 `/docs/` 默认重定向至该地址）。

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 后端 | Go 1.27.4、Gin、PostgreSQL 18.4（`pgvector` 0.8.5）、Kubernetes `client-go` |
| 前端 | React 18、TypeScript 5、Vite 6、React Router 6、TanStack Query 5、Zustand 5、Ant Design 6、Tailwind CSS 4 |
| 文档 | Next.js 16 与 Nextra 4（`opensoha/soha-docs`） |
| 打包部署 | Docker、Docker Compose、原生 Kubernetes / Kustomize、原生 Linux 安装包；Helm Chart 在 `opensoha/soha-helm` 维护 |

## 目录结构

```text
.
├── contracts/            # 公开契约 Schema 与生成的 Go/TypeScript SDK
├── web/                  # React 控制台源码
├── cli/                  # 独立 CLI 与 npm launcher
├── agent/                # 独立 Agent、Outpost 与 DSH 运行时
├── connectors/           # 官方连接器包
├── skills/               # 官方运行时技能、预设与配置目录
├── cmd/                  # Core、network-control、ingest 与 gateway 入口
├── configs/              # 后端配置模板
├── internal/             # 后端分层、领域模块与内嵌 Web 暂存产物
├── migrations/           # PostgreSQL 初始化与迁移 SQL
├── deploy/               # Docker、Compose、原生 Kubernetes 与实验资产
├── Makefile              # 本地开发、构建与验证命令
└── .agents/skills/       # Core 与各源码组件共用的开发协作技能
```

## 快速开始

### 1. 使用 Docker Compose 快速体验

使用已发布的 Core 镜像（同时提供 API 与内嵌 Web 控制台）在本机启动体验栈：

```bash
git clone --branch v0.1.13 --depth 1 https://github.com/opensoha/soha.git
cd soha
SOHA_IMAGE=ghcr.io/opensoha/soha:v0.1.13 \
  docker compose -f deploy/docker-compose.yaml up -d --no-build --wait soha
```

打开 `http://localhost:8080/`，使用默认本地初始化账号 `opensoha` / `opensoha` 登录。

> **安全提示：** 本地进程、Docker、Kubernetes 与 Helm 部署默认共用标准初始凭据（`pgsql`、`opensoha`）及四项公开默认系统密钥，以便开箱启动。**对回环地址以外开放访问前，必须替换初始凭据与全部四项系统密钥。** 详见[第一次部署](https://docs.opensoha.com/zh/docs/tutorials/first-deploy)与[配置指南](https://docs.opensoha.com/zh/docs/operations/configuration)。

### 2. 本地源码开发

环境要求：**Go 1.27.4+**、**Node.js 22.23.4**（Agent DSH 使用 `agent/runtimes/dsh/` 下独立的 Node.js 24 运行时），以及带 `pgvector` 0.8.5 的 **PostgreSQL 18.4**（或使用 Docker 启动本机 PostgreSQL）。

```bash
cp configs/config.example.yaml configs/config.yaml
npm ci
make init          # 或：make init DEV_DB_MODE=local（拉起 Compose PostgreSQL）
make               # 同时启动 Go API (:8080) 与 Vite 前端 (:5173)
```

- 控制台：`http://localhost:5173` · API：`http://localhost:8080`
- 分别启动：`make dev-api` / `make dev-web`（启动 Agent 运行时：`cd agent && go run ./cmd/agent`）
- 完整开发环境与数据库说明：[本地开发指南](https://docs.opensoha.com/zh/docs/development/local-development)

### 常用命令

```bash
make                        # 同时启动本地 API 与 Web 开发服务
make build                  # 构建 Web、暂存内嵌产物并编译服务端二进制
make test                   # 运行 Core Go 单元测试
make test-cli               # 运行 CLI 测试
make test-agent             # 运行 Agent 测试
make check-connectors       # 校验 Connectors 包与 manifest
make check-skills           # 校验运行时 Skills 与目录资产
make deploy-image           # 从当前工作树构建本地 Core 镜像
```

## 部署

完整的安装、升级、密钥轮换与多进程运行时拓扑说明统一维护在[公开部署文档](https://docs.opensoha.com/zh/docs/operations/deployment)，不在本 README 重复维护具体参数：

| 部署方式 | 适用场景 | 文档与资产入口 |
| --- | --- | --- |
| **Docker Compose** | 本地开发与单机试跑 | [第一次部署](https://docs.opensoha.com/zh/docs/tutorials/first-deploy) · [deploy/docker-compose.yaml](./deploy/docker-compose.yaml) |
| **单容器 `docker run`** | 已有外部 PostgreSQL 18 的单容器部署 | [部署指南](https://docs.opensoha.com/zh/docs/operations/deployment) · [deploy/Dockerfile](./deploy/Dockerfile) |
| **Helm Chart** *（推荐）* | 生产 Kubernetes 主交付路径（`soha`、`soha-agent`、`soha-operator`） | [部署指南](https://docs.opensoha.com/zh/docs/operations/deployment) · [`opensoha/soha-helm`](https://github.com/opensoha/soha-helm) |
| **原生 Kubernetes / Kustomize** | 不引入 Helm 时的轻量原生清单定制 | [部署指南](https://docs.opensoha.com/zh/docs/operations/deployment) · [deploy/kustomization.yaml](./deploy/kustomization.yaml) |
| **原生 Linux 安装包** | 含二进制与迁移 SQL 的归档包（`soha-linux-amd64.tar.gz`） | [deploy/native/README.md](./deploy/native/README.md) |
| **Agent、Outpost 与 DSH** | 远程集群 Agent、Identity Outpost、Chat/Heya DSH 切换 | [Agent Runtime](https://docs.opensoha.com/zh/docs/operations/agent-runtime) · [deploy/dsh-hindsight/README-cn.md](./deploy/dsh-hindsight/README-cn.md) |
| **内网接入运行时** | `network-control`、`ingest`、WireGuard `network-gateway`、FreeRADIUS | [网络访问工作台运维指南](https://docs.opensoha.com/zh/docs/operations/network-access) · [deploy/network-runtime.yaml](./deploy/network-runtime.yaml) |

内部实验网拓扑与共享集群远程联调脚本随部署目录维护，见 [deploy/network-test/README.md](./deploy/network-test/README.md) 与 [deploy/remote-dev/README.md](./deploy/remote-dev/README.md)。

## 文档

- [发布文档站](https://docs.opensoha.com/)（[文档源码](https://github.com/opensoha/soha-docs)）
- [配置与系统密钥说明](https://docs.opensoha.com/zh/docs/operations/configuration)
- [后端工程规则](./.agents/skills/soha-backend/SKILL.md)
- [部署规则](./.agents/skills/soha-deploy/SKILL.md)

## 开发原则

- **轻量 Handler、富应用服务**：handler 只负责协议解析与响应封装；业务编排、授权、作用域语义、审计与视图模型归属 `internal/application`。
- **启动与路由按域拆分**：新增路由放入 `internal/api/routes` 领域文件，新增装配放入 `internal/bootstrap` 关注点文件。
- **长耗时操作任务化**：构建、发布、Docker、Compose、虚拟机控制与 provider 执行统一走持久化任务与 callback 路径。
- **契约先行**：公开 Schema 与生成 SDK 统一维护在 `contracts/`；除 YAML 或显式透传接口外，平台 API 返回 Soha DTO。
- **权限边界分层对齐**：模块可见性、菜单可见性与后端 API 授权彼此独立、同步演进。
- **禁止手改生成物**：修改源 Schema 或前端源码后，通过生成与构建脚本（如 `npm run build:web`）更新产物。

## 贡献

欢迎提交 Issue 与 Pull Request。进行较大后端或部署改动前，请先阅读[后端](./.agents/skills/soha-backend/SKILL.md)与[部署](./.agents/skills/soha-deploy/SKILL.md)规则。

提交前常用验证命令：

```bash
npm ci
make test
npm run typecheck --workspace web
npm run build:web
npm run check:internal-consumers
```

## 项目状态

Soha 处于持续演进中。平台运维、应用交付、可观测性、AI、内网接入、虚拟化与 Docker 工作台共同迭代，各模块成熟度与部署前提以对应文档说明为准。

## 许可

Soha 采用 Apache License 2.0 许可。完整许可文本见 [LICENSE](./LICENSE)。
