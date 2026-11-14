import { MemoryRouter } from 'react-router-dom'
/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { WorkerPoolsDrawer, workerPoolSpec } from './worker-pools'

const state = vi.hoisted(() => ({ post: vi.fn(), canCreate: true, user: 'worker-user' }))
const pool = {
  id: 'pool-1',
  revision: 3,
  createdAt: '',
  updatedAt: '',
  spec: {
    name: 'Build workers',
    connectionId: 'pve-1',
    clusterId: 'target',
    owner: 'soha-kubeadm',
    imageId: 'image-1',
    providerNode: 'pve-a',
    storage: 'disk',
    bridge: 'vmbr0',
    snippetStorage: 'local',
    osProfile: 'ubuntu-24.04-amd64-containerd',
    kubernetesVersion: 'v1.35.2',
    cpu: 4,
    memoryMiB: 8192,
    diskGiB: 80,
    maxNodes: 2,
    enabled: true,
    requiredDaemonSets: [{ namespace: 'kube-system', name: 'network' }],
  },
}
vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (select: (s: unknown) => unknown) => select({ user: { userId: state.user } }),
}))
vi.mock('@/i18n', () => ({
  useI18n: () => ({ localeCode: 'zh-CN' }),
  localeText: (_: string, zh: string) => zh,
}))
vi.mock('../shared/use-virtualization-permissions', () => ({
  useVirtualizationPermissions: () => ({
    canViewWorkerPools: true,
    canViewTasks: true,
    canCreateWorkers: state.canCreate,
    canCreateWorkerPools: false,
    canUpdateWorkerPools: false,
    canDeleteWorkerPools: false,
  }),
}))
vi.mock('@/services/api-client', () => ({
  api: {
    get: async (path: string) =>
      path.includes('worker-pools')
        ? { data: [pool] }
        : path.includes('worker-readiness')
          ? { data: { verdict: 'inconclusive', summary: '等待原节点就绪', evidence: [] } }
          : { data: [] },
    post: (...args: unknown[]) => state.post(...args),
  },
}))
let root: Root | undefined
let container: HTMLDivElement
let client: QueryClient
beforeEach(() => {
  state.post.mockReset()
  state.canCreate = true
  state.user = 'worker-user'
  localStorage.clear()
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
})
afterEach(async () => {
  if (root) await act(async () => root?.unmount())
  client?.clear()
  container?.remove()
  vi.unstubAllGlobals()
})
async function settle() {
  for (let i = 0; i < 8; i++)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
}
async function render() {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  await act(async () =>
    root?.render(
      <QueryClientProvider client={client}>
        <App>
          <MemoryRouter>
            <WorkerPoolsDrawer
              connection={{ id: 'pve-1', name: 'PVE', provider: 'pve' }}
              onClose={() => {}}
            />
          </MemoryRouter>
        </App>
      </QueryClientProvider>,
    ),
  )
  await settle()
}
async function click(label: string) {
  const button = [...document.querySelectorAll('button')].find(
    (item) => item.textContent?.replace(/\s/g, '') === label.replace(/\s/g, ''),
  )
  expect(button, label).toBeTruthy()
  await act(async () => button!.click())
  await settle()
}
it('keeps a lost creation receipt across drawer remounts and reuses its exact key and revision', async () => {
  state.post
    .mockRejectedValueOnce(new Error('reply lost'))
    .mockResolvedValueOnce({ data: { id: 'original-operation', status: 'queued' } })
  await render()
  expect(document.body.textContent).toContain('Build workers')
  await click('创建节点')
  await click('确认创建')
  const original = state.post.mock.calls[0]
  expect(original[1]).toMatchObject({ poolRevision: 3 })
  expect(JSON.parse(localStorage.getItem('soha:worker-create:worker-user:pve-1')!).key).toBe(
    original[1].idempotencyKey,
  )
  await act(async () => root?.unmount())
  root = undefined
  client.clear()
  container.remove()
  await render()
  await click('确认创建')
  expect(state.post.mock.calls[1]).toEqual(original)
  expect(document.body.textContent).toContain('original-operation')
  expect(document.body.textContent).toContain('等待原节点就绪')
  await click('保留任务并关闭')
  expect(localStorage.getItem('soha:worker-create:worker-user:pve-1')).toBeNull()
})
it('hides worker writes without creation permission', async () => {
  state.canCreate = false
  await render()
  expect(
    [...document.querySelectorAll('button')].some(
      (button) => button.textContent?.replace(/\s/g, '') === '创建节点',
    ),
  ).toBe(false)
  expect(state.post).not.toHaveBeenCalled()
})
it('validates daemon references and unique node labels before sending configuration', () => {
  const values = {
    ...pool.spec,
    owner: 'soha-kubeadm' as const,
    osProfile: 'ubuntu-24.04-amd64-containerd' as const,
    daemons: 'kube-system/network',
    nodeLabels: 'workload=build',
  }
  expect(workerPoolSpec(values)).toMatchObject({
    requiredDaemonSets: [{ namespace: 'kube-system', name: 'network' }],
    labels: { workload: 'build' },
  })
  expect(() => workerPoolSpec({ ...values, daemons: 'network' })).toThrow()
  expect(() =>
    workerPoolSpec({ ...values, nodeLabels: 'workload=build\nworkload=other' }),
  ).toThrow()
})
