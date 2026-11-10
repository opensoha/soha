/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { VerifiedPodInspection } from './pod-inspection'

const state = vi.hoisted(() => ({ denied: false, pod: vi.fn(), yaml: vi.fn(), events: vi.fn() }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: () => !state.denied,
}))
vi.mock('@/features/platform', async (importOriginal) => ({
  WorkloadDetailEvents: (await importOriginal<typeof import('@/features/platform')>())
    .WorkloadDetailEvents,
  podQueries: {
    detail: () => ({ queryKey: ['verified-pod'], queryFn: state.pod }),
    events: () => ({ queryKey: ['events'], queryFn: state.events, enabled: true }),
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
  state.events.mockReset().mockResolvedValue([])
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
async function render(mode: 'details' | 'events' = 'details') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <VerifiedPodInspection
          clusterId="cluster"
          namespace="team"
          podName="api"
          expectedUid="original"
          mode={mode}
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

it('shows bounded history newest first and refreshes events without fetching YAML', async () => {
  state.pod.mockResolvedValue({ uid: 'original', containers: [] })
  state.events.mockResolvedValue([
    {
      name: 'old',
      type: 'Warning',
      reason: 'Unhealthy',
      message: 'Readiness probe timed out',
      count: 3,
      ageSeconds: 600,
    },
    {
      name: 'new',
      type: 'Normal',
      reason: 'Started',
      message: 'Started container',
      count: 1,
      ageSeconds: 10,
    },
  ])
  await render('events')
  const records = [...document.querySelectorAll('.soha-pod-detail-timeline-item')]
  expect(records).toHaveLength(2)
  expect(records[0].textContent).toContain('Started')
  expect(records[1].textContent).toContain('Readiness probe timed out')
  expect(records[1].textContent).toContain('3')
  expect(document.querySelector('.soha-pod-detail-timeline section h3')).not.toBeNull()
  expect(records[0].querySelector('time')?.getAttribute('dateTime')).toBe(
    new Date(client.getQueryState(['events'])!.dataUpdatedAt - 10_000).toISOString(),
  )
  expect(document.querySelector('button[aria-label="事件范围说明"]')).not.toBeNull()
  expect(document.body.textContent).not.toContain('历史告警不代表当前健康状态')
  const refresh = document.querySelector('button[aria-label="刷新 Pod 事件"]')!
  await act(async () => (refresh as HTMLButtonElement).click())
  expect(state.events).toHaveBeenCalledTimes(2)
  expect(state.yaml).not.toHaveBeenCalled()
})
it('keeps the bounded empty-state explanation for no Pod events', async () => {
  state.pod.mockResolvedValue({ uid: 'original' })
  await render('events')
  expect(document.body.textContent).toContain('没有匹配的 Pod 事件')
})
it('offers retry when event history cannot be loaded', async () => {
  state.pod.mockResolvedValue({ uid: 'original' })
  state.events.mockRejectedValue(new Error('unavailable'))
  await render('events')
  expect(document.body.textContent).toContain('Pod 事件读取失败')
  state.events.mockResolvedValue([])
  const retry = [...document.querySelectorAll('button')].find(
    (button) => button.getAttribute('aria-label') === '重试读取Pod 事件',
  )!
  await act(async () => retry.click())
  expect(state.events).toHaveBeenCalledTimes(2)
})
it('does not fetch events for a replaced Pod or without view permission', async () => {
  state.pod.mockResolvedValue({ uid: 'replacement' })
  await render('events')
  expect(state.events).not.toHaveBeenCalled()
  state.denied = true
  await render('events')
  expect(state.events).not.toHaveBeenCalled()
})

const historicalEvent = {
  name: 'history',
  type: 'Warning',
  reason: 'Unhealthy',
  message: 'Historical readiness timeout',
  count: 3,
  ageSeconds: 600,
}
async function clickAndSettle(label: string) {
  const button = document.querySelector(`button[aria-label="${label}"]`) as HTMLButtonElement
  expect(button).not.toBeNull()
  await act(async () => button.click())
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 40))
  })
}
it('retains cached event history after 503 and recovers through inline retry', async () => {
  state.pod.mockResolvedValue({ uid: 'original' })
  state.events.mockResolvedValue([historicalEvent])
  await render('events')
  state.events.mockRejectedValue(new ApiError(503, 'temporarily unavailable'))
  await clickAndSettle('刷新 Pod 事件')
  expect(document.body.textContent).toContain('Historical readiness timeout')
  expect(document.body.textContent).toContain('未更新，正在展示上次读取结果')
  expect(document.body.textContent).not.toContain('没有匹配的 Pod 事件')
  state.events.mockResolvedValue([{ ...historicalEvent, message: 'Updated event history' }])
  await clickAndSettle('重试读取Pod 事件')
  expect(document.body.textContent).toContain('Updated event history')
  expect(document.body.textContent).not.toContain('未更新，正在展示上次读取结果')
  expect(state.events).toHaveBeenCalledTimes(3)
})
it.each([401, 403])(
  'hides cached event history after %s without offering retry or false empty state',
  async (status) => {
    state.pod.mockResolvedValue({ uid: 'original' })
    state.events.mockResolvedValue([historicalEvent])
    await render('events')
    state.events.mockRejectedValue(new ApiError(status, 'access unavailable'))
    await clickAndSettle('刷新 Pod 事件')
    expect(document.querySelectorAll('.soha-pod-detail-timeline-item')).toHaveLength(0)
    expect(document.body.textContent).not.toContain('Historical readiness timeout')
    expect(document.body.textContent).not.toContain('没有匹配的 Pod 事件')
    expect(document.body.textContent).toContain(status === 401 ? '登录状态失效' : '无访问权限')
    expect(document.querySelector('button[aria-label="重试读取Pod 事件"]')).toBeNull()
    expect(document.querySelector('button[aria-label="刷新 Pod 事件"]')).toBeNull()
  },
)

