# Soha Core 仓库入口

- 本仓负责 Go 核心控制平面、Server API 和原始 Docker/Kubernetes 部署；必须可脱离 `soha-cloud` 独立运行。
- 在 OpenSoha 多仓工作区中读取 `../AGENTS.md` 一次；独立克隆时使用本仓规则，不要求初始化相邻仓库或规划工具。
- 后端实现或审查使用 [soha-backend](.agents/skills/soha-backend/SKILL.md)，部署变更使用 [soha-deploy](.agents/skills/soha-deploy/SKILL.md)，只读取当前任务相关内容。
- 公开协议以 `soha-contracts` 为源；Web 源码归属 `soha-web`，本仓消费构建产物，不手改嵌入生成物。
- Go 改动先验证受影响包；主入口为 `GOWORK=off go test ./...`。架构、契约、依赖、部署或发布变更执行适用的完整门禁，命令和版本以 [CI](.github/workflows/ci.yml) 与仓库脚本为准。
- 文档和技能改动只检查内容、链接与差异。已通过的检查在相关代码和环境未变化时复用；保留用户未提交改动。
