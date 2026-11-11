export function loginProviderLabel(value?: string) {
  const normalized = String(value || '')
    .trim()
    .toLowerCase()
  const labels: Record<string, string> = {
    password: '账号密码',
    oidc: 'OIDC',
    oauth2: 'OAuth2',
    saml: 'SAML',
    feishu: '飞书',
    dingtalk: '钉钉',
    wecom: '企业微信',
  }
  return labels[normalized] || String(value || '').trim() || '未设置'
}

export function loginProviderTagColor(value?: string) {
  const normalized = String(value || '')
    .trim()
    .toLowerCase()
  return normalized ? 'blue' : 'default'
}
