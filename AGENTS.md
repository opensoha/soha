# Soha Web 仓库入口

## 所有权与规则加载

- 本仓负责 React/Vite/Ant Design 控制台源码；公开协议以 `soha-contracts` 为源，API 调用遵循现有 api-client 和 feature 所有权边界。
- 在已确认的 OpenSoha 多仓工作区中读取 `../AGENTS.md` 一次；独立克隆使用本仓规则，不要求相邻仓库或工作区规划工具。根技能符号链接只是发现入口，未自动加载时直接读取本仓真实源。
- 实现或实质审查前读取 [soha-frontend](.agents/skills/soha-frontend/SKILL.md)；UI 工作读取 [产品体验规范](.agents/skills/soha-frontend/references/product-experience.md) 与 [主题规范](.agents/skills/soha-frontend/references/theme-system.md) 的相关部分；路由、数据、状态、CSS 所有权或共享边界变化读取 [架构规范](.agents/skills/soha-frontend/references/architecture-development.md)。只读查询按需定位，不遍历所有规范。
- 同一任务中已读且未变化的内容可以复用；新任务、交接后缺少关键约束或文件变化时补读必要部分。读取技能不授权安装、发布、修改其他仓库或恢复历史任务。

## 产品与变更边界

- 保留 Antd，统一设计语言与交互语义，不强制相同管理模板。产品工作区与紧凑管理页共存，品牌和导航尺度不随单页切换。
- 状态变化、操作反馈和关系探索按 [交互规范](.agents/skills/soha-design/references/interaction-feedback.md)实施：区分资源/任务/会话、受理/结果/观察，明确刷新与同步职责，保留数据和上下文；关系浏览不冒充流量或写操作。具体设计与数据所有权留在现有 skills，AGENTS 不维护第二份规则正文。
- 维护任务保持局部；明确授权的产品重设计才调整布局、信息层级与组合。基础规则和主题校准不自动启动页面重设计、指定业务样板或恢复已撤回方案。
- 当前实现用于核实既有行为，不自动成为设计样板。参考必须指出实际文件与适用部分；未经认可的图像或历史测试通过不等于用户认可。有效产品规范与既有实现冲突时记录偏差，不静默采用更宽松解释。
- 普通业务表格复用 `AdminTable` 与现有查询、工具栏、列能力；`ManagementDataPage` 仅在页面任务适合时使用。不得另建通用包装层或用局部 CSS 绕过共享密度、表头与状态规范。
- 新增或修改表格前确认分页/排序/筛选由服务端还是完整本地数据负责，稳定 `rowKey`、总数来源、选择范围、权限与行操作；内嵌表格和游标分页保留真实语义。不能以“使用了 AdminTable”替代接入验证。
- 纯视觉修改默认保留 API/DTO、Query/mutation、权限、路由及菜单身份、确认与专业引擎生命周期。修改共享默认值、主题、错误反馈或状态语义前检查直接消费者并扩大回归；不为一个页面改变全局行为。

## 验证与完成

- 命令和版本以 [package.json](package.json) 与 [CI](.github/workflows/ci.yml) 为准。局部维护选择相关组件测试、Lint/格式及必要浏览器检查；新增或重构页面另验证数据接入、权限、加载/空/错状态、类型及适用路由行为。
- 开发/UI 实现者负责最小有效自测；复杂权限、异步状态和跨模块流程在实现前或早期明确成功、失败及边界场景，按需由 `qa_designer` 协助。独立测试与只读审查在约定批次完成、准备正式提交/合入或用户明确要求时进行，不因每次局部调整或预览启动全套收尾。
- 主入口为 `npm run lint`、`npm run typecheck`、`npm test`、`npm run build`，这是入口索引，不是每次小改的执行清单；定向命令和浏览器资源限制见 [测试入口](docs/testing.md)。全量回归按实际影响及必需门禁选择，提交本身不触发全量；路由、共享边界、依赖或发布修改执行适用完整门禁，不用定向测试替代 CI 必需检查。
- 视觉完成需要实际页面与交互状态验证，构建和类型成功不能替代设计验收。共享组件通过也不代表页面接入通过；不自行代签用户认可。
- 按仓库记录本次修改、验证命令与对应提交/差异，区分通过、失败、跳过和未运行。成功结果只在相关代码、依赖及环境未变化时复用；不得为了变绿放宽断言、更新违规基线或隐藏失败。
- 文档和技能改动只检查元数据、链接、内容一致性及差异，不机械构建应用。保留用户未提交改动，不手改 `dist/` 等生成物；仅提交任务所属变更。

- UI 设计读取 [soha-design](.agents/skills/soha-design/SKILL.md)；UI/HTTP/视觉/Midscene 验收读取 [soha-web-testing](.agents/skills/soha-web-testing/SKILL.md)。实施仍遵循 soha-frontend；权限、模型外发与 CI 边界在多仓环境按需读取 Core 的 soha-security，独立 clone 保留本仓门禁，不自动安装相邻仓库。

## Graphify

- 跨模块依赖、共享能力影响、模块迁移或跨仓任务，先查询已有图谱并核对源码；在已确认的多仓工作区按 [Graphify 开发手册](../docs/guides/graphify-development/runbook.md)检查新鲜度、查询和刷新。
- 结构变更稳定后显式刷新受影响仓库及跨仓图；普通小修改、文案和局部样式不自动建图。刷新失败先诊断，不默认使用 `--force`。
- 独立克隆时使用本仓 `graphify-out/graph.json` 与原生 Graphify 命令；缺少工具或图谱时直接查源码并说明缺口，不要求补齐相邻仓库。
