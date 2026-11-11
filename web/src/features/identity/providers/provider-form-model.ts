import type {
  IdentityOIDCClient,
  IdentityOIDCClientInput,
  IdentityOIDCRedirectMatchMode,
  IdentityOIDCClientStatus,
  IdentityOIDCClientType,
  IdentityProvider,
  IdentityProviderInput,
  IdentityRuntimeProviderStatus,
  IdentityRuntimeProviderType,
} from './types'

export type ProxyMode = 'forward_auth' | 'reverse_proxy'

export interface ProviderFormValues {
  applicationId: string
  configJson: string
  enabled: boolean
  name: string
  proxyCookieDomain: string
  proxyExternalHosts: string[]
  proxyHeaderEmail: string
  proxyHeaderGroups: string
  proxyHeaderRoles: string
  proxyHeaderTeams: string
  proxyHeaderUser: string
  proxyHeaderUserId: string
  proxyMode: ProxyMode
  proxyAllowPrivateUpstream: boolean
  proxyOutpostId: string
  proxyPathPrefix: string
  proxySkipAuthPaths: string[]
  proxyUpstreamUrl: string
  proxyWebsocketEnabled: boolean
  samlAcsUrls: string[]
  samlAttributeMappingsJson: string
  samlEntityId: string
  samlNameIdFormat: 'persistent' | 'transient' | 'emailAddress' | 'unspecified'
  samlSpCertificatePem: string
  samlWantAuthnRequestsSigned: boolean
  secretRefsJson: string
  status: IdentityRuntimeProviderStatus
  type: IdentityRuntimeProviderType
}

export interface OIDCClientFormValues {
  accessTokenTtlSeconds: number
  allowedGrantTypes: string[]
  allowedScopes: string[]
  clientId: string
  clientSecret: string
  clientType: IdentityOIDCClientType
  idTokenTtlSeconds: number
  postLogoutRedirectUris: string[]
  redirectRules: Array<{ mode: IdentityOIDCRedirectMatchMode; value: string }>
  refreshTokenTtlSeconds: number
  requirePkce: boolean
  status: IdentityOIDCClientStatus
}

export const oidcRedirectMatchModeOptions: Array<{
  label: string
  value: IdentityOIDCRedirectMatchMode
}> = [
  { label: '严格', value: 'strict' },
  { label: '正则表达式', value: 'regex' },
]

export const providerTypeOptions: Array<{
  label: string
  value: IdentityRuntimeProviderType
}> = [
  { label: 'OIDC', value: 'oidc' },
  { label: 'Proxy', value: 'proxy' },
  { label: 'SAML', value: 'saml' },
]

export const providerStatusOptions: Array<{
  label: string
  value: IdentityRuntimeProviderStatus
}> = [
  { label: 'Enabled', value: 'enabled' },
  { label: 'Disabled', value: 'disabled' },
]

export const proxyModeOptions: Array<{ label: string; value: ProxyMode }> = [
  { label: 'Forward auth', value: 'forward_auth' },
  { label: 'Reverse proxy', value: 'reverse_proxy' },
]

export const oidcClientStatusOptions: Array<{
  label: string
  value: IdentityOIDCClientStatus
}> = [
  { label: 'Enabled', value: 'enabled' },
  { label: 'Disabled', value: 'disabled' },
]

export const oidcClientTypeOptions: Array<{
  label: string
  value: IdentityOIDCClientType
}> = [
  { label: 'Confidential', value: 'confidential' },
  { label: 'Public', value: 'public' },
]

export const defaultScopes = ['openid', 'profile', 'email']
export const defaultGrantTypes = ['authorization_code']
export const oidcGrantTypeOptions = [
  { label: 'authorization_code', value: 'authorization_code' },
  { label: 'refresh_token', value: 'refresh_token' },
]
export const defaultProxyHeaders = {
  email: 'X-Soha-Email',
  groups: 'X-Soha-Groups',
  roles: 'X-Soha-Roles',
  teams: 'X-Soha-Teams',
  user: 'X-Soha-User',
  userId: 'X-Soha-User-ID',
  projects: 'X-Soha-Projects',
  tags: 'X-Soha-Tags',
}

export function proxyHeaderNamesFor(provider: IdentityProvider) {
  const mappings = configStringMap(provider.config, 'headerMappings', 'header_mappings')
  return [
    ...new Set([
      ...Object.values(defaultProxyHeaders),
      ...Object.entries(defaultProxyHeaders).map(
        ([claim, fallback]) => mappings[claim] || fallback,
      ),
    ]),
  ]
}

