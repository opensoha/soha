/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ImportedSourceBrowser } from './imported-source-browser'
import type { ApplicationRuntimeWorkload } from '../types'

vi.mock('../runtime/resource-relations-browser', () => ({ ResourceRelationsBrowser: () => null }))

const state = vi.hoisted(() => ({ denied: '', get: vi.fn() }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: (_: unknown, key: string) => key !== state.denied,
}))
vi.mock('@/services/api-client', () => ({ api: { get: state.get } }))
let root: ReturnType<typeof createRoot>
let client: QueryClient
const workload: ApplicationRuntimeWorkload = {
  applicationEnvironmentId: 'test-binding',
  targetId: 'target',
  serviceId: 'service',
  clusterId: 'cluster',
  namespace: 'test',
  workloadName: 'search',
  workloadKind: 'CustomResource',
  healthStatus: 'unknown',
  sourceStatus: 'linked',
  desiredReplicas: 0,
  readyReplicas: 0,
  updatedReplicas: 0,
  availableReplicas: 0,
  source: {
    resource: {
      clusterId: 'cluster',
      namespace: 'test',
      name: 'search',
      kind: 'Elasticsearch',
      apiVersion: 'elasticsearch.k8s.elastic.co/v1',
      scopeMode: 'namespace',
      uid: 'uid-1',
    },
    specDigest: 'a'.repeat(64),
    observedAt: '2026-10-21T10:00:00Z',
    crdName: 'elasticsearches.elasticsearch.k8s.elastic.co',
    plural: 'elasticsearches',
  },
}
beforeEach(() => {
  state.denied = ''
  state.get.mockReset().mockResolvedValue({
    data: { content: 'kind: Elasticsearch\nspec:\n  password: synthetic-secret' },
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
    }),
  })
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
async function render(value = workload) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <ImportedSourceBrowser workloads={[value]} />
      </QueryClientProvider>,
    ),
  )
}
async function tick() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function click(label: string) {
  await act(async () => {
    Array.from(document.querySelectorAll('button'))
      .find((button) => button.textContent === label)!
      .click()
  })
  await tick()
}

it('loads pinned source only on request and removes YAML cache on close', async () => {
  await render()
  expect(state.get).not.toHaveBeenCalled()
  await click('查看源 YAML')
  expect(state.get).toHaveBeenCalledWith(
    '/application-environments/test-binding/targets/target/source',
    { signal: expect.any(AbortSignal) },
  )
  expect(document.querySelector('[aria-label="源配置 YAML"]')?.textContent).toContain(
    'synthetic-secret',
  )
  await click('关闭 YAML')
  expect(document.querySelector('[aria-label="源配置 YAML"]')).toBeNull()
  expect(
    client.getQueryCache().findAll({ queryKey: ['delivery', 'environments', 'target-source'] }),
  ).toHaveLength(0)
  expect(JSON.stringify(localStorage)).not.toContain('synthetic-secret')
})
it('blocks unknown CR full text without sensitive-data permission', async () => {
  state.denied = 'platform.configuration.secret-data.view'
  await render()
  expect(document.body.textContent).toContain('敏感数据读取权限')
  expect(state.get).not.toHaveBeenCalled()
})
it('never opens replaced resources and clears old content on environment change', async () => {
  await render()
  await click('查看源 YAML')
  await render({ ...workload, applicationEnvironmentId: 'prod-binding', sourceStatus: 'replaced' })
  await tick()
  expect(document.body.textContent).toContain('同名资源已重建')
  expect(document.body.textContent).not.toContain('synthetic-secret')
  expect(state.get).toHaveBeenCalledTimes(1)
})
it('aborts an in-flight read when the source view unmounts', async () => {
  let signal: AbortSignal | undefined
  state.get.mockImplementation(
    (_path: string, options: { signal: AbortSignal }) =>
      new Promise((_resolve, reject) => {
        signal = options.signal
        signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
      }),
  )
  await render()
  await click('查看源 YAML')
  await render({ ...workload, applicationEnvironmentId: 'prod-binding' })
  expect(signal?.aborted).toBe(true)
  expect(document.querySelector('[aria-label="源配置 YAML"]')).toBeNull()
})
