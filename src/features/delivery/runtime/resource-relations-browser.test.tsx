/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ResourceRelationsBrowser } from './resource-relations-browser'
import type { ApplicationRuntimeWorkload } from '../types'

const state = vi.hoisted(() => ({
  denied: '',
  get: vi.fn(),
  openSession: vi.fn(),
  pod: vi.fn(),
  yaml: vi.fn(),
}))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: (_: unknown, key: string) => state.denied !== key,
}))
vi.mock('@/services/api-client', () => ({ api: { get: state.get } }))
vi.mock('@/features/platform', () => ({
  RelatedResourceYaml: ({
    target,
    expectedUid,
  }: {
    target: { name: string }
    expectedUid?: string
  }) => (
    <div>
      resource-yaml {target.name} {expectedUid}
    </div>
  ),
  useRealtimeSessionDock: () => ({ openSession: state.openSession }),
  podQueries: {
    detail: () => ({ queryKey: ['test-pod'], queryFn: state.pod }),
    events: () => ({ queryKey: ['test-events'], queryFn: async () => [], enabled: true }),
  },
  workloadQueries: {
    yaml: () => ({ queryKey: ['test-pod-yaml'], queryFn: state.yaml, enabled: true }),
  },
}))
vi.mock('./service-pod-workspace', () => ({
  PodMetricsDetail: () => <div>verified metrics panel</div>,
}))
let root: ReturnType<typeof createRoot>, client: QueryClient
const workload = {
  applicationEnvironmentId: 'binding',
  targetId: 'target',
  clusterId: 'cluster',
  namespace: 'team',
  workloadName: 'widget',
  source: { resource: { uid: 'root' } },
} as ApplicationRuntimeWorkload
const podRef = {
  clusterId: 'cluster',
  namespace: 'team',
  apiVersion: 'v1',
  kind: 'Pod',
  name: 'child',
  uid: 'pod-uid',
  scopeMode: 'namespace',
}
const graph = {
  root: workload.source!.resource,
  config: { version: 'v1', rootUid: 'root', sources: [], rules: [] },
  nodes: [
    { resource: podRef, role: 'owned', depth: 1 },
    { resource: { ...podRef, name: 'candidate', uid: 'candidate' }, role: 'candidate', depth: 1 },
  ],
  edges: [],
  coverage: [],
  observedAt: '2026-10-22T00:00:00Z',
  partial: false,
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
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
  state.denied = ''
  state.get.mockReset().mockResolvedValue({ data: graph })
  state.openSession.mockReset()
  state.pod.mockReset().mockResolvedValue({ ...podRef, uid: 'replacement' })
  state.yaml.mockReset().mockResolvedValue({ content: podYaml('pod-uid', 'current-image') })
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
async function render(view: 'resources' | 'metrics' = 'resources', path = '/', value = workload) {
  await act(async () =>
    root.render(
      <MemoryRouter initialEntries={[path]}>
        <QueryClientProvider client={client}>
          <ResourceRelationsBrowser
            key={value.applicationEnvironmentId}
            workload={value}
            view={view}
          />
        </QueryClientProvider>
      </MemoryRouter>,
    ),
  )
  await tick()
}
async function tick() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
}
function podYaml(uid: string | undefined, image: string) {
  return JSON.stringify({
    metadata: { name: 'child', namespace: 'team', uid },
    spec: { containers: [{ name: 'main', image }] },
  })
}
it('uses the target endpoint without loading CR YAML or claiming selector candidates', async () => {
  state.denied = 'platform.configuration.secret-data.view'
  await render()
  expect(state.get).toHaveBeenCalledWith(
    '/application-environments/binding/targets/target/relations',
    { signal: expect.any(AbortSignal) },
  )
  expect(document.body.textContent).toContain('匹配候选')
  expect(document.querySelector('[aria-label="查看 candidate 日志"]')).toBeNull()
  await act(async () => {
    ;(document.querySelector('[aria-label="查看 child 日志"]') as HTMLButtonElement).click()
  })
  expect(state.openSession).toHaveBeenCalledWith({
    kind: 'logs',
    clusterId: 'cluster',
    namespace: 'team',
    podName: 'child',
    podUid: 'pod-uid',
  })
  expect(state.get.mock.calls.every(([path]) => !path.endsWith('/source'))).toBe(true)
})
it('does not query relations when environment permission is denied', async () => {
  state.denied = 'delivery.application-environments.view'
  await render()
  expect(state.get).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('无环境读取权限')
})
it('rechecks the live Pod UID before displaying the existing metrics panel', async () => {
  await render('metrics', '/?metricUid=pod-uid')
  await tick()
  expect(state.pod).toHaveBeenCalled()
  expect(document.body.textContent).toContain('Pod 已重建')
  expect(document.body.textContent).not.toContain('verified metrics panel')
})
it('shows the shared metrics panel after successful UID verification', async () => {
  state.pod.mockResolvedValue({ ...podRef })
  await render('metrics', '/?metricUid=pod-uid')
  await tick()
  expect(document.body.textContent).toContain('verified metrics panel')
  expect(state.openSession).not.toHaveBeenCalled()
})
it('aborts the old environment relation request on scope change', async () => {
  let signal: AbortSignal | undefined
  state.get.mockImplementation(
    (_path: string, opts: { signal: AbortSignal }) =>
      new Promise((_resolve, reject) => {
        signal = opts.signal
        signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
      }),
  )
  await render()
  const previous = signal
  await render('resources', '/', { ...workload, applicationEnvironmentId: 'prod-binding' })
  expect(previous?.aborted).toBe(true)
})

