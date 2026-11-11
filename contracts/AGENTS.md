# Soha Contracts 仓库入口

## 所有权与规则入口

- 本仓是公开 OpenAPI、JSON Schema、生成 SDK 和兼容性基线的真实源；源契约先于生成物，不手改 `gen/` 下生成 DTO。手维护例外以本仓 skill 为准，不扩大例外范围。
- 在已确认的 OpenSoha 工作区读取 `../AGENTS.md` 一次；独立克隆使用本仓入口，不自动初始化相邻仓库或规划工具。根符号链接只负责技能发现，未自动加载时读取本仓真实源。
- 实现或实质审查前读取 [soha-contracts](.agents/skills/soha-contracts/SKILL.md)，再按任务读取 README、COMPATIBILITY、相关源契约、fixtures 和实际生成/兼容性入口。只读查询按需定位；已读且未变化的内容可复用，交接缺少关键约束时补读。

## 变更边界

- 修改前明确公开行为、数据所有者、副作用、权限/范围、错误/状态和直接消费者。字段兼容不等于分页、删除、取消或重试语义兼容。
- 私有实现接口与页面展示模型留在所属仓库；消费者局部视觉或内部重构不自动要求修改 contracts。不得复制业务逻辑或依赖 sibling 内部包。
- 先改 OpenAPI/Schema 源，再按现有入口生成、检查差异和兼容性，最后按依赖方向更新受影响消费者。计划和技能不等于额外发布或跨仓修改授权。
- 现状证据与目标规范分别核对；历史快照不自动证明新契约有效。兼容性基线、consumer 固定版本与公开版本只有明确原因及验证证据时才调整，不能为消除失败重写基线。

## 验证与交付

- 主入口为 `npm test`；公开契约修改执行兼容性和 [CI](.github/workflows/ci.yml) 的完整 consumer 矩阵，不在本文件复制一个可能过期的消费者数量。
- 固定组合从 `compat/consumer-revisions.json` 选择提交，缺少或无效版本必须失败；手动选择 latest 的集成检查独立报告，不替代固定组合验收。记录实际 contracts/consumer SHA，而非只记录分支名。
- 缺少 consumer、工具或发布依赖时报告验证缺口，不用临时替换掩盖缺失。区分通过、失败、跳过、未运行；生成成功、编译成功、运行行为及发布验收不是同一结论。
- 协作说明修改仅检查元数据、引用、内容一致性和差异，不无故重新生成 SDK 或运行 consumer。工作流/脚本修改执行相关正反例与适用 CI；相关代码、依赖和环境不变时复用成功证据。
- 保留用户未提交改动，仅提交任务所属变更；不自动合并 PR、发布包或移动公开标签。

- 测试与审查的证据边界见 [testing reference](.agents/skills/soha-contracts/references/testing.md)；多仓安全/OCR专项可读取 Core 的 soha-security，独立 clone 保留原有门禁，不自动获取公共 Skill。

## Graphify

- 跨模块依赖、共享能力影响、模块迁移或跨仓任务，先查询已有图谱并核对源码；在已确认的多仓工作区按 [Graphify 开发手册](../docs/guides/graphify-development/runbook.md)检查新鲜度、查询和刷新。
- 结构变更稳定后显式刷新受影响仓库及跨仓图；普通小修改、文案和局部样式不自动建图。刷新失败先诊断，不默认使用 `--force`。
- 独立克隆时使用本仓 `graphify-out/graph.json` 与原生 Graphify 命令；缺少工具或图谱时直接查源码并说明缺口，不要求补齐相邻仓库。
