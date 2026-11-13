# Soha 源码仓库入口

## 规则入口与所有权

- 根目录负责 Go 核心控制平面、Server API 和原始 Docker/Kubernetes 部署；必须可脱离 `soha-cloud` 独立运行。Core 保持单 module 的模块化单体；`contracts/` 是独立 SDK module，`web/` 是控制台源码；`cli/`、`agent/`、`connectors/`、`skills/` 分别拥有独立客户端、运行时、连接器和技能产品。组件分别遵循其目录内的 AGENTS 与技能，不按菜单或代码行数拆服务。
- 在已确认的 OpenSoha 多仓工作区显式读取工作区根 `AGENTS.md` 一次，不按固定父级猜测隔离 worktree 的位置；独立克隆使用本仓规则，不要求补齐相邻仓库。技能链接只是发现入口，未自动加载时读取本仓真实路径。
- 后端实现或实质审查前读取 [soha-backend](.agents/skills/soha-backend/SKILL.md) 及本次相关参考；部署变更读取 [soha-deploy](.agents/skills/soha-deploy/SKILL.md)。只读查询按需定位，已读且未变化的内容可复用；交接后缺少关键约束时补读必要部分。
- 公开协议唯一源为 `contracts/`，Web 源码唯一入口为 `web/`；同仓使用根 `go.work`、npm workspace 与唯一根锁文件，不等待 SDK 公开发布。根 go.work 仅允许 Core/CLI/Agent 声明的精确 SDK 版本映射到 ./contracts，以本地源码解析依赖图；禁止其他 replacement。Core 消费同提交 Web 构建产物，不手改嵌入生成物。Core module identity 保持 `github.com/opensoha/soha`，SDK/CLI/Agent 分别使用其 `contracts`/`cli`/`agent` 子 module identity；源码验证不等于已发布依赖或分发资格。

## 变更契约

- 修改前确定所属业务模块、数据/状态所有者、调用者与必须保持的行为。工作台可以聚合能力，不复制底层模块的权限、生命周期或持久化规则；边界以技能、代码和架构测试共同核对。
- 维护任务不顺手修改全局权限、错误语义、共享默认值、公开契约或部署模式。确有必要时说明受影响消费者并扩大对应回归；不增加仅为未来产品准备的抽象。
- 删除、取消、重试、回调与资源清理先确认所有权、副作用、终态和失败恢复。修复一个错误不能通过隐藏关联记录、跳过鉴权或放宽断言完成。
- 当前实现证明现状，不自动证明符合有效规范；发现冲突需核对契约、测试和任务目标，记录采用依据，不能将历史计划当作新授权。

## 验证与交付

- Go 变更先验证受影响包；源码主入口为 `python3 scripts/source-workspace.py check-go` 后使用根 `go.work` 运行 `go test ./...`，SDK、CLI、Agent modules 分别验证，根 `go test ./...` 不覆盖嵌套模块。整洁性使用 `python3 scripts/source-workspace.py tidy`，不把未发布 SDK 当作内部开发的前置。对外声明依赖/发布验证仍使用 `GOWORK=off` 且禁止本地替换。架构、契约、依赖、安全、并发、部署或发布变更执行 [CI](.github/workflows/ci.yml) 适用完整门禁，保留 race、vet、复杂度、漏洞与镜像检查。
- npm 安装只从根 `npm ci` 开始；四个 workspace 为 `contracts`、`web`、`connectors`、`cli/npm/cli`，组件从根使用 `npm run <script> --workspace <组件>`。Agent DSH 独立 Node 24 runtime 保留自己的锁文件；不加入根 npm workspace。`npm run build:web` 生成并校验 SDK、构建 Web、核对字节后嵌入；不得新增子目录锁文件或用工作区链接冒充 registry 分发。
- 跨仓构建记录实际 Core、Web、Contracts 提交或产物摘要；固定组合验证和最新分支集成检查分别报告。写入版本号不等于组合已经通过验收。
- 应用/集群删除使用[独立 PostgreSQL 集成检查](.github/workflows/deletion-integration.yml)，要求目标测试实际运行、通过且无跳过子测试；本地仅连接可丢弃的隔离测试库，不使用开发或生产业务数据库。
- 数据库、任务或外部运行时集成测试被跳过时明确报告。普通 `go test` 成功不证明条件性测试已经执行；要求验证该链路时必须准备隔离环境并确认目标测试实际运行，不使用生产数据库。
- 按仓库报告变更、实际命令及对应代码版本，区分通过、失败、跳过、未运行和环境缺失。成功结果仅在相关代码、依赖与环境未变化时复用；不得重写基线或降低门禁来掩盖失败。
- 文档/技能修改检查内容、链接与差异，不机械构建应用。保留用户未提交改动，仅提交任务所属文件；未请求时不合并 PR、不发布。

- 真实数据库、内部集成与状态机验收读取 [soha-backend-testing](.agents/skills/soha-backend-testing/SKILL.md)；跨层权限、删除、runner、Secret、模型外发和 CI/OCR 审查按风险读取 [soha-security](.agents/skills/soha-security/SKILL.md)。生产实现规则仍归 soha-backend。

## Graphify

- 跨模块依赖、共享能力影响、模块迁移或跨仓任务，先查询已有图谱并核对源码；在已确认的多仓工作区按实际工作区的 `docs/guides/graphify-development/runbook.md` 检查新鲜度、查询和刷新。
- 结构变更稳定后显式刷新受影响仓库及跨仓图；普通小修改、文案和局部样式不自动建图。刷新失败先诊断，不默认使用 `--force`。
- 独立克隆时使用本仓 `graphify-out/graph.json` 与原生 Graphify 命令；缺少工具或图谱时直接查源码并说明缺口，不要求补齐相邻仓库。