it('rejects a response for a newly rebound root under an older runtime identity', async () => {
  state.get.mockResolvedValue({ data: { ...graph, root: { ...graph.root, uid: 'new-root' } } })
  await render()
  expect(document.body.textContent).toContain('根资源关联已变化')
  expect(document.querySelector('[aria-label="查看 child 日志"]')).toBeNull()
  expect(state.openSession).not.toHaveBeenCalled()
})

it.each(['replacement', undefined])(
  'blocks Pod details when the YAML response UID is %s after preflight matched',
  async (uid) => {
    state.pod.mockResolvedValue({ ...podRef })
    state.yaml.mockResolvedValue({ content: podYaml(uid, 'replacement-image') })
    await render()
    await act(async () => {
      ;(document.querySelector('[aria-label="查看 child 详情"]') as HTMLButtonElement).click()
    })
    await vi.waitFor(async () => {
      await tick()
      expect(state.yaml).toHaveBeenCalled()
    })
    await vi.waitFor(async () => {
      await tick()
      expect(document.body.textContent).toContain('Pod 已重建')
    })
    expect(document.body.textContent).not.toContain('replacement-image')
  },
)

it('hides cached YAML until a fresh response confirms the selected Pod UID', async () => {
  state.pod.mockResolvedValue({ ...podRef })
  client.setQueryData(['test-pod-yaml'], { content: podYaml('pod-uid', 'cached-image') })
  let resolve!: (value: { content: string }) => void
  state.yaml.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done
      }),
  )
  await render()
  await act(async () => {
    ;(document.querySelector('[aria-label="查看 child 详情"]') as HTMLButtonElement).click()
  })
  await vi.waitFor(async () => {
    await tick()
    expect(state.yaml).toHaveBeenCalled()
  })
  expect(document.body.textContent).not.toContain('cached-image')
  await act(async () => resolve({ content: podYaml('pod-uid', 'fresh-image') }))
  await tick()
  expect(document.body.textContent).toContain('fresh-image')
})

it('withdraws an open resource YAML view when the latest relation scope removes its UID', async () => {
  const config = {
    resource: { ...podRef, apiVersion: 'v1', kind: 'ConfigMap', name: 'config', uid: 'config-uid' },
    role: 'owned',
    depth: 1,
  }
  state.get.mockResolvedValue({ data: { ...graph, nodes: [...graph.nodes, config] } })
  await render()
  await act(async () => {
    ;(document.querySelector('[aria-label="选择 ConfigMap config"]') as HTMLButtonElement).click()
  })
  await act(async () => {
    Array.from(document.querySelectorAll('button'))
      .find((button) => button.textContent === '查看资源 YAML')!
      .click()
  })
  await tick()
  expect(document.body.textContent).toContain('resource-yaml config config-uid')
  state.get.mockResolvedValue({ data: graph })
  await act(async () => {
    Array.from(document.querySelectorAll('button'))
      .find((button) => button.textContent?.replace(/\s/g, '') === '刷新关系')!
      .click()
  })
  await tick()
  expect(document.body.textContent).not.toContain('resource-yaml config config-uid')
})
