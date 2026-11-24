/** @vitest-environment jsdom */

import { I18nProvider } from '@/i18n'
import { ApiError } from '@/services/api-error'
import { renderToStaticMarkup } from 'react-dom/server'
import { App as AntdApp } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
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

let root: ReturnType<typeof createRoot> | undefined
let container: HTMLDivElement | undefined

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  window.matchMedia = vi.fn(() => ({
    matches: false,
    media: '',
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
})

afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
  root = undefined
  container = undefined
})

async function renderPanel(panel: ReactNode) {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  await act(async () =>
    root!.render(
      <I18nProvider>
        <AntdApp>{panel}</AntdApp>
      </I18nProvider>,
    ),
  )
  return container
}

describe('in-page provider metadata', () => {
  beforeEach(() => {
    state.canView = true
    state.error = null
    state.queries = []
  })
  it('previews XML as inert text in the page and presents load errors', async () => {
    const html = renderToStaticMarkup(
      <I18nProvider>
        <ProviderProtocolMetadataPanel provider={provider} />
      </I18nProvider>,
    )
    expect(html).toContain('&lt;EntityDescriptor&gt;')
    expect(html).not.toContain('<script>')
    expect(html).not.toContain('target="_blank"')
    state.error = new Error('metadata unavailable')
    const panel = await renderPanel(<ProviderProtocolMetadataPanel provider={provider} />)
    const notice = document.querySelector('.soha-read-feedback-notification')
    expect(notice?.textContent).toContain('未更新，正在展示上次读取结果')
    expect(panel.innerHTML).toContain('&lt;EntityDescriptor&gt;')
    expect(panel.querySelector('script')).toBeNull()
    expect(notice?.textContent).toContain('metadata unavailable')
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
  it('retains metadata selectors with an explicit stale warning after directory failure', async () => {
    state.error = new Error('directory unavailable')
    const panel = await renderPanel(
      <ProviderUserMetadataPanel provider={{ ...provider, type: 'oidc' }} />,
    )
    const notice = document.querySelector('.soha-read-feedback-notification')
    expect(notice?.textContent).toContain('未更新，正在展示上次读取结果')
    expect(panel.querySelector('form')).not.toBeNull()
    expect(notice?.textContent).toContain('directory unavailable')
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
