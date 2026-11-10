# 可选 dsh / Hindsight 部署（B3）

两个运行时默认关闭。此 Core 版本的新 Chat 和 Heya 依赖 dsh，Hindsight 可选。
已有安装先用原运行时排空 Native 在途任务、备份 PostgreSQL 并完成下文的离线切换。
Hindsight 切换前应用 Core 迁移至 0145，启用 Task124 运行配置与托管资产前应用至 0147。
详细参数与安全边界见 [完整说明](README.md) 和 `core.env.example`。

- dsh 固定 Node 24.14.0、SDK/runtime 0.2.2-rc.1，镜像见 Agent deploy/Dockerfile.dsh-runtime。
- Hindsight 固定 1.2.0/source 34a1386/index sha256:19d0c08f3df79acac29384826419a1ca158129e81ec527cdcbd8c97ff4ef76da。
- 为 Hindsight 单独准备数据库、角色、pgvector/pg_trgm 及迁移权限，禁止共享 Core 数据库或 schema，不自动迁移业务数据。
- Node 生产模式 `SOHA_DSH_MODEL_AUTH_MODE=core`，无需 PAT，模型固定虚拟名 `soha-workbench`；由 Core 选择用户实际路由。配置 `DSH_CORE_BASE_URL` 后派生 `/api/v1/copilot/dsh/model/v1` 与 `/api/v1/copilot/dsh/tools`，不得指向 Agent 集群代理路径。Core/Node 共享同一服务 token。
- Hindsight 仍须显式指定 Gateway OpenAI-compatible URL、LLM/embedding 模型和向量维度，PAT/服务 token/数据库 URL 使用只读密钥文件；不提供公有 provider 默认值。
- `HINDSIGHT_EMBEDDING_GATEWAY_URL` 可单独指定向量入口，空值沿用 `HINDSIGHT_GATEWAY_URL`。本次完整资格组合均走 `/api/v1/ai-gateway/llm/qwen/v1`：抽取模型 `Qwen/Qwen3-30B-A3B-Instruct-2507`，向量模型 `Qwen/Qwen3-Embedding-0.6B`、1024 维。Hindsight 协议 provider 仍为 `openai`，`qwen` 是 Gateway 路线类型；也可显式选独立 OpenAI 抽取入口。启动脚本通过官方 extra-body 设置请求 float 向量，输入保持原始文本；PAT 仅授权所选模型/provider。
- 启用 dsh 时使用配套 Core/dsh 镜像，设置 `SOHA_DSH_RUNTIME_ENABLED=true`。所有 Core worker 均可读取加密检查点后，设置 `SOHA_SECURITY_PROTECTED_DURABLE_STORAGE=true` 并沿用安装的有效凭据加密密钥。停用 dsh 时，仍需恢复加密检查点的安装应保留该开关和原密钥。
- Core 容器只挂载所启用运行时的服务 token。容器间使用可达的私有地址，隔离网络内使用 HTTP 时显式开启 dsh ALLOW_INSECURE_HTTP。Hindsight namespace 必须稳定且由服务端持有。
- 上游 API 默认无 Bearer 保护；gateway.py 校验服务 token，仅放行 Core adapter 所需 raw API。原始 API 绑定共享网络命名空间的 loopback，不发布管理 API；reflect/config/model 覆写被拒绝。
- start.py 禁用自动 consolidation 和下载，使用 rrf，不启动额外 worker/UI；部署网络仍须限制出站仅到私有 Gateway/独立数据库。已有 bank 覆写与派生计划的数据库不能直接接入。
- 模型与反思单次请求超时均为 95 秒，官方模型重试限定一次；固定 provider 对 HTTP400/429 和 JSON 解析错误也会重试一次，不会放宽格式校验及 Soha 来源检查。这只重试模型请求，不重提未知结果的 retain operation；Core 继续对账原操作并维持删除依赖屏障。
- 文档删除会通过内部 loopback API 清除同 bank 全部 mental models（含手工模型和知识页承载模型），并由固定 PostgreSQL schema 的外键级联删除刷新历史；其他 raw 文档保留。只适用于 Soha 独占 bank、新建独立 PostgreSQL 和默认检索索引；Soha 未开放 Hindsight 手工模型/知识页编辑。这不是共享 Hindsight 数据库或外部索引存储的通用清理代理，也不能同时接受管理员、UI/MCP、模板或导入写入。服务 token 和路径格式本身不能证明任意已有 bank 归属。
- 对已尝试的 retain，Core 用持久 outbox 的操作 ID 发起文档删除。保护网关核对 bank、文档及操作终态，先删除含原始载荷的子操作，再删除父操作。Core 要求成功或原始文档 404 响应附带匹配的清理回执。恢复记忆同步前部署配套网关。
- raw 文档已不存在（404）时仍须完成上述清理才确认。内部反复取第一页 metadata（不排除知识页），校验 bank/模型 ID；单次最多删除 1000 个模型，整个流程限时 20 秒，失败返回脱敏 502，由 Core durable delete 重试续清；并发删除直接 503，不无界排队。建议 Core Hindsight timeout 为 30 秒；较短调用超时仍须持久重试，不能当作删除成功。外部 MM/history/refresh 管理 API 继续拒绝。
- 固定版 late refresh 只 UPDATE 已有模型，不 upsert；删除模型和历史外键阻止其回写复活，但不能封住管理员重新创建模型或旧 retain 重放。生产必须保持独占写入、无旧计划/模板、关闭自动 consolidation，并保留 Core 原 retain 回执与删除依赖。默认 PostgreSQL 检索中的旧索引命中无法 hydrate 已删模型；不据此宣称其他外部索引已完成存储擦除。
- 所有容器非 root、只读根文件系统，不挂 Docker socket/宿主工作区；dsh 单写者独立持久卷，Hindsight 持久数据在独立数据库。

