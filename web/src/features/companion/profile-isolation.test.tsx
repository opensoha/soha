/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { useAuthStore } from '@/stores/auth-store'
import { AssistantCompanionOverlay } from './assistant-companion-overlay'
import { companionApi, companionKeys } from './api'
import type {
  CompanionProfile,
  CompanionInteractionReceipt,
} from '@opensoha/contracts/gen/ts/sohaapi'
vi.mock('@/features/copilot', () => ({
  DraggableFloatShell: ({ children }: { children: ReactNode }) => <>{children}</>,
}))
vi.mock('@/features/plugins', () => ({
  pluginQueries: { installed: () => ({ queryKey: ['qa-plugins'], queryFn: async () => [] }) },
}))
vi.mock('./companion-renderer', () => ({ CompanionRenderer: () => <span>Pet</span> }))
let host: HTMLDivElement
let root: ReturnType<typeof createRoot>
let client: QueryClient
const profile = (level: number) =>
  ({ level, affinity: level, xp: level, revision: 1 }) as CompanionProfile
function user(userId: string) {
  useAuthStore.setState({
    user: { userId, userName: userId, email: '', roles: [], teams: [], projects: [], tags: [] },
  })
}
afterEach(async () => {
  await act(async () => root?.unmount())
  client?.clear()
  host?.remove()
  useAuthStore.setState({ user: null })
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
it('isolates cached growth and ignores a previous account interaction receipt', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const media = window.matchMedia
  window.matchMedia = vi.fn(() => ({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
  })) as unknown as typeof window.matchMedia
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  user('first')
  vi.spyOn(companionApi, 'profile').mockImplementation(async () =>
    profile(useAuthStore.getState().user?.userId === 'first' ? 41 : 2),
  )
  let settle: (receipt: CompanionInteractionReceipt) => void = () => undefined
  vi.spyOn(companionApi, 'interact').mockImplementation(
    () =>
      new Promise((resolve) => {
        settle = resolve
      }),
  )
  await act(async () => {
    root.render(
      <App>
        <QueryClientProvider client={client}>
          <AssistantCompanionOverlay
            messages={[]}
            onAction={() => undefined}
            onOpenAssistant={() => undefined}
            onOpenWorkbench={() => undefined}
          />
        </QueryClientProvider>
      </App>,
    )
  })
  await vi.waitFor(() => expect(host.textContent).toContain('Lv.41'))
  await act(async () => {
    host.querySelector<HTMLButtonElement>('[aria-label="摸摸宠物"]')!.click()
  })
  await act(async () => {
    user('second')
  })
  expect(host.textContent).not.toContain('Lv.41')
  await vi.waitFor(() => expect(host.textContent).toContain('Lv.2'))
  await act(async () => {
    settle({ profile: profile(99) } as CompanionInteractionReceipt)
  })
  expect(host.textContent).toContain('Lv.2')
  expect(client.getQueryData(companionKeys.profile('second'))).toMatchObject({ level: 2 })
  expect(client.getQueryData(companionKeys.profile('first'))).toMatchObject({ level: 41 })
  window.matchMedia = media
})
