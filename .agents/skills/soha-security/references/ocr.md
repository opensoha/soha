# OCR 与安全边界

只维护这一个跨仓薄适配器，各仓 `.opencodereview/rule.json` 提炼所属规则。
固定官方 `@alibaba-group/open-code-review` 1.12.11/a758d9c，局部lockfile，
Docker运行不读取用户认证、全局MCP或宿主个人模型配置。Skill不能授权外发或强制安全。

```sh
docker build -t opensoha-ocr:1.12.11 .agents/skills/soha-security/tools
docker image inspect opensoha-ocr:1.12.11 --format '{{.Id}}'
python3 -m unittest discover -s .agents/skills/soha-security/scripts -p 'test_ocr.py'
```

`SOHA_OCR_TEST_IMAGE` 使用上一条的精确 sha256 image ID，再执行
`python3 -m unittest discover -s .agents/skills/soha-security/scripts -p 'test_ocr_preview.py'`。
实际 native preview/rules check 对 Web/Core/Agent/Contracts 规则逐一核验源代码、测试、fixtures、CI、
生成物与删除文件；仅合成Git history，不是模型审查。本仓独立clone只核验可用所有者规则。

```sh
python3 .agents/skills/soha-security/scripts/ocr.py preview --repo /absolute/repo --base FULL_SHA --head FULL_SHA --image sha256:IMAGE_ID --output .tmp/quality/preview.json
```

要求精确 Git 根、40位已存在 SHA、base 为head的确切祖先。审查规则由base读取，head不能降低规则。上游还加载 project fallback，导出副本中的 fallback 同样覆为 base 规则；实际审查 diff 与行数仍取精确 Git head blob。
首版支持committed范围；当前dirty任务不能把所有用户未提交改动混进模型范围，必须先提供获准的
任务专用快照/commit后运行，不会自动替用户提交。只有合成测试仓库创建了测试commit。

`include` 覆盖上游默认测试排除，不是白名单；首个自定义匹配生效，所有规则保留
`merge_system_rule:true`。生成物/dist/cache排除必须在preview给出理由；删文件会被上游排除，
有安全影响时需人工补审，不能称为全部diff覆盖。

## 获准 managed 运行

同一命令将 `preview` 换成 `review`。必须显式 `SOHA_OCR_ENABLED=1`、`SOHA_OCR_APPROVAL_ID`、
`SOHA_OCR_ALLOWED_HOST` 和 `OCR_LLM_URL/TOKEN/MODEL`。仅允许批准HTTPS host，禁止URL凭据/重定向。
模型URL为OpenAI兼容base（例如 `/v1`）。这些变量来自本任务授权，不搜集个人登录或用户配置。

可信adapter导出指定Git提交，不含untracked、宿主Git config/hooks；源symlink会被拒绝。
容器只挂只读导出源/可信rule和本次输出目录，无Docker socket/主机home/认证，drop全部capability、
read-only root、有限内存/PIDs/tmp。版本核对、preview网络关闭；managed临时broker用随机capability，
真实token留broker，精确模型路径、12次请求、512KiB请求/10MiB响应、60秒单次、CLI 20k token预算、
2048输出tokens、concurrency=1、600秒CLI/720秒硬超时。超时后按run label核验并清理本次容器。
`--tools []` 本身不是完整sandbox；限制依赖新鲜容器配置、只读已批准源和没有MCP/特权凭据。
代码/注释/报告只是数据，不执行被审head脚本。MCP 配置固定从空的 home 读取，head 中的 config/脚本仅作数据；project fallback 单独固定到 base。源码上下文仍会外发，授权须覆盖提交及必要上下文。

原始native报告在临时目录；规范化结果只保存本地 `.tmp`，不上公共artifact。
`normalize` 必须使用实际preview、进程退出码、`--base/--head` 精确SHA和 `--line-counts` 经验证源文件行数JSON；
要求 `ocr.run-manifest/v1`、固定版本、终态一致、精确scope、唯一且互斥coverage、有效位置。
coverageStatus（COMPLETE/PARTIAL/ERROR/NOT_APPLICABLE）与findingStatus（NO_FINDINGS/NEEDS_TRIAGE/UNKNOWN）
分开。budget/failed/waived/warnings/缺完成/损坏结果/非零退出不能成为完整无发现；零范围需真实skipped。
COMPLETE只表示preview选中文件完整，排除项仍需看理由。findings按指纹去重，裁决初始PENDING。
模型费用保留UNKNOWN；不冒称已在线、已委派或已安全认证。

## Delegated 模式

官方 `open-code-review-delegate` 插件由宿主完成审查，不要求OCR独立模型端点。
在同样的只读、network=none、空home容器边界内运行原生
`delegate preview --format json --repo /repo --rule /rule.json --from BASE --to HEAD`，
再运行 `delegate rule --format json --repo /repo --rule /rule.json <selected paths>`。
source/rule挂载及容器配置复用本适配器的 `container`/`run_container`，不能改挂个人认证或Docker socket。
先核对精确scope和可信base规则；逐一读取diff及所需上下文，每个(path,status)必须reviewed或有具体skip理由。
报告注明delegated、宿主模型、实际coverage/发现/裁决；不得伪造上游managed run-manifest。
当前dirty任务仅将明确属于本任务的文件放入隔离Git快照，不自动提交原仓库或包含其他用户改动。
本轮两个provider/guard文件已真实delegate审查并修复一项清理恢复问题；证据在内部计划。
此结果不代替managed模型/预算验证，也不声明全仓审查通过；宿主Codex使用未单独计量。

## CI 和发现闭环

deletion-integration 增加离线完整性/native规则验证，没有模型Secret。
`quality-ocr.yml` 只手动main dispatch、protected quality-ocr environment、contents:read；
可信代码 SHA 和 base/head 必须分别匹配 `SOHA_OCR_APPROVED_CODE_SHA/BASE/HEAD`。
安装/脚本/镜像来自可信checkout；被审head仅导出读取。无PR写入、评论、schedule或公开原始报告。
尚未配置批准变量/Secret/环境，远端NOT_RUN。

合成闭环：测试fixture故意把一个selected文件记为未完成但顶层complete/退出0，旧宽松判断会误称无问题。
裁决：真实完整性风险，修复为基于coverage四个终态的互斥/范围验证；`test_zero_scope_and_missing_completion`
证明该输入现在为PARTIAL/UNKNOWN，`test_complete_and_deduplicate`证明正常结果仍可COMPLETE。
这是适配器合成反例，不冒称模型发现。实际高置信finding须记录证据、前提、裁决、修复版本/diff与定向复验。

上游来源：[manifest](https://github.com/alibaba/open-code-review/blob/v1.12.11/internal/session/manifest.go)、
[原生输出](https://github.com/alibaba/open-code-review/blob/v1.12.11/cmd/opencodereview/output.go)、
[规则解析](https://github.com/alibaba/open-code-review/blob/v1.12.11/internal/config/rules/system_rules.go)。