const knownProxyConfigKeys = [
  'cookieDomain',
  'cookie_domain',
  'externalHost',
  'externalHosts',
  'external_host',
  'external_hosts',
  'headerMappings',
  'header_mappings',
  'host',
  'hosts',
  'mode',
  'allowPrivateUpstream',
  'allow_private_upstream',
  'outpostId',
  'outpost_id',
  'pathPrefix',
  'path_prefix',
  'protectedPathPrefix',
  'protected_path_prefix',
  'skipAuthPaths',
  'skip_auth_paths',
  'upstreamURL',
  'upstreamUrl',
  'upstream_url',
  'websocketEnabled',
  'websocket_enabled',
]

export function compactStrings(values: string[] = []) {
  const seen = new Set<string>()
  const out: string[] = []
  values.forEach((value) => {
    const normalized = String(value ?? '').trim()
    if (!normalized || seen.has(normalized)) return
    seen.add(normalized)
    out.push(normalized)
  })
  return out
}

function jsonText(value?: Record<string, unknown>) {
  return JSON.stringify(value ?? {}, null, 2)
}

function parseRecordJSON(value: string, label: string): Record<string, unknown> {
  const text = String(value ?? '').trim()
  if (!text) return {}
  const parsed = JSON.parse(text) as unknown
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error(`${label} 必须是 JSON object`)
  }
  return parsed as Record<string, unknown>
}