在 Core 仓执行 `opensoha-verify run -- docker compose -f deploy/dsh-hindsight/compose.yaml --profile dsh --profile hindsight config --quiet` 可离线校验配置。
两个 profile 可独立启用。启用 Hindsight 前须另行验证独立数据库和 Gateway。
只启用 dsh 时使用 `--profile dsh up --wait --wait-timeout 180`，由镜像执行带认证的健康检查。
Core `/readyz` 只检查 PostgreSQL，开放流量前应确认一条已认证的 Chat 和一条具备资格的 Heya 请求都完成并留下对应 Run。
Task124 发布默认配置前还要核对 dsh 的 `runtime-assembly-v1` 和一份已审核的测试配置。
轮换共享服务 token 时先暂停准入并排空受影响 Run，同时更新两端，重建对应运行时容器并重启全部 Core 副本。
保留 journal 卷和数据库，复验通过后再恢复准入。
用目标镜像摘要、架构、UID、只读根文件系统和挂载验证容器启动。
Hindsight 还要检查保护 API 与独立 PostgreSQL、Gateway 的实际连通。
分别记录离线渲染、容器启动和业务数据迁移验收。


### 离线升级工具

镜像构建配方包含显式维护入口 `/app/runtime-upgrade`，Server entrypoint不会调用它。先停止并排空执行和同步worker，将schema迁移到0144，再审核只读清单。通过受控进程环境提供 `SOHA_UPGRADE_DATABASE_URL`；apply还要求已审核digest和挂载的非空0600备份，DSN及凭据不放命令参数。`-mode rollback -digest CHECKPOINT`仅执行已说明的条件补偿，不复活旧引擎、审批或墓碑。

配方变更不代表已发布镜像包含工具。Task123候选仍需真实镜像构建及兼容的已发布contracts，满足前提后才能部署。匹配源码的构建入口为 `GOWORK=off go build -o runtime-upgrade ./cmd/runtime-upgrade`，同样须先满足依赖前提。
