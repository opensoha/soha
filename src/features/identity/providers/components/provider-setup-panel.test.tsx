import { renderToStaticMarkup } from 'react-dom/server'
import type { ReactNode } from 'react'
import { I18nProvider } from '@/i18n'
import { describe, expect, it, vi } from 'vitest'
import type { IdentityProvider } from '../types'
import { ProviderConfigurationStatus, ProviderSetupPanel } from './provider-setup-panel'

const state = vi.hoisted(() => ({
  endpoints: {} as Record<string, string>,
  migrationRequired: false,
}))
const render = (node: ReactNode) => renderToStaticMarkup(<I18nProvider>{node}</I18nProvider>)
vi.mock('@tanstack/react-query', async (original) => ({
  ...(await original<typeof import('@tanstack/react-query')>()),
  useQuery: () => ({
    isPending: false,
    isError: false,
    data: {
      configurationStatus: 'complete',
      issues: [],
      endpoints: state.endpoints,
      migrationRequired: state.migrationRequired,
    },
  }),
}))

describe('provider connection information', () => {
  it.each([
    [
      'oidc',
      'discoveryUrl',
      'https://soha.example/.well-known/openid-configuration',
      '/api/v1/provider/oidc/.well-known/openid-configuration',
    ],
    [
      'saml',
      'samlMetadataUrl',
      'https://soha.example/saml/provider/metadata',
      '/api/v1/saml2/idp/provider/metadata',
    ],
  ])('renders %s endpoints without opening another browser tab', (type, key, url) => {
    state.endpoints = { [key]: url }
    const html = render(
      <ProviderSetupPanel provider={{ id: 'provider', type } as IdentityProvider} />,
    )
    expect(html).not.toContain('target="_blank"')
    expect(html).toContain(url)
    expect(html).not.toContain('查看协议元数据')
    expect(html).toContain('ant-descriptions-bordered')
    expect(html).toContain('真实接入验证需在业务应用中完成')
    expect(html).toContain('此页面不保存验证结果')
  })

  it('retains the legacy entry and explains migration recovery without claiming completion', () => {
    state.migrationRequired = true
    state.endpoints = { legacyReverseProxyUrl: 'https://soha.example/legacy/app' }
    const html = render(
      <ProviderSetupPanel provider={{ id: 'provider', type: 'proxy' } as IdentityProvider} />,
    )
    expect(html).toContain('Legacy Core Reverse Proxy：仍需迁移')
    expect(html).toContain('https://soha.example/legacy/app')
    expect(html).toContain('失败时恢复原入口和配置')
    expect(html).toContain('读取或复制模板不会完成迁移')
    expect(html).not.toContain('href="https://soha.example/legacy/app"')
    state.migrationRequired = false
  })

  it('keeps unsafe metadata URLs inert and list configuration state compact', () => {
    state.endpoints = { discoveryUrl: 'javascript:alert(1)' }
    const html = render(
      <ProviderSetupPanel provider={{ id: 'provider', type: 'oidc' } as IdentityProvider} />,
    )
    expect(html).not.toContain('href="javascript:')
    const status = render(<ProviderConfigurationStatus providerId="provider" />)
    expect(status).toContain('配置完整')
    expect(status).not.toContain('未验证真实登录')
  })
})
