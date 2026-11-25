# Soha 源码仓库入口

## 规则入口与所有权

- 根目录负责 Go 核心控制平面、Server API 和原始 Docker/Kubernetes 部署；必须可脱离 `soha-cloud` 独立运行。Core 本身保持单 module 的模块化单体；`contracts/` 是独立 SDK module，`web/` 是控制台源码；`cli/`、`agent/`、`connectors/`、`skills/` 分别拥有独立客户端、运行时、连接器和技能产品。组件分别遵循其目录内的 AGENTS 与技能，不按菜单或代码行数拆服务。主仓是本 Git 仓库的根目录；代码路径默认相对所属组件，安装、共享锁文件和活动 CI 以主仓为基准。
- 在已确认的 OpenSoha 多仓工作区显式读取工作区根 `AGENTS.md` 一次，不按固定父级猜测隔离 worktree 的位置；独立克隆整个 soha 主仓即可读取全部组件规则，不要求补齐外部仓库。技能链接只是发现入口，未自动加载时读取本仓真实路径。
- 后端实现或实质审查前读取 [soha-backend](.agents/skills/soha-backend/SKILL.md) 及本次相关参考；部署变更读取 [soha-deploy](.agents/skills/soha-deploy/SKILL.md)。只读查询按需定位，已读且未变化的内容可复用；交接后缺少关键约束时补读必要部分。
- 公开协议唯一源为 `contracts/`，Web 源码唯一入口为 `web/`；同仓使用根 `go.work`、npm workspace 与唯一根锁文件，不等待 SDK 公开发布。根 go.work 仅允许 Core/CLI/Agent 声明的精确 SDK 版本映射到 ./contracts，以本地源码解析依赖图；禁止其他 replacement。Core 消费同提交 Web 构建产物，不手改嵌入生成物。Core module identity 保持 `github.com/opensoha/soha`，SDK/CLI/Agent 分别使用其 `contracts`/`cli`/`agent` 子 module identity；源码验证不等于已发布依赖或分发资格。

## 组件规则入口

| 组件 | 规则 | 开发技能真实源 |
| --- | --- | --- |
| Contracts | [contracts/AGENTS.md](contracts/AGENTS.md) | [soha-contracts](.agents/skills/soha-contracts/SKILL.md) |
| Web | [web/AGENTS.md](web/AGENTS.md) | [frontend](.agents/skills/soha-frontend/SKILL.md)、[design](.agents/skills/soha-design/SKILL.md)、[testing](.agents/skills/soha-web-testing/SKILL.md) |
| CLI | [cli/AGENTS.md](cli/AGENTS.md) | [soha-cli](.agents/skills/soha-cli/SKILL.md) |
| Agent | [agent/AGENTS.md](agent/AGENTS.md) | [soha-agent](.agents/skills/soha-agent/SKILL.md) |
| Skills 产品资产 | [skills/AGENTS.md](skills/AGENTS.md) | [soha-skills](.agents/skills/soha-skills/SKILL.md) |
| Connectors | [connectors/AGENTS.md](connectors/AGENTS.md) | [soha-connectors](.agents/skills/soha-connectors/SKILL.md) |

全部开发协作技能统一维护在根 `.agents/skills/`；组件保留自己的 AGENTS 和模块边界，不维护技能副本，也不将开发技能混入产品资产 `skills/skills/` 或 `skills/agent-skills/`。App、Operator、Cloud、Helm、公开文档站及内部 docs 仍是外部仓库，按公开契约、产物或明确的工作区协作入口集成。

活动工作流只在根 `.github/workflows/`，审查和图谱共享配置分别在根 `.opencodereview/`、`.graphifyignore`。组件原工作流及重复配置保留在 Git 历史，不作为当前入口。

## 变更契约

