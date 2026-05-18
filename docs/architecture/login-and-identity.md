# 登录与身份链路

## 目标

`kubecrux` 的登录链路现在分成两层：

- 本地账号密码登录
- 多第三方登录源聚合登录

当前第三方登录源模型支持：

- `oidc`
- `feishu`
- `dingtalk`
- `wecom`
- `oauth2`
- `saml`

这里的关键设计目标不是“支持一种企业单点登录”，而是让控制台同时持有多套企业身份入口，并且由登录页按启用状态并列展示。

## 配置模型

后端设置服务将登录配置聚合在 `identity.login_providers` 里，结构上包含：

- `providers[]`
- `defaultProviderId`

每个 provider 至少包含：

- `id`
- `name`
- `type`
- `enabled`
- `redirectUrl`
- `frontendRedirectUrl`

针对 OAuth/OIDC 类 provider，还包含：

- `clientId`
- `clientSecret`
- `issuer` 或 `authorizeUrl`
- `tokenUrl`
- `userInfoUrl`
- `scopes`
- `defaultRoles`
- `userIdField`
- `userNameField`
- `emailField`

针对 SAML 类 provider，目前只保存配置态字段，例如：

- `metadataUrl`
- `entityId`
- `certificate`

## 向后兼容

历史单 OIDC 配置仍保留在 `identity.oidc`。

当前兼容规则：

- 多 provider 设置是新的配置源
- 运行时解析 OIDC 时，优先从多 provider 中选默认或首个可用 `oidc`
- 更新多 provider 时，会回写兼容态 `identity.oidc`
- 如果库里还只有旧 OIDC 配置，设置服务会在读取时投影出一个默认 `oidc` provider

这样做的原因是：

- 不破坏旧配置文件和已有数据库内容
- 不要求一次性迁移所有现网部署
- 允许现有 OIDC 浏览器回调链路继续工作

## 运行链路

### 1. 登录页展示

登录页先读取 `/api/v1/auth/providers`。

返回结果现在包含：

- `id`
- `type`
- `name`
- `enabled`
- `loginUrl`

前端会展示所有启用的第三方登录源，而不是只筛选 OIDC。

### 2. 浏览器跳转

每个 provider 的登录入口是：

- `GET /api/v1/auth/login/:providerID/start`

兼容入口仍保留：

- `GET /api/v1/auth/oidc/login`

### 3. 回调处理

统一 provider 回调入口：

- `GET /api/v1/auth/login/:providerID/callback`

兼容 OIDC 回调入口仍保留：

- `GET /api/v1/auth/oidc/callback`

后端在回调阶段做：

- 校验 state
- 解析 provider 类型
- 交换授权码
- 拉取或构造用户资料
- 绑定或创建本地用户
- 写入 `user_identities`
- 创建 session
- 生成一次性 exchange code
- 跳回 `/login/callback?code=...`

### 4. 前端换取会话

前端回调页统一调用：

- `POST /api/v1/auth/oidc/exchange`

这个接口现在不只服务 OIDC，也承载 OAuth2 类登录的最终会话交换。

## 当前 provider 状态

### OIDC

运行完整。

- 发现 issuer
- 交换 code
- 校验 id token
- 按 claims 或 userinfo 补齐用户信息

### Generic OAuth2

运行完整，前提是 operator 提供可用的：

- `authorizeUrl`
- `tokenUrl`
- `userInfoUrl`
- 字段映射

### Feishu

当前按飞书开放平台授权码链路走专用 token 交换，再取用户信息。

它依赖 operator 校准：

- app 凭证
- 回调地址
- 用户资料字段映射

默认预置的是一套常见端点，不代表覆盖所有飞书应用形态。

### DingTalk

当前走 OAuth2 授权码和 access token 交换，用户信息拉取依赖 operator 提供的开放平台应用可访问接口。

钉钉开放平台不同应用形态的用户资料接口差异较大，所以这里按“可配置 provider”处理，而不是把字段和 URL 强行写死成唯一标准。

### WeCom

企业微信不是标准通用 OAuth2 token 交换模型。

当前实现采用：

- 网页授权拿 `code`
- 服务端用 corp secret 取企业 access token
- 再用 `code + access_token` 取 `UserId`

因此：

- `tokenUrl` 配置的是企业 access token 获取地址
- `userInfoUrl` 配置的是 `getuserinfo` 地址
- `clientId` 实际对应 `corpid`
- `clientSecret` 实际对应 `corpsecret`

当前默认只稳定拿到企业内 `UserId`，不保证能直接拿到邮箱或显示名；这些字段是否可补齐，取决于 operator 是否提供后续用户资料接口和映射。

### SAML

当前仅配置可见，不是完整可运行链路。

已支持：

- 设置页配置
- 配置持久化
- 登录页展示占位
- 能明确告诉用户当前链路未启用

未支持：

- SP metadata 生成
- ACS endpoint
- assertion 校验
- nameID / attribute statement 解析
- SAML 到本地用户的正式映射

所以当前服务端必须把 SAML 视为“配置态能力”，不能对外宣称已经具备可用登录能力。

## 审计与本地身份绑定

无论第三方 provider 类型是什么，最终都要落到同一套本地身份体系：

- 本地 `users`
- 本地 `roles`
- 本地 `sessions`
- 本地 `user_identities`

第三方登录第一次成功时：

- 优先按 `provider_type + provider_id + provider_user_id` 查历史绑定
- 没有绑定时按邮箱尝试合并已有用户
- 再没有时创建新本地用户
- 若该用户无角色且 provider 配置了 `defaultRoles`，则绑定默认角色

审计仍由平台统一记录，provider 只负责身份来源，不直接决定授权。

## 前端与权限边界

“登陆设置” 只是配置面，不直接绕过现有权限模型。

仍然使用：

- `settings.identity.view`
- `settings.identity.manage`

登录成功后的可见菜单、可见路由、可调用 API，仍由本地权限快照和后端鉴权决定，而不是由外部 IdP 直接决定。
