# macOS 原生 VPN Docker 联调

这个本地实验网运行独立的 Core、Network Control、Ingest、两个 PostgreSQL、一个 WireGuard 网关和两个 HTTP 资源。所有宿主监听端口绑定 `127.0.0.1`，不会复用现有 Soha 容器或数据库。

实验网验证 Mac → 原生 utun → WireGuard 网关 → 私有 HTTP/DNS 的真实路径。单网关足以验证原生组件；跨站点 A/B/C hub-spoke 仍使用主 README 的多站点测试部署。

## 构建和启动

先构建当前工作区代码。这里用缓存运行时镜像挂载新二进制，属于本地联调，不代替发行镜像构建验收。需要已有 `soha-vpn-implementation:test`、`soha-vpn-gateway:test` 镜像；也可把脚本中的镜像名改为按 `deploy/Dockerfile` 构建的对应 runtime target。

```sh
# 在 soha 仓库执行；显式使用 sibling contracts。
mkdir -p /private/tmp/soha-vpn-build
printf 'go 1.26.6\n\nuse %s\n\nreplace github.com/opensoha/soha-contracts => %s\n' "$PWD" "$(cd ../soha-contracts && pwd)" > /private/tmp/soha-vpn-build/go.work
GOWORK=/private/tmp/soha-vpn-build/go.work GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags embedassets -o /private/tmp/soha-vpn-build/lab-server ./cmd/server
GOWORK=/private/tmp/soha-vpn-build/go.work GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /private/tmp/soha-vpn-build/lab-control ./cmd/network-control
GOWORK=/private/tmp/soha-vpn-build/go.work GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /private/tmp/soha-vpn-build/lab-ingest ./cmd/ingest
GOWORK=/private/tmp/soha-vpn-build/go.work GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /private/tmp/soha-vpn-build/lab-gateway ./cmd/network-gateway
python3 deploy/network-test/vpn-lab.py up
```

先启动一次 Soha App，脚本会读取它保存的 `device-id`，通过管理 API 注册此设备、准入策略、连接方案及短期 enrollment。不会直接写数据库。凭据保存在 `~/.local/share/opensoha-vpn-lab/`，有效期七天的实验 CA 不安装进系统信任库。

管理台：<http://127.0.0.1:18080>。账号 `opensoha@soha.local`；随机密码在 `~/.local/share/opensoha-vpn-lab/admin-password`。

## 安装本机组件

先按 soha-app README 构建并安装带 helper 的 App。以下命令在本机终端执行，需要 macOS 管理员密码；不要把密码发到聊天或写进脚本。

```sh
# enrollment 只在十分钟内有效。首次安装前刷新实验配置：
python3 deploy/network-test/vpn-lab.py seed
sudo /bin/sh /Applications/Soha.app/Contents/Resources/install-network-service.sh \
  "$HOME/.local/share/opensoha-vpn-lab/endpoint-provision"
```

安装器拒绝覆盖已配置的设备身份。常规 helper 更新不传 provisioning 目录：

```sh
sudo /bin/sh /Applications/Soha.app/Contents/Resources/install-network-service.sh
```

在 App「设置」把服务器切到 `http://127.0.0.1:18080`，使用实验账号登录。进入 VPN，应看到组织分配的“macOS 原生 VPN · Docker 实验网”，支持 Auto 和手动选择实验入口。

## 验证顺序

1. **未连接时**：下列 HTTP 命令应超时。资源地址放在容器 loopback，不在 Docker 自动添加到 Mac 的 `10.252.240.0/24` 网段中。
2. **点击连接**：App 必须在真实握手、路由及 DNS readback 成功后显示“已连接”。执行同一个 HTTP 命令，应返回 `Soha VPN lab: real private resource reached`。
3. **DNS**：系统解析 `vpn-lab.soha.test` 应得到 `10.252.250.10`，通过域名访问也应成功。不要用 `dig @服务器` 代替系统 DNS 验证。
4. **ProtectedSet**：`10.252.250.11:8000` 是标记为受保护的资源，即便获得整个 `/24` 网络授权也应失败。
5. **断开**：再次访问 `.10` 应失败；utun 和本次临时 DNS 配置应消失，普通上网继续正常。
6. **遥测**：管理台「内网中心 → VPN → 运行看板」检查实际会话、入口选择原因和传输量。未采集的数据应为空，不应显示伪造的零值。

```sh
curl --noproxy '*' --connect-timeout 2 --max-time 5 http://10.252.250.10:8000
dscacheutil -q host -a name vpn-lab.soha.test
curl --noproxy '*' --connect-timeout 2 --max-time 5 http://vpn-lab.soha.test:8000
curl --noproxy '*' --connect-timeout 2 --max-time 5 http://10.252.250.11:8000
route -n get 10.252.250.10
scutil --dns
```

如果网络冲突，服务会拒绝连接；不要删除其他 VPN、办公网或 Docker 的路由来强行通过。

## 状态与停止

```sh
python3 deploy/network-test/vpn-lab.py status
docker compose -p soha-macos-vpn-lab -f "$HOME/.local/share/opensoha-vpn-lab/compose.json" logs --tail 30 control gateway-a
sudo tail -30 /var/db/opensoha/network-service/service.log
# 先在 App 断开，再停止实验网；保留实验数据和凭据。
python3 deploy/network-test/vpn-lab.py down
# 不再使用本机 helper 时，管理员卸载其 launchd 作业：
sudo launchctl bootout system /Library/LaunchDaemons/com.opensoha.network-service.plist
```

停止容器不等于卸载本机组件。需要彻底卸载时，先确认已断开并停止 launchd，再由管理员移除对应 helper/plist；设备私钥和配置应按设备退役流程处理。
