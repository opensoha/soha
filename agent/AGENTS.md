# Soha Agent 仓库入口

## 入口与所有权

- 本仓负责独立 Agent、runner 和集群执行能力；不得 import `soha/internal/**`，通过公开 API 和 contracts 与控制面交互。
- 在已确认的 OpenSoha 多仓工作区读取 `../AGENTS.md` 一次；独立克隆使用本仓规则，不要求初始化相邻仓库或规划工具。技能链接仅提供发现，不能代替读取真实源。
- Agent 实现或实质审查前读取 [soha-agent](.agents/skills/soha-agent/SKILL.md) 及其指定的相关参考；只读查询按需定位。已读且未变化的规则可复用，交接后缺少关键约束时补读必要部分。
- 技能与历史计划不是安装、执行远程任务、修改生产环境或发布的授权。

## 修改前确认

- 确认能力归属本地 API、集群适配器、runner、Outpost、环境运行时还是打包；先复用既有所有者，不增加平行执行路径。
- 明确协议和配置、动作权限、工作目录与资源所有权、并发限制、取消/超时、heartbeat/callback 和清理行为；保留诊断脱敏、终态幂等与迟到回调保护。
- 普通修复不顺手扩大 allowlist、改变共享默认值或 Core 状态机。涉及公开协议先走 contracts；共享 runner/安全修改识别消费者并扩大回归。
- 当前实现用于核实行为，不自动证明符合有效规范；发现冲突记录证据，不通过放宽断言、关闭鉴权或跳过失败完成任务。

## 验证与交付

- 普通 Go 修复执行受影响包回归；主入口为 `GOWORK=off go test ./...`。架构、契约、依赖、安全、并发/runner、打包或发布变化执行技能及 [CI](.github/workflows/ci.yml) 的适用完整门禁，保留各 runtime 镜像和 Hermes 检查。
- mock API、包测试、真实 runner/集群、镜像构建属于不同证据。记录实际 Agent/Core/Contracts 版本，外部环境缺失或测试 skip 不能表述为运行链路通过。
- 区分本次失败、既有失败、未运行及环境缺失；成功结果只在相关代码、依赖和环境未变化时复用。保留原 CI 要求，不更新基线掩盖新问题。
- 文档/技能改动只检查内容、链接和差异，不机械构建运行时。保留用户工作树，仅提交任务所属文件；未请求时不合并 PR、不发布。
- 共享 runner/lifecycle 修改验证重试、租约及资源清理，新增外部操作保持有界超时和现有审计路径；验证记录包括实际执行模式。

- 测试/安全验收按需读取 [testing reference](.agents/skills/soha-agent/references/testing.md)；多仓专项审查可读取 Core 的 soha-security。独立 clone 的基础门禁不依赖该公共 Skill，不自动 clone 或安装工具。

## Graphify

- 跨模块依赖、共享能力影响、模块迁移或跨仓任务，先查询已有图谱并核对源码；在已确认的多仓工作区按 [Graphify 开发手册](../docs/guides/graphify-development/runbook.md)检查新鲜度、查询和刷新。
- 结构变更稳定后显式刷新受影响仓库及跨仓图；普通小修改、文案和局部样式不自动建图。刷新失败先诊断，不默认使用 `--force`。
- 独立克隆时使用本仓 `graphify-out/graph.json` 与原生 Graphify 命令；缺少工具或图谱时直接查源码并说明缺口，不要求补齐相邻仓库。
