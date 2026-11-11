# Web 验收入口

先按改动影响选择入口。开发迭代由实现者定向自测；独立测试和只读审查在约定功能批次完成、
准备正式提交/合入或用户明确要求时进行。局部文案、样式和页面调整不默认运行全量 Vitest、
Playwright、稳定性重复测试或构建；提交动作本身不扩大范围。新增/重构页面及复杂权限、状态、
重试和集成行为应覆盖对应成功、失败和边界场景，按需在实现前或早期设计。真实 CI、共享影响、
架构、依赖和发布门禁仍保留，以下定向入口不代替阶段必需检查。

本地命令遵循工作区 `opensoha-verify run --` 入口；路径替换为实际受影响文件：

| 范围 | 入口与边界 |
| --- | --- |
| 相关单元/组件行为 | `npm test -- <spec-path>`；确认实际收集了目标用例，零匹配不算通过 |
| 相关源码 Lint | `./node_modules/.bin/eslint <changed-path> --max-warnings=0`；`npm run lint` 内含 `eslint .`，追加路径仍会扫全仓 |
| 相关浏览器场景 | `npm run test:e2e -- e2e/<spec-path>`；需要当前受测版本对应的构建产物与浏览器，记录产物来源 |
| E2E 源码/fixtures 改动 | `npm run typecheck:e2e`，再选相关 specs；guard 行为变更加 `npm run test:guards` |
| 适用的完整浏览器回归 | `npm run typecheck:e2e && npm run test:guards && npm run test:e2e`；按实际影响或必需门禁执行 |

浏览器入口依赖 `npm run build` 的 preview 产物；源码变化后先更新对应构建，避免验证旧页面。
缺依赖/浏览器时才按项目入口准备 `npm ci` / `npx playwright install chromium`，不每轮重装。
`npm run test:e2e:stable` 连续3次、retries=0，用于建立稳定性证据或调查不稳定行为，不是每轮必跑。
Vitest 排除 `e2e/**`，普通 Playwright 只收集 `ui-mock/` 与 `visual/`，不加载 Midscene。
现有 lint、格式、覆盖率、route、boundary、bundle 与 release 门禁继续由 package/CI 维护。

## 并行与环境所有权

主代理在现有计划/对话指定受测提交/快照/产物、目标、资源身份、读写/独占方式与准备/清理责任人。
测试和只读审查可使用同一稳定候选，验收期间不修改其源码、依赖或产物；后续开发隔离或错开时段。

所有 npm 浏览器入口经 `scripts/run-e2e.mjs`，在 Playwright 加载配置和清理输出前取得资源。
设置 `SOHA_E2E_TASK_ID` 后，每次自动生成 `SOHA_E2E_EXECUTION_ID`，普通/AI 自动分配 loopback
端口；也可显式指定 `SOHA_E2E_PORT`（1024–65535）。显式端口被占用即失败，strictPort 和禁止
复用已有 server 保留。任务/执行 ID 只能为1–64位字母数字、下划线或连字符，首位为字母数字。

在 Web checkout 中运行单项定向任务：

```sh
rtk proxy env SOHA_E2E_TASK_ID=task-a opensoha-verify run -- \
  npm run test:e2e -- e2e/ui-mock/baseline.spec.ts
```

本机容量锁仍串行管理独立验证调用。需要同时运行多个任务时，由主代理在一个验证批次内调度，
最多同时运行两个浏览器进程，其余排队；不绕过容量锁或给每个任务复制工具缓存：

```sh
rtk proxy opensoha-verify run -- npm run test:e2e:parallel -- \
  task-a=e2e/ui-mock/baseline.spec.ts task-b=e2e/ui-mock/crd.spec.ts
```

报告根为 `test-results/tasks/<taskId>/<executionId>/`，manifest、JSON、附件/截图、AI摘要和
Midscene 报告均归本次执行；目录已存在时拒绝复用。需要调用方定位报告时，可提供新的执行 ID，
不得重用旧证据目录。`--list` 只列用例，不创建/改写报告或取得端口。配置、reporter 与输出由入口
统一管理，不接受低层 `--config/--reporter/--output` 覆盖；直接运行 Playwright 不能代替此入口。
未设置任务 ID 时保留旧4179端口与报告路径，但同一 checkout 使用独占锁，仅供串行兼容。

