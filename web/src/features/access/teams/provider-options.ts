import type { AccessLoginProviderRef } from '../shared/types'

const LOGIN_PROVIDER_TYPE_LABELS: Record<string, string> = {
  oidc: 'OIDC',
  oauth2: 'OAuth2',
  feishu: '飞书',
  dingtalk: '钉钉',
  wecom: '企业微信',
  saml: 'SAML',
}

const ORGANIZATION_SOURCE_TYPE_OPTIONS = [
  { value: 'oidc', label: 'OIDC 类型映射' },
  { value: 'oauth2', label: 'OAuth2 类型映射' },
  { value: 'feishu', label: '飞书类型映射' },
  { value: 'dingtalk', label: '钉钉类型映射' },
  { value: 'wecom', label: '企业微信类型映射' },
]

const DIRECTORY_SOURCE_OPTIONS = [
  { value: 'ldap', label: 'LDAP 同步' },
  { value: 'saml', label: 'SAML 映射' },
]

function loginProviderTypeLabel(type: string) {
  return LOGIN_PROVIDER_TYPE_LABELS[type] || type || '登录源'
}

function loginProviderOptionLabel(provider: AccessLoginProviderRef) {
  const name = provider.name || provider.id
  const disabledSuffix = provider.enabled === false ? '（停用）' : ''
  return `${loginProviderTypeLabel(provider.type)} · ${name} (${provider.id})${disabledSuffix}`
}

export function buildOrganizationSourceLabelMap(providers: AccessLoginProviderRef[]) {
  const entries: Array<[string, string]> = [
    ['local', '本地维护'],
    ...ORGANIZATION_SOURCE_TYPE_OPTIONS.map((item) => [item.value, item.label] as [string, string]),
    ...DIRECTORY_SOURCE_OPTIONS.map((item) => [item.value, item.label] as [string, string]),
    ...providers.map(
      (provider) => [provider.id, loginProviderOptionLabel(provider)] as [string, string],
    ),
  ]
  return Object.fromEntries(entries)
}

export function buildOrganizationSourceOptions(providers: AccessLoginProviderRef[]) {
  const loginProviderOptions = providers
    .filter((provider) => ['oidc', 'oauth2', 'feishu', 'dingtalk', 'wecom'].includes(provider.type))
    .map((provider) => ({ value: provider.id, label: loginProviderOptionLabel(provider) }))

  return [
    { label: '本地', options: [{ value: 'local', label: '本地维护' }] },
    ...(loginProviderOptions.length
      ? [{ label: '登录源应用', options: loginProviderOptions }]
      : []),
    { label: '按类型兼容', options: ORGANIZATION_SOURCE_TYPE_OPTIONS },
    { label: '目录服务', options: DIRECTORY_SOURCE_OPTIONS },
  ]
}

export function organizationSourceLabel(
  value: string | undefined,
  labelMap: Record<string, string>,
) {
  const source = String(value || 'local')
  return labelMap[source] || source
}
