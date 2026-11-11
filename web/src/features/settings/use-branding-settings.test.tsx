/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { permissionSnapshotQueryKey } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { settingsApi } from './api'
import { useBrandingSettings } from './use-branding-settings'
import { defaultBrandingSettings } from '@/utils/branding'

vi.mock('./api', () => ({ settingsApi: { branding: { get: vi.fn() } } }))
const user = {
  userId: 'branding-test',
  userName: 'Test',
  email: '',
  roles: [],
  teams: [],
  projects: [],
  tags: [],
}
let root: Root
let client: QueryClient
let container: HTMLDivElement
function Subject() {
  const query = useBrandingSettings()
  return <span>{query.data.data.appTitle}</span>
}
async function mount(permissionKeys: string[]) {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(permissionSnapshotQueryKey, {
    data: { permissionKeys, visibleMenuIds: [], visibleMenus: [] },
  })
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <Subject />
      </QueryClientProvider>,
    ),
  )
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.removeItem('soha-branding')
  useAuthStore.setState({ user, accessToken: 'browser-test' })
  vi.mocked(settingsApi.branding.get).mockResolvedValue({
    ...defaultBrandingSettings,
    appTitle: 'Authorized brand',
  })
})
afterEach(async () => {
  if (root) await act(async () => root.unmount())
  client?.clear()
  document.body.innerHTML = ''
  useAuthStore.setState({ user: null, accessToken: null })
})
describe('shared branding bootstrap authorization', () => {
  it('uses the public/cached fallback without requesting administrative settings for a readonly user', async () => {
    await mount(['platform.clusters.view'])
    expect(settingsApi.branding.get).not.toHaveBeenCalled()
    expect(container.textContent).toBe(defaultBrandingSettings.appTitle)
  })
  it('still refreshes branding for an authenticated user with the existing read permission', async () => {
    await mount(['settings.branding.view'])
    expect(settingsApi.branding.get).toHaveBeenCalledTimes(1)
    expect(container.textContent).toBe('Authorized brand')
  })
})