- 修改前确定所属业务模块、数据/状态所有者、调用者与必须保持的行为。工作台可以聚合能力，不复制底层模块的权限、生命周期或持久化规则；边界以技能、代码和架构测试共同核对。
- 维护任务不顺手修改全局权限、错误语义、共享默认值、公开契约或部署模式。确有必要时说明受影响消费者并扩大对应回归；不增加仅为未来产品准备的抽象。
- 删除、取消、重试、回调与资源清理先确认所有权、副作用、终态和失败恢复。修复一个错误不能通过隐藏关联记录、跳过鉴权或放宽断言完成。
- 当前实现证明现状，不自动证明符合有效规范；发现冲突需核对契约、测试和任务目标，记录采用依据，不能将历史计划当作新授权。

## 验证与交付

- 本地对受影响的包或用例运行定向回归。Go 源码使用根 `go.work`，模块归属检查用 `python3 scripts/source-workspace.py check-go`；外部声明依赖另用 `GOWORK=off`。完整测试、依赖审计、race、vet、漏洞扫描与镜像矩阵由 [GitHub Actions](.github/workflows/ci.yml) 按变更路径执行。需要复现某项失败时在本地运行对应检查。
- npm 安装只从根 `npm ci` 开始；五个 workspace 为 `contracts`、`web`、`connectors`、`cli/npm/cli`、`plugin-sdk/ui`，组件从根使用 `npm run <script> --workspace <组件>`。Agent DSH 独立 Node 24 runtime 保留自己的锁文件；不加入根 npm workspace。`npm run build:web` 生成并校验 SDK、构建 Web、核对字节后嵌入；不得新增子目录锁文件或用工作区链接冒充 registry 分发。
- 发布所有者与标签入口以 [.github/release-components.json](.github/release-components.json) 为准；组件工作流只使用本仓精确 tag/SHA，公开依赖验证与同源码验证分开。写入前按发布守卫核对 `.github/legacy-retirement.json` 的封存回执及 `.github/release-components.json` 中的哈希、该精确 SHA 的 main CI 和受限 environment/owner 开关；不要求已退役旧仓的 API 继续可读，也不能将 404 当作交接证明。只有 Core 管理 GitHub latest。已发布版本、附件和镜像版本标签不得覆盖，SDK staged 恢复复用原产物，不能重新打包替换。
- 主仓源码构建记录共同的 Root SHA 及各组件产物摘要；外部消费者另记其 SHA/产物。固定兼容组合和同提交源码检查分别报告。写入版本号不等于组合已经通过验收。
- 应用/集群删除使用[独立 PostgreSQL 集成检查](.github/workflows/deletion-integration.yml)，要求目标测试实际运行、通过且无跳过子测试；本地仅连接可丢弃的隔离测试库，不使用开发或生产业务数据库。
- 数据库、任务或外部运行时集成测试被跳过时明确报告。普通 `go test` 成功不证明条件性测试已经执行；要求验证该链路时必须准备隔离环境并确认目标测试实际运行，不使用生产数据库。
- 按仓库报告变更、实际命令及对应代码版本，区分通过、失败、跳过、未运行和环境缺失。成功结果仅在相关代码、依赖与环境未变化时复用；不得重写基线或降低门禁来掩盖失败。
- 文档/技能修改检查内容、链接与差异，不机械构建应用。保留用户未提交改动，仅提交任务所属文件；未请求时不合并 PR、不发布。

- 真实数据库、内部集成与状态机验收读取 [soha-backend-testing](.agents/skills/soha-backend-testing/SKILL.md)；跨层权限、删除、runner、Secret、模型外发和 CI/OCR 审查按风险读取 [soha-security](.agents/skills/soha-security/SKILL.md)。生产实现规则仍归 soha-backend。

## Graphify

- 跨模块依赖、共享能力影响、模块迁移或跨仓任务，先查询已有图谱并核对源码；在已确认的多仓工作区按实际工作区的 `docs/guides/graphify-development/runbook.md` 检查新鲜度、查询和刷新。
- 结构变更稳定后显式刷新受影响仓库及跨仓图；普通小修改、文案和局部样式不自动建图。刷新失败先诊断，不默认使用 `--force`。
- 独立克隆时使用本仓 `graphify-out/graph.json` 与原生 Graphify 命令；缺少工具或图谱时直接查源码并说明缺口，不要求补齐相邻仓库。