mock 数据和 localStorage/会话在每测试独立 context/fixture 内，合成资源可同名；严格出站检查只
允许本次 origin，另一任务的 localhost 端口也不放行。真实测试必须提供各自批准的独立环境、账号
及数据；按真实 runId/容器身份互斥，指向同环境的不同 target 文件不能并行。direct/Agent 可能共用
底层集群，CRD为集群级资源，不以 namespace 或 clusterId 不同冒充数据隔离；它们保留 workers=1、
retries=0 和已有顺序。Core quality-lab 每次传入独立任务/执行 ID 并只读取自己的报告。

构建 `dist` 在整个批次中保持不变；不同源码版本使用隔离 checkout，先构建再验收，不在测试中重建
共享产物。报告分别记录测试源码证据及 index/Vite manifest 摘要；摘要只标识实际产物，不证明它由
当前 checkout 构建。构建失败时复用已有产物，只能证明明确限定的测试基础设施行为，不能代替页面
变更验收。运行前核对资源占用，不抢端口、覆盖会话或杀他人服务。SIGINT/SIGTERM 只停止本轮子进程并释放自己的资源占用，报告保留；异常退出留下的
锁不自动抢占。锁位于系统临时目录的 `soha-e2e-<uid>/`，失败提示给出准确 owner 路径；确认该 PID
及其子进程均已结束、身份属于本次后才按准确路径恢复，禁止按名称前缀批量删锁或杀进程。

仅清理本轮创建且已无人使用的准确资源；中断、清理失败或仍被使用时交回资源身份与责任。
环境或输入变化只使受影响证据失效，修复后复验相关项；不因测试代理数量增加而重复同一套验收。

修改并行入口本身时，可定向运行 `node scripts/check-e2e-parallel.mjs`（双进程隔离）和
`node scripts/check-e2e-lifecycle.mjs`（零用例、退出及占用回收）；两者同样放在
`opensoha-verify run --` 下执行。普通页面小改动只选择相关 spec，不附带运行这两套基础设施检查。

## 浏览器覆盖与证据范围

旧 `browser:baseline` 已切换到 Playwright。`baseline.spec.ts` 保留集群列表文本/版本、
Deployment 深链/正文、namespace-scoped 请求、非空 root、fatal console、网络失败和 missing mock
检查；guards 注入未知 endpoint、错误 namespace、空 root 与外部 origin。旧 CDP 引擎可从
实施前 commit `c77b85ee88ca039e1d1383c3faec4fadac87ae0d` 查看，不长期运行两套引擎。

CRD 目录是完整本地数据：分页/搜索/排序测试断言显示结果，并证明没有假造服务端排序参数。
鼠标和键盘独立验证；删除测试校验精确对象/UID、确认禁用、失败上下文、重试和重复提交。
这些是 **ui-mock** 证据，不能证明服务端权限或真实 Kubernetes 删除。CRD 双击请求缺陷已在
提交入口修复，并由该浏览器回归验证。

几何检查覆盖 light/dark、1440/1280 桌面宽度、长名称、遮挡和原生 select/popup；局部表格滚动
允许存在，全页溢出和操作遮挡不允许。截图是候选证据，没有自动认可的视觉基线。

## 真实目标

`npm run test:api:real` 与 `npm run test:flow:real` 使用独立配置，不启动 mock。
`SOHA_E2E_TARGET_FILE` 指向本地私有 JSON；缺失会写 BLOCKED 并失败。要求：

- `mode`: `e2e-direct`/`e2e-agent`；`baseURL` 必须匹配本次 Docker 容器 loopback 端口。
- `disposable: true`、`runId`、完整 `containerId`/`imageDigest`、`approvalId`。
- `sources.web/core/contracts`（Agent模式另含agent）：精确40位 commit 与64位 artifactDigest；
  容器标签 `soha.test.<name>-sha`/`soha.test.<name>-artifact` 必须一致。这些标签由可信环境提供者
  从实际构建产物设置，不能自行填假摘要。测试代码来源与被测产物来源应分别记录。
- `role`: readonly/test-writer；`clusterId`/`namespace`，A03另需提供本专用环境中的 `deniedClusterId`；`loginEnv`/`passwordEnv` 引用
  显式 `SOHA_E2E_*` 环境变量。UI 登录后独立读取服务端 permission snapshot 核验角色。
- 负向删除和写入需 `lease: {runId,id,uid}`：环境提供者预创建的本次专用 CRD，集群也必须专用。
  CRD创建没有可复用公开 API，所以首版不新增测试后门；准备证据和精确 UID 来自环境提供者。
  UI写入还需 `SOHA_E2E_MUTATIONS=1`。测试不会以 API 绕过待测 UI 删除。

