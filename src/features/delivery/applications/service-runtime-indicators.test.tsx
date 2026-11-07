/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ServiceRuntimeIndicators } from './service-runtime-indicators'
import { deliveryQueries } from '../queries'
import { podQueries } from '@/features/platform'
import type { ApplicationRuntimeWorkload, ApplicationWorkloadRuntimeDetail, Pod } from '../types'
import type { ResourceMetrics } from '@/types'

const state = vi.hoisted(() => ({
  allowed: true,
  permissionFailure: false,
  configured: true,
  partial: false,
  failure: false,
  count: 2,
  metricMessage: '',
  get: vi.fn(),
}))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} }, isError: state.permissionFailure }),
  hasPermission: () => state.allowed,
}))
vi.mock('@/services/api-client', () => ({
  api: { get: (...args: unknown[]) => state.get(...args) },
}))
const workload: ApplicationRuntimeWorkload = {
  applicationEnvironmentId: 'env',
  clusterId: 'c',
  namespace: 'ns',
  workloadKind: 'Deployment',
  workloadName: 'api',
  readyReplicas: 2,
  desiredReplicas: 2,
  updatedReplicas: 2,
  availableReplicas: 2,
}
let root: ReturnType<typeof createRoot>
let host: HTMLDivElement
let client: QueryClient
const openPods = vi.fn()
const openMetrics = vi.fn()
const pods = (): Pod[] =>
  Array.from(
    { length: state.count },
    (_, index) =>
      ({
        name: `pod-${index}`,
        namespace: 'ns',
        uid: `uid-${index}`,
        phase: 'Running',
        readyContainers: index === 0 ? '1/1' : '0/1',
        restarts: index + 1,
      }) as Pod,
  )
