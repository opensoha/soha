import { I18nProvider } from '@/i18n'
import { ApiError } from '@/services/api-error'
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { identityProviderQueries } from '../queries'
import type { IdentityProvider } from '../types'
import { ProviderProtocolMetadataPanel, ProviderUserMetadataPanel } from './provider-metadata-panel'

const state = vi.hoisted(() => ({
  canView: true,
  error: null as Error | null,
  queries: [] as { enabled?: boolean }[],
}))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({}),
  hasPermission: () => state.canView,
}))
vi.mock('@tanstack/react-query', async (original) => ({
  ...(await original<typeof import('@tanstack/react-query')>()),
  useQuery: (options: { enabled?: boolean; queryKey: readonly unknown[] }) => {
    state.queries.push(options)
    return {
      isPending: false,
      isError: Boolean(state.error),
      error: state.error,
      isEnabled: options.enabled !== false,
      data: options.queryKey.includes('protocol-metadata')
        ? '<EntityDescriptor><script>unsafe</script></EntityDescriptor>'
        : [],
      refetch: vi.fn(),
    }
  },
}))
const provider = { id: 'provider', type: 'saml' } as IdentityProvider

describe('in-page provider metadata', () => {
  beforeEach(() => {
    state.canView = true
    state.error = null
    state.queries = []
  })
  it('previews XML as inert text in the page and presents load errors', () => {
    let html = renderToStaticMarkup(
      <I18nProvider>
        <ProviderProtocolMetadataPanel provider={provider} />
      </I18nProvider>,
    )
    expect(html).toContain('&lt;EntityDescriptor&gt;')
    expect(html).not.toContain('<script>')
    expect(html).not.toContain('target="_blank"')
    state.error = new Error('metadata unavailable')
    html = renderToStaticMarkup(
      <I18nProvider>
        <ProviderProtocolMetadataPanel provider={provider} />
      </I18nProvider>,
    )
    expect(html).toContain('未更新，正在展示上次读取结果')
    expect(html).toContain('&lt;EntityDescriptor&gt;')
    expect(html).not.toContain('<script>')
    expect(html).toContain('metadata unavailable')
  })
  it('does not query users or reveal cached attributes without user-view permission', () => {
    state.canView = false
    const html = renderToStaticMarkup(
      <I18nProvider>
        <ProviderUserMetadataPanel provider={provider} />
      </I18nProvider>,
    )
    expect(html).toContain('需要用户查看权限')
    expect(state.queries.every((query) => query.enabled === false)).toBe(true)
    expect(html).not.toContain('unsafe')
  })
  it('hides cached XML when the metadata request is denied', () => {
    state.error = new ApiError(403, 'metadata denied', {
      method: 'GET',
      path: '/identity/providers/provider/metadata',
    })
    const html = renderToStaticMarkup(
      <I18nProvider>
        <ProviderProtocolMetadataPanel provider={provider} />
      </I18nProvider>,
    )
    expect(html).toContain('无访问权限')
    expect(html).not.toContain('&lt;EntityDescriptor&gt;')
  })
  it('retains metadata selectors with an explicit stale warning after directory failure', () => {
    state.error = new Error('directory unavailable')
    const html = renderToStaticMarkup(
      <I18nProvider>
        <ProviderUserMetadataPanel provider={{ ...provider, type: 'oidc' }} />
      </I18nProvider>,
    )
    expect(html).toContain('未更新，正在展示上次读取结果')
    expect(html).toContain('ant-form')
    expect(html).toContain('directory unavailable')
  })
  it('separates preview caches for each provider, user and client', () => {
    const first = identityProviderQueries.userMetadata('provider', 'alice', 'client')
    expect(first.queryKey).not.toEqual(
      identityProviderQueries.userMetadata('provider', 'bob', 'client').queryKey,
    )
    expect(first.queryKey).not.toEqual(
      identityProviderQueries.userMetadata('provider', 'alice', 'other-client').queryKey,
    )
    expect(first.queryKey).not.toEqual(
      identityProviderQueries.userMetadata('other-provider', 'alice', 'client').queryKey,
    )
    expect(first.gcTime).toBe(0)
  })
})
