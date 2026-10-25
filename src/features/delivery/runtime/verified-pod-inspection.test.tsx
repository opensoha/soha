/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { VerifiedPodInspection } from './pod-inspection'

const state = vi.hoisted(() => ({ denied: false, pod: vi.fn(), yaml: vi.fn() }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: () => !state.denied,
}))
vi.mock('@/features/platform', () => ({
  podQueries: {
    detail: () => ({ queryKey: ['verified-pod'], queryFn: state.pod }),
    events: () => ({ queryKey: ['events'], queryFn: async () => [], enabled: true }),
  },
  workloadQueries: {
    yaml: () => ({ queryKey: ['verified-yaml'], queryFn: state.yaml, enabled: true }),
  },
}))
let root: ReturnType<typeof createRoot>, client: QueryClient
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
    configurable: true,
  })
  state.denied = false
  state.pod.mockReset()
  state.yaml.mockReset()
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <VerifiedPodInspection
          clusterId="cluster"
          namespace="team"
          podName="api"
          expectedUid="original"
          mode="details"
        />
      </QueryClientProvider>,
    ),
  )
  for (let i = 0; i < 4; i++)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
}
it('verifies UID before reading YAML and shows main, sidecar, init and termination evidence', async () => {
  state.pod.mockResolvedValue({
    uid: 'original',
    containers: [
      { name: 'app', role: 'main' },
      { name: 'proxy', role: 'sidecar' },
      { name: 'prepare', role: 'init' },
    ],
  })
  state.yaml.mockResolvedValue({
    content: JSON.stringify({
      metadata: { name: 'api', namespace: 'team', uid: 'original' },
      spec: {
        containers: [{ name: 'app' }, { name: 'proxy' }],
        initContainers: [{ name: 'prepare' }],
      },
      status: {
        containerStatuses: [
          {
            name: 'proxy',
            ready: false,
            restartCount: 4,
            state: { waiting: { reason: 'CrashLoopBackOff' } },
            lastState: {
              terminated: {
                reason: 'OOMKilled',
                exitCode: 137,
                finishedAt: '2026-10-25T00:00:00Z',
              },
            },
          },
        ],
      },
    }),
  })
  await render()
  for (const text of ['主容器', 'Sidecar', 'Init', 'OOMKilled', '137', 'CrashLoopBackOff'])
    expect(document.body.textContent).toContain(text)
  expect(state.pod).toHaveBeenCalledTimes(1)
  expect(state.yaml).toHaveBeenCalledTimes(1)
})
it('refuses replacement Pod details and does not read replacement YAML', async () => {
  state.pod.mockResolvedValue({ uid: 'replacement' })
  await render()
  expect(document.body.textContent).toContain('Pod 已重建或 UID 无法确认')
  expect(state.yaml).not.toHaveBeenCalled()
})
it('does not read Pod or YAML without Pod view permission', async () => {
  state.denied = true
  await render()
  expect(state.pod).not.toHaveBeenCalled()
  expect(state.yaml).not.toHaveBeenCalled()
})
