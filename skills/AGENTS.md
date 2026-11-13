# Soha Skills 仓库入口

- 本组件负责官方 runtime skills、agent-facing skills、MCP presets、profiles 与发布目录。`skills/`、`agent-skills/` 是产品资产，`.agents/skills/` 是开发协作规则；不能把各仓开发技能集中复制到本仓。
- 先读取源码仓库根 `../AGENTS.md`；在已确认的 OpenSoha 多仓工作区另读取工作区 AGENTS 一次，不按固定父级猜测内部 docs 位置。根技能链接只提供发现入口，未自动加载时读取本仓真实源。
- 产品资产实现或实质审查前读取 [soha-skills](.agents/skills/soha-skills/SKILL.md)、受影响资产、schema 和能力/兼容性依据。开发协作说明修改只核对对应说明及引用，不因此触发资产构建。只读查询按需定位，未变化内容不重复读取。
- 变更前区分 runtime 资产、可安装 agent skill、目录/兼容性信息与开发规则，明确真实所有者和消费者。共享 catalog 修改需检查引用它的真实资产，不复制 Core 业务逻辑。
- 公开 manifest/schema 先改 contracts 真实源；本仓约束可以更严格，但不能分叉公开协议。能力声明来自真实实现与证据，目录快照不替代运行时能力发现。
- 保留示例、权限、禁止动作、敏感信息处理与安装完整性要求；发现或安装技能不等于用户授权执行外部操作。不得把尚未实现的工具、试验或 Cloud-only 能力描述为普遍可用。
- 产品资产验证入口为 `python3 tools/validate_assets.py`；仅相关资产变化时按本仓 skill 生成 index，包/发布变化执行 [活动 CI](../.github/workflows/skills-ci.yml) 和发布入口的适用完整检查。协作规则变更不更新 runtime index 或版本。
- 记录被验证的资产版本、目标能力与实际产物；schema 通过不代表运行时任务已执行。区分通过、失败、跳过和未运行，不放宽规则或重写兼容性证据掩盖失败。
- 文档和技能说明修改检查元数据、链接、内容一致性及差异；仅在相关代码、输入和环境未变化时复用结果。保留用户工作树，按仓库报告，未授权不安装、发布、合并或恢复历史任务。
- 资产权限、工具、输入输出、失败行为和依赖必须有契约或运行证据；格式合法、打包、安装和真实调用分别验收，记录 contracts 版本与目标运行时。

## Graphify

- 跨模块依赖、共享能力影响、模块迁移或跨仓任务，先查询已有图谱并核对源码；在已确认的多仓工作区按 工作区 `docs/guides/graphify-development/runbook.md`检查新鲜度、查询和刷新。
- 结构变更稳定后显式刷新受影响仓库及跨仓图；普通小修改、文案和局部样式不自动建图。刷新失败先诊断，不默认使用 `--force`。
- 独立克隆时使用本仓 `graphify-out/graph.json` 与原生 Graphify 命令；缺少工具或图谱时直接查源码并说明缺口，不要求补齐相邻仓库。

## 源码布局与历史入口

从源码根运行 `python3 skills/tools/validate_assets.py`，同提交 `contracts/` 与 Core `internal/` 为必需输入；缺失或重定向必须失败。普通技能安装仍使用经校验的稳定 release，不默认安装源码目录。
本目录 `.github/workflows/` 是保留的历史入口，在主仓不活动；不启用新发布者，公开分发入口切换另行验收。