读/拒绝用例分别验证 UI 登录、真实目录、无权限403/错UID409以及前后状态相同。写入用例验证
UI请求与最终消失，并保持其他定义相同。异常时保留精确 lease，由提供者重新核验 UID 后清理；
没有按名称前缀批量删除或对共享集群回退。直连与 Agent 必须分两次提供各自版本/环境。
会话只在单个测试上下文使用，注销并销毁；无 storageState 文件或认证后门。

## Midscene

仅 `npm run test:e2e:ai` 启动 `playwright.ai.config.ts`，固定 @midscene/web 1.14.0。
显式 `SOHA_AI_ENABLED=1`、`SOHA_AI_APPROVAL_ID`、`SOHA_AI_ALLOWED_HOST`、`SOHA_AI_MAX_CALLS`（1..12），
以及 `MIDSCENE_MODEL_BASE_URL/API_KEY/NAME/FAMILY` 均需要本任务授权。无启用为 `NOT_RUN`；
启用但缺配置为 BLOCKED（非零退出）。模型不是当前宿主模型名，也不进入 `VITE_*`。

模型端点只接受 HTTPS 精确host；预算代理拒绝重定向、额外路径、超过12次请求/20MiB请求，
单次60秒。真实 key 只留代理，SDK拿随机临时 capability。直接运行 AI config 缺少受控代理标识会 BLOCKED；不能绕过预检调用模型。子进程不继承个人模型配置；页面网络另由
mock fixture 限制到本地 origin。两个场景只操作合成页面（只读组入口/人工遮挡），
结果另由 locator、API请求和几何断言验证。循环3次、workers=1、case180秒、job15分钟。
缓存关闭；forceSameTabNavigation/forceChromeSelectRendering/autoFollowNewPage 均为 false，
保持原生控件/新tab语义。普通 M03/M06 用例不依赖模型或缓存。

模型价格未配置，费用标为 UNKNOWN；请求数和 token预算边界不能冒称实际账单。
审计已对 Midscene 的 js-yaml/uuid/sharp 设置限定父依赖的修复版本；剩余 extract-zip
上游漏洞位于未调用的 Puppeteer 浏览器下载链。本入口使用固定 Playwright 浏览器，不调用该下载路径；
仍保留审计发现，升级或启用其他SDK能力时必须重审。无本任务模型授权时在线仍 BLOCKED。

## 证据与 CI

本次报告目录的 `run-manifest.json` 记录 taskId/executionId、来源 commit/dirty digest、模式、测试结果，
`playwright.json` 和失败截图仅保存合成数据。真实模式关闭截图/trace；不输出token/请求正文。
`test-results/`、`.auth/`、旧 `midscene_run/` 被忽略；新 Midscene 输出位于本次私有报告根，
原始 AI/认证/trace/HAR 不上传公共artifact。
本地报告按7天保留约定人工清理，不能当正式设计基线。CI只上传原有构建manifest。

真实环境可由 Core 的 [quality-lab 入口](../../soha/docs/testing.md) 准备，并由该脚本从私有文件
传入账号和 target。四个用户分别限定 direct/Agent 和只读/写入；真实 API 列表使用 `items`。
UI 登录按当前页面完成滑块验证，未预注入认证。logout 后访问401、无权删除403、错UID409、
同名重建后的旧UI确认409、客户端传输失败后合法重试及最终对象消失均独立校验。
catalog 状态比较保留所有持久字段，只排除自然递增的 ageSeconds。

固定 Playwright1.63 的失败 ARIA 快照会包含密码输入值。real config 在 worker 启动前设置
该版本原生 `PLAYWRIGHT_NO_COPY_PROMPT=1`；真实 screenshot/trace 仍关闭。
`npm run check:auth-artifacts` 实际触发隔离失败，确认快照和输出不含密码 canary。
这项原生变量依赖固定版本，升级 Playwright 时必须重跑反例。

基础 CI 没有模型secret依赖。增强 `quality-ai.yml` 只允许 main 的手动 dispatch、
protected `quality-ai` environment、匹配 `SOHA_APPROVED_AI_SHA` 与批准ID/host；固定 action SHA，
contents:read，不评论PR，不schedule/push收费调用。环境/Secret/批准变量尚未配置，工作流未激活或运行。

来源：[Playwright配置](https://playwright.dev/docs/test-configuration)、
[Midscene官方集成](https://midscenejs.com/integrate-with-playwright)。实际API以固定依赖声明为准。