function configString(config: Record<string, unknown> | undefined, ...keys: string[]) {
  for (const key of keys) {
    const value = config?.[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function configStringArray(config: Record<string, unknown> | undefined, ...keys: string[]) {
  const values: string[] = []
  keys.forEach((key) => {
    const value = config?.[key]
    if (typeof value === 'string') {
      values.push(...value.split(','))
      return
    }
    if (Array.isArray(value)) {
      value.forEach((item) => {
        if (typeof item === 'string') values.push(item)
      })
    }
  })
  return compactStrings(values)
}

function configBoolean(config: Record<string, unknown> | undefined, ...keys: string[]) {
  for (const key of keys) {
    const value = config?.[key]
    if (typeof value === 'boolean') return value
  }
  return false
}

function configStringMap(config: Record<string, unknown> | undefined, ...keys: string[]) {
  const out: Record<string, string> = {}
  keys.forEach((key) => {
    const value = config?.[key]
    if (!value || typeof value !== 'object' || Array.isArray(value)) return
    Object.entries(value as Record<string, unknown>).forEach(([itemKey, itemValue]) => {
      if (typeof itemValue === 'string') out[itemKey] = itemValue.trim()
    })
  })
  return out
}

function stripKnownProxyConfig(config?: Record<string, unknown>) {
  const out: Record<string, unknown> = { ...(config ?? {}) }
  knownProxyConfigKeys.forEach((key) => delete out[key])
  return out
}

function setStringConfig(config: Record<string, unknown>, key: string, value?: string) {
  const normalized = String(value ?? '').trim()
  if (normalized) config[key] = normalized
}

function proxyConfigFromValues(
  values: ProviderFormValues,
  advancedConfig: Record<string, unknown>,
) {
  const config = stripKnownProxyConfig(advancedConfig)
  const externalHosts = compactStrings(values.proxyExternalHosts)
  if (externalHosts.length) config.externalHosts = externalHosts
  setStringConfig(config, 'upstreamUrl', values.proxyUpstreamUrl)
  setStringConfig(config, 'mode', values.proxyMode || 'forward_auth')
  setStringConfig(config, 'cookieDomain', values.proxyCookieDomain)
  setStringConfig(config, 'pathPrefix', values.proxyPathPrefix)
  setStringConfig(config, 'outpostId', values.proxyOutpostId)
  const skipAuthPaths = compactStrings(values.proxySkipAuthPaths)
  if (skipAuthPaths.length) config.skipAuthPaths = skipAuthPaths
  config.websocketEnabled = Boolean(values.proxyWebsocketEnabled)
  config.allowPrivateUpstream = Boolean(values.proxyAllowPrivateUpstream)

  const headerMappings: Record<string, string> = {}
  const headerValues = {
    email: values.proxyHeaderEmail,
    groups: values.proxyHeaderGroups,
    roles: values.proxyHeaderRoles,
    teams: values.proxyHeaderTeams,
    user: values.proxyHeaderUser,
    userId: values.proxyHeaderUserId,
  }
  Object.entries(headerValues).forEach(([claim, headerName]) => {
    const normalized = String(headerName ?? '').trim()
    if (normalized) headerMappings[claim] = normalized
  })
  if (Object.keys(headerMappings).length) config.headerMappings = headerMappings
  return config
}

export function defaultProviderValues(): ProviderFormValues {
  return {
    applicationId: '',
    configJson: '{}',
    enabled: true,
    name: '',
    proxyCookieDomain: '',
    proxyExternalHosts: [],
    proxyHeaderEmail: defaultProxyHeaders.email,
    proxyHeaderGroups: defaultProxyHeaders.groups,
    proxyHeaderRoles: defaultProxyHeaders.roles,
    proxyHeaderTeams: defaultProxyHeaders.teams,
    proxyHeaderUser: defaultProxyHeaders.user,
    proxyHeaderUserId: defaultProxyHeaders.userId,
    proxyMode: 'forward_auth',
    proxyAllowPrivateUpstream: false,
    proxyOutpostId: '',
    proxyPathPrefix: '/',
    proxySkipAuthPaths: [],
    proxyUpstreamUrl: '',
    proxyWebsocketEnabled: true,
    samlAcsUrls: [],
    samlAttributeMappingsJson: '[]',
    samlEntityId: '',
    samlNameIdFormat: 'persistent',
    samlSpCertificatePem: '',
    samlWantAuthnRequestsSigned: false,
    secretRefsJson: '{}',
    status: 'enabled',
    type: 'oidc',
  }
}

export function providerValuesFor(item: IdentityProvider): ProviderFormValues {
  const config = item.config ?? {}
  const headerMappings = configStringMap(config, 'headerMappings', 'header_mappings')
  return {
    applicationId: item.applicationId,
    configJson: item.type === 'proxy' ? jsonText(stripKnownProxyConfig(config)) : jsonText(config),
    enabled: item.enabled,
    name: item.name,
    proxyCookieDomain: configString(config, 'cookieDomain', 'cookie_domain'),
    proxyExternalHosts: configStringArray(
      config,
      'externalHosts',
      'external_hosts',
      'hosts',
      'externalHost',
      'external_host',
      'host',
    ),
    proxyHeaderEmail: headerMappings.email || defaultProxyHeaders.email,
    proxyHeaderGroups: headerMappings.groups || defaultProxyHeaders.groups,
    proxyHeaderRoles: headerMappings.roles || defaultProxyHeaders.roles,
    proxyHeaderTeams: headerMappings.teams || defaultProxyHeaders.teams,
    proxyHeaderUser: headerMappings.user || defaultProxyHeaders.user,
    proxyHeaderUserId: headerMappings.userId || defaultProxyHeaders.userId,
    proxyMode: configString(config, 'mode') === 'reverse_proxy' ? 'reverse_proxy' : 'forward_auth',
    proxyAllowPrivateUpstream: configBoolean(
      config,
      'allowPrivateUpstream',
      'allow_private_upstream',
    ),
    proxyOutpostId: configString(config, 'outpostId', 'outpost_id'),
    proxyPathPrefix:
      configString(
        config,
        'pathPrefix',
        'path_prefix',
        'protectedPathPrefix',
        'protected_path_prefix',
      ) || '/',
    proxySkipAuthPaths: configStringArray(config, 'skipAuthPaths', 'skip_auth_paths'),
    proxyUpstreamUrl: configString(config, 'upstreamUrl', 'upstreamURL', 'upstream_url'),
    proxyWebsocketEnabled: configBoolean(config, 'websocketEnabled', 'websocket_enabled'),
    samlAcsUrls: configStringArray(config, 'acsUrls', 'acs_urls'),
    samlAttributeMappingsJson: JSON.stringify(config.attributeMappings ?? [], null, 2),
    samlEntityId: configString(config, 'entityId', 'entity_id'),
    samlNameIdFormat:
      (configString(
        config,
        'nameIdFormat',
        'name_id_format',
      ) as ProviderFormValues['samlNameIdFormat']) || 'persistent',
    samlSpCertificatePem: configString(config, 'spCertificatePem', 'sp_certificate_pem'),
    samlWantAuthnRequestsSigned: configBoolean(
      config,
      'wantAuthnRequestsSigned',
      'want_authn_requests_signed',
    ),
    secretRefsJson: '',
    status: item.status,
    type: item.type,
  }
}

function parseSecretRefs(value: string): Record<string, string> {
  const result: Record<string, string> = {}
  for (const [alias, reference] of Object.entries(parseRecordJSON(value, 'Secret refs'))) {
    if (typeof reference !== 'string') throw new Error('Secret refs 的值必须是密钥引用字符串')
    result[alias] = reference
  }
  return result
}

export function providerInputFromValues(values: ProviderFormValues): IdentityProviderInput {
  const advancedConfig = parseRecordJSON(values.configJson, 'Config')
  let config = advancedConfig
  if (values.type === 'proxy') {
    config = proxyConfigFromValues(values, advancedConfig)
  } else if (values.type === 'saml') {
    const mappings = JSON.parse(values.samlAttributeMappingsJson || '[]') as unknown
    if (!Array.isArray(mappings)) throw new Error('Attribute mappings 必须是 JSON array')
    config = {
      ...advancedConfig,
      entityId: values.samlEntityId.trim(),
      acsUrls: compactStrings(values.samlAcsUrls),
      nameIdFormat: values.samlNameIdFormat,
      spCertificatePem: values.samlSpCertificatePem.trim() || undefined,
      wantAuthnRequestsSigned: Boolean(values.samlWantAuthnRequestsSigned),
      attributeMappings: mappings,
    }
  }
  return {
    applicationId: values.applicationId.trim(),
    config,
    enabled: Boolean(values.enabled),
    name: values.name.trim(),
    secretRefs: String(values.secretRefsJson ?? '').trim()
      ? parseSecretRefs(values.secretRefsJson)
      : undefined,
    status: values.status || 'disabled',
    type: values.type || 'oidc',
  }
}

export function defaultOIDCClientValues(): OIDCClientFormValues {
  return {
    accessTokenTtlSeconds: 3600,
    allowedGrantTypes: defaultGrantTypes,
    allowedScopes: defaultScopes,
    clientId: '',
    clientSecret: '',
    clientType: 'confidential',
    idTokenTtlSeconds: 300,
    postLogoutRedirectUris: [],
    redirectRules: [{ mode: 'strict', value: '' }],
    refreshTokenTtlSeconds: 0,
    requirePkce: true,
    status: 'enabled',
  }
}

export function oidcClientValuesFor(client: IdentityOIDCClient): OIDCClientFormValues {
  return {
    accessTokenTtlSeconds: client.accessTokenTtlSeconds,
    allowedGrantTypes: client.allowedGrantTypes?.length
      ? client.allowedGrantTypes
      : defaultGrantTypes,
    allowedScopes: client.allowedScopes ?? defaultScopes,
    clientId: client.clientId,
    clientSecret: '',
    clientType: client.clientType || 'confidential',
    idTokenTtlSeconds: client.idTokenTtlSeconds,
    postLogoutRedirectUris: client.postLogoutRedirectUris ?? [],
    redirectRules: [
      ...(client.redirectUris ?? []).map((value) => ({ mode: 'strict' as const, value })),
      ...(client.redirectUriRegexes ?? []).map((value) => ({ mode: 'regex' as const, value })),
    ],
    refreshTokenTtlSeconds: client.refreshTokenTtlSeconds || 0,
    requirePkce: client.requirePkce,
    status: client.status,
  }
}

export function oidcClientInputFromValues(
  providerId: string,
  values: OIDCClientFormValues,
): IdentityOIDCClientInput {
  const clientSecret = values.clientSecret.trim()
  const clientId = values.clientId.trim()
  const redirectRules = values.redirectRules ?? []
  return {
    accessTokenTtlSeconds: Number(values.accessTokenTtlSeconds || 3600),
    allowedGrantTypes: compactStrings(values.allowedGrantTypes).map((value) => {
      if (value !== 'authorization_code' && value !== 'refresh_token')
        throw new Error('不支持的授权类型')
      return value
    }),
    allowedScopes: compactStrings(values.allowedScopes),
    ...(clientId ? { clientId } : {}),
    clientSecret: clientSecret || undefined,
    clientType: values.clientType || 'confidential',
    idTokenTtlSeconds: Number(values.idTokenTtlSeconds || 300),
    providerId,
    postLogoutRedirectUris: compactStrings(values.postLogoutRedirectUris),
    redirectUriRegexes: compactStrings(
      redirectRules.filter((rule) => rule.mode === 'regex').map((rule) => rule.value),
    ),
    redirectUris: compactStrings(
      redirectRules.filter((rule) => rule.mode !== 'regex').map((rule) => rule.value),
    ),
    refreshTokenTtlSeconds: Number(values.refreshTokenTtlSeconds || 0),
    requirePkce: values.clientType === 'public' || Boolean(values.requirePkce),
    status: values.status || 'enabled',
  }
}
