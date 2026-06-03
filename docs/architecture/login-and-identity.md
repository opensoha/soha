# 登录与身份链路

## 目标

`soha` 的登录链路现在分成两层：

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
- `roleField`
- `organizationField`
- `syncRolesOnLogin`
- `syncOrgsOnLogin`
- `roleSyncMode`
- `orgSyncMode`

## 登录时角色与组织映射

OIDC、OAuth2、飞书、钉钉、企业微信这几类可运行 provider 现在支持“登录时补充绑定”：

- `roleField` 从 id token claims 或 userinfo/profile 响应中读取外部角色引用
- `organizationField` 从 id token claims 或 userinfo/profile 响应中读取外部组织引用
- `syncRolesOnLogin` 控制是否在登录成功后补充本地角色绑定
- `syncOrgsOnLogin` 控制是否在登录成功后补充本地组织绑定
- `roleSyncMode` 和 `orgSyncMode` 支持 `append` 与 `replace_external`

`append` 只追加本次解析到的本地角色或组织绑定。`replace_external` 会先清理同一 provider 上次写入的外部来源绑定，再写入本次解析结果；它不会删除管理员在用户页面手工维护的本地绑定。

角色解析只匹配本地已存在的 `roles.id` 或 `roles.name`。组织解析只匹配本地已存在的组织：

- `teams.id`
- `teams.slug`
- `teams.org_path`
- `teams.source` 等于 provider type 或 provider id 且 `teams.external_id` 等于外部组织引用

登录回调不会自动创建本地组织或角色。管理员需要先在“组织架构”中建立组织树，并为第三方组织填入对应来源和外部 ID。这样可以把第三方组织目录的实时同步与登录授权映射拆开，避免登录链路变成隐式主数据导入。

飞书、钉钉、企业微信的 App Key、App Secret、CorpID、CorpSecret 等应用凭据属于“登陆设置”的登录源应用配置，不属于组织节点。组织节点只保存映射来源和第三方部门/组织 ID；如果同一种 provider 配了多个应用，组织来源应优先选择具体登录源应用 ID，而不是只选择 `feishu`、`dingtalk` 或 `wecom` 类型。

当前实现不是飞书、钉钉、企业微信通讯录的全量同步或事件订阅。第三方目录同步、组织变更 webhook、离职禁用同步等应作为后续 connector 能力设计；本阶段只在用户成功登录时根据 provider 返回的数据补充本地授权上下文。

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

## 浏览器会话与 token 存储

浏览器控制台现在按 BFF 方向收敛：

- `accessToken` 只保存在前端内存态，用于普通 API 请求的 `Authorization: Bearer ...`
- `refreshToken` 由后端写入 `HttpOnly` cookie，前端不再持久化或读取 refresh token
- `/api/v1/auth/refresh` 支持空 body，优先从 refresh cookie 读取会话并续写 refresh cookie
- `/api/v1/auth/logout` 会清除 refresh cookie，前端退出登录必须调用该接口后再清本地状态
- 开发环境默认通过 Vite `/api` proxy 访问同源 `/api/v1`，避免 refresh cookie 在跨站请求里失效

页面刷新后如果内存态 `accessToken` 不存在，受保护路由会先尝试调用 refresh 接口恢复会话。恢复失败才跳转登录页。

浏览器不能给原生 `WebSocket`、`EventSource` 和 noVNC 连接稳定附加 `Authorization` header。Console 需要打开 Pod logs stream、Pod terminal、虚拟化 operation stream 或 VNC/noVNC 时，必须先用当前 session access token 调用 `POST /api/v1/auth/stream-ticket` 换取短期一次性票据，再把 `stream_ticket` 放到同源 stream URL query。服务端只接受白名单路径，票据 60 秒过期、精确绑定 path，并在第一次校验时从 PostgreSQL 消费删除；票据消费后还会校验 session 状态和 `authz_version`，所以角色、组织和账号状态变更仍能让后续流式连接失效。

## 机器调用与 runner 令牌

浏览器登录和机器调用走不同边界：

- 浏览器控制台使用 Soha session access/refresh token，并通过服务端 session 和 `authz_version` 让角色、组织、账号状态变更可触发刷新
- API、CI runner、Docker runner、AI agent runner、AI Gateway client 使用 Bearer token
- 长期机器 token 应收敛到 AI Gateway 已有的 PAT/SAT 形态：token 只保存 hash，主体是用户或 service account，权限通过 `permissionKeys` 和 scope 限制

当前过渡实现保持 `runtime.execution_runner_token` 兼容，同时允许外部 runner 入口接受 `service_account_token`：

- Delivery execution runner 需要 `delivery.execution-tasks.manage`
- Docker runner 需要 `docker.operations.manage`
- AI agent runner 需要 `ai.gateway.invoke` 或 `observe.ai.chat`

任务级 callback token 仍然由对应执行任务或 agent run 自己校验。SAT 只替代“谁可以 claim/status/callback 这个 runner 通道”的全局共享入口令牌，不替代任务级防重放和回调归属校验。

### 1. 登录页展示

登录页先读取 `/api/v1/auth/providers`。

返回结果现在包含：

- `id`
- `type`
- `name`
- `enabled`
- `loginUrl`

前端会展示所有启用的第三方登录源，而不是只筛选 OIDC。

登录页还会读取 `/api/v1/auth/login-options`。当配置文件里的
`auth.login_verification.slider_enabled` 为 `true` 时，前端会在密码登录前展示滑块动作。

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
- 若开启 `syncRolesOnLogin`，则 `defaultRoles` 和 `roleField` 解析结果都会作为该 provider 管理的外部来源角色补充
- 若开启 `syncOrgsOnLogin`，则 `organizationField` 解析结果会作为该 provider 管理的外部来源组织补充

审计仍由平台统一记录，provider 只负责身份来源，不直接决定授权。

## 前端与权限边界

“登陆设置” 只是配置面，不直接绕过现有权限模型。

仍然使用：

- `settings.identity.view`
- `settings.identity.manage`

登录成功后的可见菜单、可见路由、可调用 API，仍由本地权限快照和后端鉴权决定，而不是由外部 IdP 直接决定。