it('connects the shared time tree severity filters and event search', async () => {
  state.pod.mockResolvedValue({ uid: 'original' })
  state.events.mockResolvedValue([
    historicalEvent,
    {
      ...historicalEvent,
      name: 'started',
      type: 'Normal',
      reason: 'Started',
      message: 'Container started',
    },
  ])
  await render('events')
  const warning = [...document.querySelectorAll('.ant-segmented-item')].find(
    (item) => item.textContent === 'Warning',
  ) as HTMLElement
  await act(async () => warning.click())
  expect(document.querySelectorAll('.soha-pod-detail-timeline-item')).toHaveLength(1)
  expect(document.querySelector('.soha-pod-detail-timeline')?.textContent).toContain(
    'Historical readiness timeout',
  )
  expect(document.querySelector('.soha-pod-detail-timeline')?.textContent).not.toContain(
    'Container started',
  )
  const all = [...document.querySelectorAll('.ant-segmented-item')].find(
    (item) => item.textContent === '全部',
  ) as HTMLElement
  await act(async () => all.click())
  const input = document.querySelector('input[aria-label="搜索事件"]') as HTMLInputElement
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      input,
      'Container started',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  expect(document.querySelectorAll('.soha-pod-detail-timeline-item')).toHaveLength(1)
  expect(document.querySelector('.soha-pod-detail-timeline')?.textContent).toContain(
    'Container started',
  )
})

it('uses the shared initial skeleton and retains the time tree during background refresh', async () => {
  state.pod.mockResolvedValue({ uid: 'original' })
  let finish!: (events: (typeof historicalEvent)[]) => void
  state.events.mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  await render('events')
  expect(document.querySelector('.soha-pod-detail-events.ant-card-loading')).not.toBeNull()
  await act(async () => finish([historicalEvent]))
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 40))
  })
  await clickAndSettle('刷新 Pod 事件')
  expect(document.querySelector('.soha-pod-detail-events.ant-card-loading')).toBeNull()
  expect(document.querySelector('.soha-pod-detail-timeline')?.textContent).toContain(
    'Historical readiness timeout',
  )
  await act(async () => finish([historicalEvent]))
})
