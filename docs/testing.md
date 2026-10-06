# 后端质量验收

普通 Go 验证沿用仓库 AGENTS/CI 的 `GOWORK=off`、test/race/vet/复杂度/漏洞/lint/构建入口，
Web/Midscene 不成为 Go 测试依赖。公开协议仍由 contracts 所有，无协议变更不改 SDK/生成物。

`python3 scripts/run-deletion-integration.py` 复用已有 deletion-integration 测试，
创建独立 pgvector:0.8.5-pg18-trixie 容器，动态127.0.0.1端口、run label/ID/image验证、
独立 test-only账号，实际 `-race -count=1 -json` 运行父测试及16个必要子场景。
finally只清理核对归属的精确容器，绝不复用/清理开发 PG。
`.tmp/quality/deletion-test.json` 和 manifest 记录实际 core SHA、环境、运行/清理结论。

`python3 -m unittest discover -s scripts -p 'test_go_test_evidence.py'` 验证 no-skip 反例；
`check-go-test-evidence.py <json> --exit-code <actual>` 不以包PASS代替目标运行，拒绝空结果、
无run、skip/fail、损坏JSON、缓存、缺子场景、非零退出和不完整结束。CI继续使用既有PG service。

状态机用例位于 `internal/application/execution`，取消需要runtime确认，超时/重试/迟到回调
沿用已有行为。Agent fake runner race 与真实 runner/cluster分别报告；不因假控制平面PASS宣布上线。
HTTP黑盒/真实UI归 Web real入口；数据库与持久化冲突/副作用归本仓，不让Web直接SQL。

安全和OCR的实际用法见 [OCR与信任边界](../.agents/skills/soha-security/references/ocr.md)。

## 专用真实验收环境

`scripts/quality-lab.py` 仅用于明确获准的专用 middleware 环境，不复用开发库或用户认证。
工作区 Web 先执行 `npm run build`，再按顺序运行：

```bash
python3 scripts/quality-lab.py build
python3 scripts/quality-lab.py cluster
python3 scripts/quality-lab.py start
python3 scripts/quality-lab.py provision
python3 scripts/quality-lab.py seed
python3 scripts/quality-lab.py test --mode direct --role readonly
python3 scripts/quality-lab.py test --mode direct --role writer
python3 scripts/quality-lab.py test --mode agent --role readonly
python3 scripts/quality-lab.py test --mode agent --role writer
python3 scripts/quality-lab.py test --mode direct --role writer --flow
python3 scripts/quality-lab.py test --mode agent --role writer --flow
```

build 使用 npm pack 的 contracts 产物、临时 modfile 和真实 Core/Agent Dockerfile；
正式依赖 pin 不变。cluster 使用已核实的固定 K3s 镜像、新卷和远端回环动态端口，
middleware 的 SSH 入口需已存在；不修改 SSH、全局工具或 host sysctl。
provision 创建四个角色/用户及精确 cluster 的 scope grant/ABAC 策略，Agent 仅允许 CRD 删除动作，
Kubernetes RBAC 仅授予定义读取/删除及节点/命名空间读取，不挂载 Docker socket。

`.tmp/quality-lab/private.json` 和环境/证书文件仅本机 0600，目录 0700。脚本通过进程环境传入
登录密码，结果 manifest 不含凭据。禁止打印或上传该目录。`seed` 创建本次 CRD/实例并记录 UID；
Git/Docker忽略该私有目录，源码打包另外无条件排除所有`.tmp`，避免认证进入构建输入。
认证API及Kubernetes请求拒绝重定向；测试包装器拒绝缺失、旧结果、skip或身份不符的manifest。
同名替换使用 Kubernetes UID precondition，校验 run/id/UID 不符就拒绝。flow 会删除本次 CRD，
所以验收成功后不能直接重跑同一 lease；先核验旧对象已消失，再准备新的专用 lease。
环境保留供后续验收，停止或移除时仍须核对完整容器 ID/run label/镜像和精确卷归属。
`python3 scripts/quality-lab.py stop` 按上述身份校验销毁本次独立环境和隧道；
停止后状态不可复用，新建前仅归档无凭据结果并移除本次私有目录，不清理共享工具缓存。

`python3 -m unittest discover -s scripts -p test_quality_lab.py` 验证状态/归属反例。
这些测试和新环境不证明已发布 SDK 组合或 GitHub CI 通过。