beforeEach(() => {
  state.allowed = true
  state.permissionFailure = false
  state.configured = true
  state.partial = false
  state.failure = false
  state.count = 2
  state.metricMessage = ''
  state.get.mockReset().mockImplementation(async (path: string) => {
    if (path.endsWith('/runtime')) {
      if (state.failure) throw new Error('runtime failed')
      return { data: { workload, pods: [...pods(), ...pods()] } }
    }
    if (path.includes('/detail')) {
      const pod = pods().find((item) => path.includes(`/pods/${item.name}/`))
      return { data: pod }
    }
    if (path.includes('/metrics'))
      return {
        data: {
          configured: state.configured,
          message: state.metricMessage,
          series:
            state.partial && path.includes('pod-1')
              ? []
              : [
                  { key: 'cpu', label: 'CPU', unit: 'cores', latest: 0.25, points: [] },
                  { key: 'memory', label: 'Memory', unit: 'bytes', latest: 1024, points: [] },
                ],
        },
      }
    throw new Error(`Unexpected request ${path}`)
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  openPods.mockClear()
  openMetrics.mockClear()
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  host.remove()
  vi.unstubAllGlobals()
})
async function render(workloads = [workload], podWorkloads = [workload], enabled = true) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <ServiceRuntimeIndicators
          applicationId="app"
          workloads={workloads}
          podWorkloads={podWorkloads}
          enabled={enabled}
          onOpenPods={openPods}
          onOpenMetrics={openMetrics}
        />
      </QueryClientProvider>,
    ),
  )
  for (let i = 0; i < 6; i++)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5))
    })
}
function values() {
  return [...host.querySelectorAll('strong')].map((element) => element.textContent)
}
function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (error: Error) => void
  const promise = new Promise((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
const runtimeKey = () =>
  deliveryQueries.workloads.runtime({
    applicationId: 'app',
    applicationEnvironmentId: 'env',
    workloadName: 'api',
  }).queryKey

it('deduplicates snapshot Pods, aggregates real readings and opens the existing views', async () => {
  await render()
  expect(values()).toEqual(['1/2', '3', '500 mCPU', '2.0 KB'])
  expect(state.get.mock.calls.filter(([path]) => path.includes('/metrics'))).toHaveLength(2)
  const buttons = [...host.querySelectorAll('button')]
  await act(async () => {
    buttons[0].click()
    buttons[2].click()
  })
  expect(openPods).toHaveBeenCalledOnce()
  expect(openMetrics).toHaveBeenCalledOnce()
  expect(
    client.getQueryData(
      podQueries.metrics({ clusterId: 'c', namespace: 'ns' }, 'pod-0', 60, 'uid-0').queryKey,
    ),
  ).toBeUndefined()
})
it('hides cached values immediately when permission or the active view is removed', async () => {
  await render()
  state.allowed = false
  state.get.mockClear()
  await render()
  expect(values()).toEqual(['—', '—', '—', '—'])
  expect(host.textContent).toContain('无查看权限')
  expect(state.get).not.toHaveBeenCalled()
  state.allowed = true
  await render([workload], [workload], false)
  expect(values()).toEqual(['—', '—', '—', '—'])
})
it.each(['StatefulSet', 'CustomResource'])(
  'does not advertise a Deployment subset as the total for %s',
  async (workloadKind) => {
    await render([workload, { ...workload, workloadName: 'operator', workloadKind }])
    expect(values()).toEqual(['—', '—', '—', '—'])
    expect(host.textContent).toContain('待观测')
    expect(host.textContent).toContain('部分来源')
  },
)
it.each(['forbidden', 'replaced', 'missing', 'unavailable'] as const)(
  'does not read a %s source or its cache',
  async (sourceStatus) => {
    await render([{ ...workload, sourceStatus }], [{ ...workload, sourceStatus }])
    expect(state.get).not.toHaveBeenCalled()
    expect(values()).toEqual(['—', '—', '—', '—'])
  },
)
it('distinguishes unconfigured monitoring from partial series', async () => {
  state.configured = false
  await render()
  expect(values()).toEqual(['1/2', '3', '—', '—'])
  expect(host.textContent).toContain('未接入监控')
  state.configured = true
  state.partial = true
  await act(async () => {
    await client.invalidateQueries()
  })
  await render()
  expect(host.textContent).toContain('指标覆盖 1/2 Pod')
  expect(values()).toEqual(['1/2', '3', '—', '—'])
})
it('caps metric reads and explicitly labels the sample', async () => {
  state.count = 12
  await render()
  expect(state.get.mock.calls.filter(([path]) => path.includes('/metrics'))).toHaveLength(8)
  expect(state.get.mock.calls.filter(([path]) => path.includes('/detail'))).toHaveLength(16)
  expect(values()).toEqual(['1/12', '78', '—', '—'])
  expect(host.textContent).toContain('指标覆盖 8/12 Pod')
  expect(host.textContent).toContain('采样上限 8 Pod')
})
it('does not request metrics for an empty snapshot', async () => {
  state.count = 0
  await render()
  expect(values()).toEqual(['0/0', '0', '—', '—'])
  expect(state.get.mock.calls.filter(([path]) => path.includes('/metrics'))).toHaveLength(0)
})
it('hides old runtime data after a failed read and offers retry', async () => {
  state.failure = true
  client.setQueryData(
    deliveryQueries.workloads.runtime({
      applicationId: 'app',
      applicationEnvironmentId: 'env',
      workloadName: 'api',
    }).queryKey,
    { workload, pods: pods() } as ApplicationWorkloadRuntimeDetail,
  )
  await render()
  expect(values()).toEqual(['—', '—', '—', '—'])
  expect(host.textContent).toContain('运行数据读取失败')
  expect(host.textContent).toContain('重试运行指标')
  expect(state.get.mock.calls.filter(([path]) => path.includes('/metrics'))).toHaveLength(0)
})

it('caps runtime fan-out and does not publish a partial-source total', async () => {
  const workloads = Array.from({ length: 10 }, (_, index) => ({
    ...workload,
    workloadName: `api-${index}`,
  }))
  await render(workloads, workloads)
  expect(state.get.mock.calls.filter(([path]) => path.endsWith('/runtime'))).toHaveLength(4)
  expect(values()).toEqual(['—', '—', '—', '—'])
})

it('does not issue name-only metrics requests when snapshot Pod UIDs are missing', async () => {
  state.get.mockImplementation(async () => ({
    data: { workload, pods: pods().map((pod) => ({ ...pod, uid: undefined })) },
  }))
  await render()
  expect(values()).toEqual(['1/2', '3', '—', '—'])
  expect(host.textContent).toContain('指标覆盖 0/2 Pod')
  expect(state.get).toHaveBeenCalledOnce()
})

it('does not accept a forbidden source returned by a fresh runtime read', async () => {
  state.get.mockImplementation(async () => ({
    data: { workload: { ...workload, sourceStatus: 'forbidden' }, pods: pods() },
  }))
  await render()
  expect(values()).toEqual(['—', '—', '—', '—'])
  expect(state.get).toHaveBeenCalledOnce()
})

it('fails closed when permission refresh fails while an old allow snapshot remains cached', async () => {
  await render()
  state.permissionFailure = true
  state.get.mockClear()
  await render()
  expect(values()).toEqual(['—', '—', '—', '—'])
  expect(host.textContent).toContain('无查看权限')
  expect(state.get).not.toHaveBeenCalled()
})

it('retains verified readings during background fetches without remounting metric observers', async () => {
  await render()
  const backgroundRuntime = deferred()
  state.get.mockImplementation((path: string) => {
    if (path.endsWith('/runtime')) return backgroundRuntime.promise
    throw new Error(`Unexpected metrics remount: ${path}`)
  })
  await act(async () => {
    void client.invalidateQueries({ queryKey: runtimeKey() })
  })
  await flush()
  expect(values()).toEqual(['1/2', '3', '500 mCPU', '2.0 KB'])
  expect(host.textContent).toContain('刷新中')
  expect(state.get.mock.calls.filter(([path]) => path.includes('/metrics'))).toHaveLength(2)
  await act(async () => backgroundRuntime.resolve({ data: { workload, pods: pods() } }))
  await flush()
  expect(values()).toEqual(['1/2', '3', '500 mCPU', '2.0 KB'])
  expect(state.get.mock.calls.filter(([path]) => path.includes('/metrics'))).toHaveLength(2)

  const backgroundMetrics = deferred()
  state.get.mockImplementation((path: string) =>
    path.includes('/detail')
      ? Promise.resolve({ data: pods().find((pod) => path.includes(`/pods/${pod.name}/`)) })
      : backgroundMetrics.promise,
  )
  await act(async () => {
    for (const pod of pods())
      void client.invalidateQueries({
        queryKey: podQueries.metrics({ clusterId: 'c', namespace: 'ns' }, pod.name, 60, pod.uid)
          .queryKey,
      })
  })
  await flush()
  expect(values()).toEqual(['1/2', '3', '500 mCPU', '2.0 KB'])
  await act(async () => backgroundMetrics.reject(new Error('metrics denied')))
  await flush()
  expect(values()).toEqual(['1/2', '3', '—', '—'])
  expect(host.textContent).toContain('指标读取失败')

  const failedRuntime = deferred()
  state.get.mockImplementation(() => failedRuntime.promise)
  await act(async () => {
    void client.invalidateQueries({ queryKey: runtimeKey() })
  })
  await act(async () => failedRuntime.reject(new Error('runtime unavailable')))
  await flush()
  expect(values()).toEqual(['—', '—', '—', '—'])
  expect(host.textContent).toContain('运行数据读取失败')
})

it('blocks initial cached runtime and metrics until fresh mount reads complete', async () => {
  client.setQueryData(runtimeKey(), { workload, pods: pods() } as ApplicationWorkloadRuntimeDetail)
  for (const pod of pods())
    client.setQueryData(
      podQueries.metrics({ clusterId: 'c', namespace: 'ns' }, pod.name, 60, pod.uid).queryKey,
      {
        resourceKind: 'Pod',
        resourceName: pod.name,
        namespace: pod.namespace,
        source: 'prometheus',
        generatedAt: '2026-11-07T00:00:00Z',
        rangeMinutes: 60,
        stepSeconds: 30,
        configured: true,
        series: [
          { key: 'cpu', label: 'CPU', unit: 'cores', latest: 99, points: [] },
          { key: 'memory', label: 'Memory', unit: 'bytes', latest: 99, points: [] },
        ],
      } satisfies ResourceMetrics,
    )
  const initialRuntime = deferred()
  const initialMetrics = deferred()
  state.get.mockImplementation((path: string) => {
    if (path.endsWith('/runtime')) return initialRuntime.promise
    if (path.includes('/detail'))
      return Promise.resolve({ data: pods().find((pod) => path.includes(`/pods/${pod.name}/`)) })
    return initialMetrics.promise
  })
  await render()
  expect(values()).toEqual(['—', '—', '—', '—'])
  expect(state.get).toHaveBeenCalledOnce()
  await act(async () => initialRuntime.resolve({ data: { workload, pods: pods() } }))
  await flush()
  expect(values()).toEqual(['1/2', '3', '—', '—'])
  expect(state.get.mock.calls.filter(([path]) => path.includes('/metrics'))).toHaveLength(2)
  await act(async () => initialMetrics.reject(new Error('metrics failed')))
  await flush()
  expect(values()).toEqual(['1/2', '3', '—', '—'])
})

it.each(['before', 'after'])(
  'rejects a Pod UID replacement %s the fresh metrics read',
  async (when) => {
    const original = state.get.getMockImplementation()!
    const detailCalls = new Map<string, number>()
    state.get.mockImplementation(async (path: string) => {
      if (path.includes('/detail')) {
        const count = (detailCalls.get(path) ?? 0) + 1
        detailCalls.set(path, count)
        const pod = pods().find((item) => path.includes(`/pods/${item.name}/`))!
        return { data: { ...pod, uid: when === 'before' || count > 1 ? 'replacement' : pod.uid } }
      }
      return original(path)
    })
    await render()
    expect(values()).toEqual(['1/2', '3', '—', '—'])
    expect(host.textContent).toContain('指标读取失败')
    expect(host.textContent).toContain('Pod 身份已变化')
    expect(host.textContent).toContain('重试运行指标')
    expect(state.get.mock.calls.filter(([path]) => path.includes('/metrics'))).toHaveLength(
      when === 'before' ? 0 : 2,
    )
  },
)

it('does not adopt shared metric cache writes as UID-verified readings', async () => {
  await render()
  const calls = state.get.mock.calls.length
  await act(async () => {
    for (const pod of pods())
      client.setQueryData(
        podQueries.metrics({ clusterId: 'c', namespace: 'ns' }, pod.name, 60, pod.uid).queryKey,
        {
          resourceKind: 'Pod',
          resourceName: pod.name,
          namespace: pod.namespace,
          source: 'prometheus',
          generatedAt: '2026-11-07T00:00:00Z',
          rangeMinutes: 60,
          stepSeconds: 30,
          configured: true,
          series: [{ key: 'cpu', label: 'CPU', unit: 'cores', latest: 99, points: [] }],
        } satisfies ResourceMetrics,
      )
  })
  await flush()
  expect(values()).toEqual(['1/2', '3', '500 mCPU', '2.0 KB'])
  expect(state.get).toHaveBeenCalledTimes(calls)
})

it('shows configured HTTP-200 observation errors and retries without accepting empty coverage as success', async () => {
  state.metricMessage = 'Prometheus query timed out'
  state.partial = true
  await render()
  expect(values()).toEqual(['1/2', '3', '—', '—'])
  expect(host.textContent).toContain('指标读取失败')
  expect(host.querySelector('[role="alert"]')?.textContent).toContain('Prometheus query timed out')
  expect(host.textContent).not.toContain('未接入监控')
  const retry = [...host.querySelectorAll('button')].find((button) =>
    button.textContent?.includes('重试运行指标'),
  )!
  state.metricMessage = ''
  state.partial = false
  await act(async () => retry.click())
  await flush()
  expect(values()).toEqual(['1/2', '3', '500 mCPU', '2.0 KB'])
})
