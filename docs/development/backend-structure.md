# 后端结构

```text
.
  cmd/server
  cmd/agent
  configs/config.yaml
  internal/api
  internal/application
  internal/bootstrap
  internal/domain
  internal/infrastructure
  internal/policy
  internal/platform
  internal/repository
  migrations
```

## 模块职责

- `api`: HTTP 传输层、middleware、请求解析和响应封装。
- `application`: 用例编排、授权、作用域语义、审计、操作日志和面向前端的 view model。
- `bootstrap`: 依赖图装配、数据库迁移、内置种子和启动生命周期。
- `domain`: 领域契约和平台 view model。
- `infrastructure`: config、logger、PostgreSQL、Kubernetes、informer、agent、Swagger、MCP 和外部系统 client。
- `policy`: RBAC、ABAC 和 scope 计算。
- `repository`: 审计、业务记录、运行时记录等持久化访问。

## 模块化单体约定

后端保持单仓库、单 `go.mod`、多 `cmd` 入口的模块化单体形态。`cmd/server` 是当前管理控制面入口，`cmd/agent` 是远程集群 agent 和 runner 入口。未来如果增加内网安全工作台或设备上报能力，优先新增同仓库入口，例如 `cmd/security-ingest` 或 `cmd/security-worker`，而不是把高频设备上报直接塞进 `cmd/server` 的 Gin handler。

新增入口必须复用 `internal/bootstrap`、`internal/infrastructure`、`internal/repository` 和 `internal/application` 中的稳定能力。只有入口生命周期、监听地址、认证方式、队列/worker 循环等运行形态差异应该留在 `cmd/**` 或专门的 bootstrap 文件中。

## 路由注册约定

`internal/api/routes/router.go` 只负责 Gin engine、全局 middleware、基础兼容路径、静态资源和顶层 route group 装配。业务路由要落在同包的领域文件中，例如：

- `routes_public.go`: health、登录、公开 webhook、runner claim/callback 等无需用户会话但需要 token 或公开协议的入口。
- `routes_protected.go`: 受登录态保护的顶层编排函数。
- `routes_platform.go`: Kubernetes 平台管理资源。
- `routes_delivery.go`: 应用交付、执行任务、发布和制品。
- `routes_monitoring.go`: 监控、告警、通知和值班。
- `routes_runtime.go`: 虚拟化、Docker、AI workbench 等运行时工作台。
- `routes_governance.go`: system、access、settings、AI Gateway、audit/operation 等治理面。

新增领域不要继续膨胀 `router.go`。先新增 `register<Domain>Routes`，再从 `registerProtectedRoutes` 或 `registerPublicRoutes` 中接入。模块开关、公开 callback 路径、用户登录态路径要保持边界清晰。

## Go 热点文件拆分约定

大文件优化优先做同包机械拆分，再做行为调整。拆分时应保持 receiver、私有 helper、DTO、错误语义和 audit/operation 记录不变。

- 平台 handler REST 方法按资源域拆分：`platform_inventory.go`、`platform_workloads.go`、`platform_configuration.go`、`platform_network.go`、`platform_storage.go`、`platform_rbac.go`、`platform_crd_helm.go`、`platform_generic.go` 和 `platform_observability.go`。WebSocket 流处理归 `platform_streams.go`，共享的 `websocketStreamSession` lifecycle helper 也保留在这里。
- 平台资源 application service 按资源族拆分：`pods.go`/`pods_helpers.go`、`workloads.go`、`configuration.go`、`rbac.go`、`network.go`、`storage.go`、`crd.go`、`events.go` 和 `resource_yaml.go`。共享授权/审计 helper 归 `common.go`，direct Kubernetes bundle/timeout helper 归 `direct_query.go`。
- 修改资源服务行为时保持现有资源族边界，并至少运行 `go test ./internal/application/resource`。机械移动和 agent/direct 行为调整不要混在同一 patch 中。
- 执行面状态机必须有测试保护。claim/callback/cancel/retry/timeout、callback token 轮换、terminal 状态 late callback、artifact 落库和 build/release 回填都属于执行面测试范围。
- AI Gateway 按行为域拆分：`manifest.go`、`tools.go`、`policies.go`、`rate_limit_budget.go`、`redaction.go`、`approval.go`、`tokens.go`、`audit.go` 和 `governance.go`。`service.go` 只保留 wiring、interface、constructor 和 setter。
- transport 行为发生变化时需要 handler 测试；纯文件移动可先依赖 package compile、全量 Go test 和路由注册对比。

## Bootstrap 和 Seed 约定

`internal/bootstrap/app.go` 保持顶层依赖装配。生命周期方法属于 `lifecycle.go`，跨模块窄适配器属于独立文件，例如 Docker 主机快速创建只通过 `docker_provisioner.go` 调用虚拟化服务。

数据库 bootstrap 也按关注点拆分。`database.go` 保留 seed 编排、角色/策略/用户/集群等通用持久化 helper；菜单 seed、模块禁用过滤、菜单角色绑定和内置菜单升级逻辑归 `database_menus.go`。未来增加领域菜单或权限时，应优先在对应 seed 文件中落位，并同步权限键、菜单可见性、前端 route metadata 和文档。

## 未来安全工作台边界

当前仓库尚未实现内网安全工作台业务。预留边界如下：

- `/api/v1/security/**`: 管理控制面 API，由 Soha web admin 使用。
- `/api/client/v1/**`: Wails 桌面端和 Flutter 移动端的客户端 API。
- `/api/ingest/v1/**`: 设备上报、心跳、审计证据和安全遥测 ingest API，适合由未来 `cmd/security-ingest` 承载。

Soha 负责软件库、设备资产与上报、策略、审计和控制面数据。FreeRADIUS、Fleet、mihomo 等应作为被管理或被集成的执行侧系统，不应成为 Soha 主进程的运行时核心。
