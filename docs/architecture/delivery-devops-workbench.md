# 应用交付 DevOps 工作台方案

## 目标

应用交付工作台需要面向开发和测试人员提供一个以应用为核心的 DevOps 操作面。

它不是单纯的流水线列表，也不是 Kubernetes 资源页面的包装，而是把应用、环境、服务、容器、构建、测试、发布、交付物和运行态证据组织到同一个应用上下文中。

设计参考 Zadig 的产品组织方式：

- 项目/应用是研发交付的业务单元，聚合服务、环境、构建、测试、工作流和版本。
- 环境承载一组服务及其配置，是开发联调、测试验证和生产发布的目标空间。
- 工作流由阶段和任务组成，阶段通常串行推进，阶段内任务可以串行或并发。
- 构建、部署、测试、发布、通知、配置变更等任务都应成为可编排能力。

参考资料：

- [Zadig 核心概念](https://docs.koderover.com/zadig/cn/Zadig%20v3.4/quick-start/concepts/)
- [Zadig 项目手册](https://docs.koderover.com/zadig/en/Zadig%20v3.4/project-manual/)
- [Zadig 环境服务概览](https://docs.koderover.com/zadig/en/Zadig%20v4.0/env/overview/)
- [Zadig 工作流任务](https://docs.koderover.com/zadig/cn/Zadig%20v4.1/project/workflow-jobs/)
- [Zadig 构建配置](https://docs.koderover.com/zadig/en/Zadig%20v4.2/project/build/)

soha 不照搬 Zadig 的对象命名。当前控制面已经拥有 `applications`、`application_build_sources`、`application_environments`、`release_targets`、`workflow_templates`、`release_bundles`、`execution_tasks` 和 `execution_logs`，新的设计应优先复用这些对象，并补齐应用服务、容器组件、测试运行和交付物策略。

## 设计原则

- 应用优先：研发和测试从应用进入，而不是从集群、命名空间或流水线进入。
- 环境分层：开发、测试、预发、生产使用同一绑定模型，但 UI 语义、审批、权限和默认动作不同。
- 服务可见：一个应用可以包含多个服务，每个服务可以包含多个容器、端口、配置、构建来源和部署目标。
- 交付物不可变：一次构建或组合发布产出的镜像、包、SBOM、测试报告和部署证据应沉淀为可追踪交付物。
- DAG 编排：CI/CD 流程使用显式 DAG 描述依赖、并发、失败分支、审批和回滚，不用隐式前端步骤拼装。
- 开发自助：开发人员可以围绕自己的服务触发构建、部署开发环境、查看日志和回滚。
- 测试闭环：测试人员可以按环境和版本查看待测内容、触发测试、确认报告并推动升级。
- 平台守边界：API 服务只做控制面编排，真实构建、测试、发布动作进入执行平面或 runner，不在 Gin handler 中直接执行长任务。

## 用户视角

### 开发视角

开发人员的主路径：

1. 进入应用中心，选择自己负责的应用。
2. 在应用详情中选择服务组件，例如 `api`、`worker`、`frontend`。
3. 选择代码分支、提交、构建来源和镜像标签策略。
4. 触发 CI：拉代码、构建镜像、单测、扫描、产出交付物。
5. 将产物部署到开发环境或联调环境。
6. 查看服务运行状态、Pod、日志、事件、最近执行任务和失败原因。
7. 必要时重试、回滚、重启服务或只更新某个容器镜像。

开发页面需要强调：

- 我的应用和我的服务。
- 最近提交、最近构建和当前环境版本。
- 快速构建、快速部署、日志和失败诊断。
- 环境差异，例如开发环境使用临时分支，测试环境使用候选版本。

### 测试视角

测试人员的主路径：

1. 进入测试环境矩阵，查看每个应用在 `test`、`sit`、`uat` 等环境的当前版本。
2. 选择待测应用环境，查看本次变更包含哪些服务、镜像、提交、配置和数据库变更。
3. 触发测试工作流，包括接口测试、UI 自动化、冒烟、性能基线或人工验证任务。
4. 查看测试报告、失败用例、关联日志、环境健康和阻塞项。
5. 将验证通过的 Release Bundle 标记为可升级，或退回开发修复。
6. 在需要时创建缺陷、补充验证说明或冻结环境。

测试页面需要强调：

- 环境维度优先，其次才是服务维度。
- 一个环境当前正在验证哪个 Release Bundle。
- 测试任务、测试报告和环境健康必须能追溯到服务和容器镜像。
- 测试通过不是发布成功的替代品，而是后续预发或生产发布的质量门禁输入。

## 信息架构

应用交付工作台建议收敛为以下一级/二级页面。

| 页面 | 面向角色 | 目的 |
| --- | --- | --- |
| 应用中心 | 开发、测试、发布 | 以应用卡片和列表进入交付上下文 |
| 应用详情 | 开发、测试 | 应用总览、服务、环境、流水线、交付物、测试报告 |
| 服务组件 | 开发 | 管理应用下的服务、容器、端口、构建来源和部署目标 |
| 环境矩阵 | 测试、发布 | 按环境查看应用版本、服务状态、测试状态和升级门禁 |
| CI/CD 工作流 | 开发、测试、平台 | 维护可复用 DAG 模板和应用环境绑定使用的流程 |
| 交付物中心 | 开发、测试、发布 | 查看 Release Bundle、镜像、包、报告、SBOM 和部署证据 |
| 执行任务 | 平台、开发 | 查看 runner/job 执行状态、日志、心跳、取消和重试 |
| 审批策略 | 发布、平台 | 管理测试升级、预发、生产发布等门禁 |

现有页面可以这样归位：

- `/applications` 继续作为应用中心。
- `/applications/:applicationId` 扩展为应用详情工作台。
- `/application-environments` 继续作为应用环境绑定管理面，但在应用详情内提供更自然的环境视图。
- `/workflow-templates` 继续作为 DAG 模板管理面。
- `/delivery/release-bundles` 作为交付物中心的控制面列表。
- `/delivery/execution-tasks` 作为执行任务和日志查看面。
- `/release-board` 演进为环境矩阵或发布矩阵。

## 核心对象模型

### 已有对象

| 对象 | 当前职责 | 方案中的定位 |
| --- | --- | --- |
| `applications` | 应用注册、仓库元数据、构建来源聚合 | 应用级入口和权限/归属边界 |
| `application_build_sources` | 应用的多构建来源 | 服务或应用级 CI 输入来源 |
| `application_environments` | 应用与环境的绑定 | 环境级交付策略、工作流模板和目标集合 |
| `release_targets` | 集群、命名空间、工作负载、容器目标 | 服务容器部署目标，可继续扩展 group/wave/region |
| `workflow_templates` | DAG 模板定义 | CI/CD 编排模板 |
| `release_bundles` | 不可变交付版本包 | 应用或服务组合版本 |
| `execution_tasks` | 执行平面任务 | 构建、测试、部署、扫描、通知等任务实例 |
| `execution_logs` | 执行日志 | 工作流和任务的证据流 |
| `approval_policies` | 审批策略 | 环境升级和生产发布门禁 |

### 新增建议对象

#### 应用服务 `application_services`

应用服务是应用内可独立构建、测试、部署和观测的最小业务组件。

建议字段：

- `id`
- `application_id`
- `key`
- `name`
- `description`
- `service_kind`: `kubernetes_workload`、`helm_release`、`external_service`、`job`
- `owner_team`
- `repository_provider`
- `repository_project_id`
- `repository_path`
- `default_branch`
- `build_source_id`
- `enabled`
- `metadata`

说明：

- 单体应用可以只有一个服务。
- 微服务应用可以包含多个服务。
- 一个服务可以复用应用级仓库，也可以覆盖自己的仓库和构建来源。
- 服务不直接等于 Kubernetes Deployment。Deployment 是某个环境中的运行目标。

#### 服务容器 `application_service_containers`

服务容器描述服务内需要被构建、发布或观测的容器单元。

建议字段：

- `id`
- `service_id`
- `name`
- `image_repository`
- `default_tag_template`
- `dockerfile_path`
- `build_context_dir`
- `runtime_ports`
- `env_schema`
- `resource_profile`
- `health_check`
- `metadata`

说明：

- 一个服务可包含 `main`、`sidecar`、`init` 等多个容器。
- CI 任务产出的镜像应绑定到具体容器，而不是只绑定到应用。
- 发布时允许只更新某个服务的某个容器。

#### 环境服务实例 `application_environment_services`

环境服务实例表示某个服务在某个应用环境中的配置和状态。

建议字段：

- `id`
- `application_environment_id`
- `service_id`
- `desired_version`
- `current_version`
- `config_profile_id`
- `variable_set`
- `deploy_order`
- `enabled`
- `metadata`

说明：

- 同一个应用服务在 `dev`、`test`、`prod` 环境可以有不同变量、目标、资源规格和发布策略。
- 环境服务实例是测试人员理解“当前环境包含哪些服务版本”的关键对象。

#### 服务部署目标 `application_service_targets`

可以逐步从 `release_targets` 扩展或派生，不急于替换现表。

建议语义：

- 绑定 `application_environment_service_id`
- 指向 `cluster_id`、`namespace`、`workload_kind`、`workload_name`、`container_name`
- 保留 `targetKind`、`executorKind`、`groupKey`、`waveKey`、`regionKey`、`configRef`

说明：

- `release_targets` 当前已经能表达 Deployment 容器目标。
- 新对象的意义是把目标从“应用环境”进一步下沉到“服务环境实例”。
- 初期可以保持后端兼容：没有服务实例时继续使用 `release_targets`。

#### 测试配置 `test_plans`

测试配置描述可复用测试能力。

建议字段：

- `id`
- `application_id`
- `service_id`
- `key`
- `name`
- `test_kind`: `unit`、`api`、`ui`、`smoke`、`performance`、`security`
- `runner_kind`: `ci_agent_runner`、`k8s_job_runner`、`external`
- `command`
- `repository_ref`
- `variable_schema`
- `report_pattern`
- `enabled`

#### 测试运行 `test_runs`

测试运行记录一次测试执行和报告。

建议字段：

- `id`
- `application_id`
- `application_environment_id`
- `service_id`
- `release_bundle_id`
- `execution_task_id`
- `test_plan_id`
- `status`
- `summary`
- `report_artifact_id`
- `started_at`
- `finished_at`

说明：

- 测试运行必须能关联 Release Bundle、服务、环境和执行任务。
- 测试报告作为交付物进入统一交付物中心。

#### 交付物策略 `artifact_policies`

交付物策略定义产物命名、保留、晋级和准入。

建议字段：

- `id`
- `key`
- `name`
- `versioning_mode`
- `retention_days`
- `required_artifact_kinds`
- `promotion_gates`
- `signing_required`
- `sbom_required`
- `metadata`

## 环境模型

环境从应用中心内维护，不再依赖独立的全局环境主数据页面。每个应用可以用自己的环境标签组织交付策略和运行态绑定。

| 层级 | 对象 | 说明 |
| --- | --- | --- |
| 应用环境标签 | `application_environments.environment_id` / `environment_key` | 应用内定义 `dev`、`test`、`sit`、`uat`、`pre`、`prod` 等标签，不要求预先维护全局环境记录 |
| 应用环境绑定 | `application_environments` | 某个应用在某个环境标签中的交付策略、工作流模板、构建策略、发布策略和目标集合 |
| 环境服务实例 | `application_environment_services` | 某个服务在某个应用环境中的启用状态、变量、当前版本和目标集合 |
| 服务部署目标 | `release_targets` 或 `application_service_targets` | 指向真实 Kubernetes 工作负载、容器或其他执行目标 |

推荐环境语义：

| 环境 | 默认动作 | 审批 | 典型用户 | 版本来源 |
| --- | --- | --- | --- | --- |
| `dev` | 构建并部署 | 默认不需要 | 开发 | 分支、提交、临时镜像 |
| `test` / `sit` | 部署并测试 | 可选 | 测试 | 候选 Release Bundle |
| `uat` | 验收验证 | 通常需要 | 测试、业务 | 测试通过的 Release Bundle |
| `pre` | 预发布 | 需要 | 发布、运维 | 待上线 Release Bundle |
| `prod` | 正式发布 | 必须需要 | 发布、运维 | 已审批 Release Bundle |

前端用语要求：

- `dev/test/pre` 可以显示为“部署”，即使底层仍使用同一个 orchestration 模型。
- `prod` 应显示为“发布”，并突出审批、窗口、回滚和审计。
- 测试环境的主入口是“当前待测版本”和“测试结论”，不是单纯 Pod 状态。

## 多服务与容器设计

应用详情应新增服务组件视图。

服务组件卡片建议展示：

- 服务名、负责人、语言、仓库、默认分支。
- 当前各环境版本：`dev`、`test`、`uat`、`prod`。
- 容器列表：容器名、镜像仓库、当前镜像、最近构建、健康检查。
- 运行目标：集群、命名空间、工作负载、容器名。
- 快捷动作：构建、部署到开发、部署到测试、查看日志、查看事件、回滚。

服务详情建议分为：

- 总览：最近构建、最近部署、当前环境状态。
- 容器：镜像、Dockerfile、构建上下文、端口、环境变量 schema。
- 构建：构建来源、构建模板、构建参数、最近构建记录。
- 环境：每个环境的变量、目标、当前版本、部署策略。
- 测试：关联测试配置、最近测试运行、报告。
- 运行态：Deployment、Pod、Service、Ingress、事件和日志。

容器更新规则：

- CI 构建产物绑定到 `service + container`。
- CD 部署节点从 Release Bundle 选择目标容器镜像。
- 如果一个服务有多个容器，DAG 必须允许只构建或只更新其中一部分。
- Sidecar 和 init container 默认不作为独立服务，但可以成为服务下的容器条目。

## CI/CD DAG 编排

现有 `workflow_templates.definition` 已支持 `release_dag`，前端已有 DAG 编辑器。后续应把 DAG 从“发布流程模板”扩展为“CI/CD 工作流模板”。

### DAG 结构

推荐结构：

```json
{
  "schemaVersion": 3,
  "mode": "delivery_dag",
  "inputs": [],
  "nodes": [],
  "edges": []
}
```

节点公共字段：

- `id`
- `type`
- `name`
- `timeoutSeconds`
- `continueOnFailure`
- `dependsOn`
- `runPolicy`: `always`、`on_success`、`on_failure`
- `serviceSelector`
- `environmentSelector`
- `config`

边公共字段：

- `source`
- `target`
- `condition`: `success`、`failure`、`always`

### 节点类型

建议从以下节点类型扩展：

| 类型 | 作用 | 产出 |
| --- | --- | --- |
| `checkout` | 拉取代码或固定提交 | 源码快照 |
| `build_image` | 构建服务容器镜像 | 镜像交付物 |
| `build_package` | 构建二进制、前端包或 Helm 包 | 文件交付物 |
| `unit_test` | 单元测试 | 测试报告 |
| `code_scan` | 代码扫描、安全扫描 | 扫描报告 |
| `package_bundle` | 组合多服务产物为 Release Bundle | Release Bundle |
| `deploy_service` | 部署一个或多个服务 | 部署记录 |
| `deploy_environment` | 按环境服务编排部署 | 部署记录集合 |
| `wait_rollout` | 等待 Kubernetes rollout | rollout 证据 |
| `smoke_test` | 冒烟测试 | 测试运行 |
| `api_test` | 接口自动化测试 | 测试报告 |
| `performance_test` | 性能测试 | 性能报告 |
| `manual_approval` | 人工审批 | 审批记录 |
| `promote_bundle` | 晋级 Release Bundle | 晋级记录 |
| `rollback` | 回滚服务或环境 | 回滚记录 |
| `notify` | 通知 IM、邮件或 webhook | 通知记录 |
| `http_callback` | 调用外部系统 | 外部回执 |

现有节点兼容关系：

- `build` 可兼容为 `build_image`。
- `deploy_update_image` 可兼容为 `deploy_service`。
- `check_http`、`smoke_test`、`manual_approval`、`notify`、`rollback_to_previous` 保持语义。

### 典型开发流水线

适用于分支开发和联调环境。

1. `checkout`
2. `build_image`，可对多个服务并行执行。
3. `unit_test`
4. `deploy_service` 到 `dev`
5. `wait_rollout`
6. `smoke_test`
7. `notify`

特点：

- 默认不需要审批。
- 允许开发只选择一个服务执行。
- 产物可以是临时 Bundle，但仍要记录镜像、提交和任务日志。

### 典型测试流水线

适用于测试环境验证。

1. `package_bundle`
2. `deploy_environment` 到 `test`
3. `wait_rollout`
4. `api_test` 和 `ui_test` 并行。
5. `performance_test` 可选。
6. `manual_approval` 或测试确认。
7. `promote_bundle` 到 `uat` 或 `pre`。

特点：

- 以 Release Bundle 为输入，不建议直接选择任意分支部署。
- 测试报告是门禁输入。
- 失败时可进入 `rollback` 或 `notify` 分支。

### 典型生产发布流水线

适用于生产变更。

1. `manual_approval`
2. `promote_bundle`
3. `deploy_environment` 到 `prod`，按 `groupKey`、`waveKey`、`regionKey` 分批。
4. `wait_rollout`
5. `smoke_test`
6. `create_silence` 或解除静默。
7. `notify`
8. 失败分支进入 `rollback`。

特点：

- 必须有审批、审计、操作日志和发布窗口。
- `release_targets` 的 `groupKey`、`waveKey`、`regionKey` 应用于分组、波次和地域编排。
- 生产发布不应允许浏览器临时拼接目标。

## 交付物设计

Release Bundle 是交付版本的主对象。

一个 Release Bundle 可以包含：

- 镜像：`image`，带 registry、repository、tag、digest。
- 文件包：`package`，例如 tar、zip、jar、前端静态包。
- Helm Chart 或 Kustomize 输出。
- Kubernetes manifest 渲染结果。
- SBOM。
- 签名和摘要。
- 测试报告。
- 扫描报告。
- 部署证据：rollout、Pod 状态、事件摘要。

建议 Bundle 结构：

```json
{
  "applicationId": "app-1",
  "version": "2026.05.23-001",
  "services": [
    {
      "serviceId": "svc-api",
      "containers": [
        {
          "name": "api",
          "image": "registry.example.com/app/api:20260523-abc",
          "digest": "sha256:..."
        }
      ],
      "source": {
        "repo": "group/app",
        "branch": "feature/demo",
        "commit": "abc123"
      }
    }
  ],
  "artifacts": [],
  "quality": {
    "testRunIds": [],
    "scanRunIds": []
  }
}
```

交付物页面必须支持：

- 按应用、环境、服务、状态筛选。
- 查看 Bundle 中每个服务和容器的镜像。
- 查看生成该产物的 DAG、执行任务和日志。
- 查看测试、扫描、审批、部署证据。
- 对通过质量门禁的 Bundle 执行晋级或发布。

## API 设计方向

建议新增或扩展 API。

### 应用服务

- `GET /api/v1/applications/:applicationID/services`
- `POST /api/v1/applications/:applicationID/services`
- `GET /api/v1/applications/:applicationID/services/:serviceID`
- `PUT /api/v1/applications/:applicationID/services/:serviceID`
- `DELETE /api/v1/applications/:applicationID/services/:serviceID`
- `GET /api/v1/applications/:applicationID/services/:serviceID/runtime`

### 环境服务

- `GET /api/v1/application-environments/:applicationEnvironmentID/services`
- `PUT /api/v1/application-environments/:applicationEnvironmentID/services`
- `GET /api/v1/application-environments/:applicationEnvironmentID/services/:serviceID/detail`

### 测试

- `GET /api/v1/delivery/test-plans`
- `POST /api/v1/delivery/test-plans`
- `PUT /api/v1/delivery/test-plans/:testPlanID`
- `DELETE /api/v1/delivery/test-plans/:testPlanID`
- `GET /api/v1/delivery/test-runs`
- `GET /api/v1/delivery/test-runs/:testRunID`
- `POST /api/v1/delivery/test-runs`

### 交付物

现有 `release_bundles` 和 `execution_tasks.artifacts[]` 可先复用。后续可新增：

- `GET /api/v1/delivery/artifacts`
- `GET /api/v1/delivery/artifacts/:artifactID`
- `POST /api/v1/delivery/release-bundles/:bundleID/promote`

### 工作流

当前 `workflow_templates.definition` 可以兼容新 schema。建议新增：

- `POST /api/v1/workflows/preview-dag`
- `POST /api/v1/workflows/:workflowTemplateID/validate`
- `POST /api/v1/workflows/:workflowTemplateID/run`
- `POST /api/v1/workflow-runs/:workflowRunID/cancel`
- `POST /api/v1/workflow-runs/:workflowRunID/retry-node`

## 后端分层

推荐模块归属：

- `internal/application/app`
  - 应用和应用服务编排。
  - 服务仓库、构建来源、容器定义校验。
- `internal/application/delivery`
  - 应用详情聚合。
  - 环境矩阵聚合。
  - 服务运行态聚合。
- `internal/application/workflow`
  - DAG 校验、运行、节点状态推进。
  - 节点到 execution task 的转换。
- `internal/application/build`
  - 构建节点执行入口。
  - 镜像和包交付物元数据回填。
- `internal/application/release`
  - 部署、发布、回滚节点执行入口。
  - Kubernetes 目标更新和发布记录。
- `internal/application/execution`
  - runner claim、callback、heartbeat、取消、重试、日志和 artifact 归一。
- `internal/application/test`
  - 测试配置、测试运行、测试报告归档。
- `internal/repository/application`
  - 应用服务和服务容器持久化。
- `internal/repository/delivery`
  - Bundle、execution task、artifact、approval policy。
- `internal/repository/test`
  - test plan 和 test run。

控制面规则：

- HTTP handler 只做 request/response 和 principal 解析。
- 应用服务、环境服务、DAG 运行语义放在 application 层。
- 构建、测试、扫描、部署长任务只能进入 execution plane。
- 回调必须通过 `internal/application/execution`，不能绕过统一任务状态同步。

## 前端页面设计

### 应用中心

列表字段：

- 应用名、应用分组、负责人、服务数、环境数、最近 Bundle、最近执行状态。
- 开发环境状态、测试环境状态、生产版本。
- 快捷入口：详情、构建、环境矩阵、交付物。

筛选：

- 我的应用。
- 应用分组。
- 语言/技术栈。
- 环境状态。
- 最近失败。

### 应用详情

推荐 tabs：

- 总览：应用健康、环境版本、最近执行、质量门禁。
- 服务：多服务列表和服务详情入口。
- 环境：应用环境矩阵。
- 流水线：绑定的 DAG 模板和最近运行。
- 交付物：Release Bundle 和 artifact。
- 测试：测试计划、测试运行和报告。
- 变更：提交、部署、审批、审计和操作日志。

### 服务详情

推荐布局：

- 左侧服务元数据和环境选择。
- 中间容器、构建、部署目标和测试配置。
- 右侧最近任务、日志、事件和失败诊断。

### 环境矩阵

环境矩阵按环境列、应用/服务行展示：

- 当前版本。
- 目标版本。
- 部署状态。
- 测试状态。
- 门禁状态。
- 最近失败节点。

测试人员可以从矩阵进入某个应用环境详情，查看待测 Bundle 的服务清单和报告。

### DAG 编辑器

现有 React Flow 编辑器继续保留，但需要新增：

- 节点库按 CI、CD、测试、质量、安全、通知分类。
- 节点输出声明，例如 `image`、`package`、`testReport`、`bundle`。
- 节点输入绑定，例如来自前置节点、运行时输入或环境绑定。
- 服务选择器，支持全部服务、指定服务、运行时选择。
- 环境选择器，支持绑定环境、运行时选择、固定环境。
- 分支条件和失败处理。
- DAG 校验结果面板。

## 权限与范围

建议权限键：

- `delivery.applications.view`
- `delivery.applications.manage`
- `delivery.application-services.view`
- `delivery.application-services.manage`
- `delivery.application-environments.view`
- `delivery.application-environments.manage`
- `delivery.workflow-templates.view`
- `delivery.workflow-templates.manage`
- `delivery.workflow-runs.execute`
- `delivery.workflow-runs.cancel`
- `delivery.release-bundles.view`
- `delivery.release-bundles.promote`
- `delivery.test-plans.view`
- `delivery.test-plans.manage`
- `delivery.test-runs.execute`
- `delivery.test-runs.view`

范围规则：

- 应用归属分组、团队和环境标签范围。
- 开发人员默认可操作 `dev` 和个人/团队授权环境。
- 测试人员默认可操作 `test`、`sit`、`uat` 测试相关动作，但不直接拥有生产发布权。
- 生产发布、回滚、审批策略变更必须独立授权。

## 迭代路线

### Phase 1：应用服务建模

- 新增应用服务和服务容器模型。
- 应用详情新增服务 tab。
- 服务与现有 build source、release target 建立映射。
- Release Bundle 展示服务/容器级 artifact。

### Phase 2：环境服务和测试闭环

- 新增环境服务实例。
- Release board 演进为环境矩阵。
- 新增 test plan 和 test run。
- 测试报告进入 artifact 视图。

### Phase 3：CI/CD DAG 升级

- `workflow_templates.definition` 升级为 `delivery_dag` schema。
- DAG 节点支持输入/输出声明。
- 工作流运行支持服务选择、环境选择和节点重试。
- 构建、测试、部署节点统一进入 execution task。

### Phase 4：交付物晋级和生产发布

- Release Bundle promote API。
- 环境晋级门禁。
- 按 group/wave/region 的生产发布编排。
- 生产回滚、审计、操作日志和通知闭环。

### Phase 5：智能诊断与效能

- 将执行日志、测试报告、Kubernetes 事件和监控指标接入 AI 工作台。
- 生成失败根因摘要、瓶颈节点、服务质量趋势和环境稳定性报告。
- 提供测试环境占用、构建耗时和发布成功率分析。

## 与现有实现的兼容策略

- `applications` 继续作为应用入口，不拆成项目对象。
- `application_build_sources` 保留应用级构建来源，同时允许服务覆盖。
- `application_environments` 继续作为应用和环境的主绑定对象。
- `release_targets` 继续服务于当前 Deployment 容器目标，新增服务目标时做兼容映射。
- `workflow_templates.definition` 保留旧 `release_dag` 兼容解析，新增 `delivery_dag` 不破坏历史模板。
- `release_bundles` 继续作为不可变版本包，artifact 明细从 execution task result 逐步正规化。
- `execution_tasks` 继续作为执行平面统一任务，不为构建、测试、部署各造一套任务状态机。

## 非目标

- 不把 soha 改造成 GitOps-only 平台。
- 不让前端直接拼 Kubernetes manifest 作为主要发布路径。
- 不在 API handler 内执行构建、测试、扫描或部署长任务。
- 不把测试报告只作为 execution log 文本保存。
- 不把生产发布简化为普通开发环境部署按钮。
- 不要求第一阶段支持所有发布策略，先稳定应用、环境、服务、容器、DAG 和交付物主线。
