/** @vitest-environment jsdom */
import { act } from 'react'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { ProgressiveRolloutStatus } from '@opensoha/contracts/gen/ts/sohaapi'
import type { ExecutionTask } from '../types'
import { ApiError } from '@/services/api-error'
import { ExecutionRolloutPanel } from './rollout-panel'

const mocks = vi.hoisted(() => ({ allowed: true, get: vi.fn(), post: vi.fn() }))
vi.mock('@/features/auth', () => ({
  hasPermission: () => mocks.allowed,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))
vi.mock('@/services/api-client', () => ({ api: { get: mocks.get, post: mocks.post } }))

const state: ProgressiveRolloutStatus = {
  name: 'web',
  namespace: 'test',
  uid: 'native-uid',
  resourceVersion: '42',
  operationId: 'operation',
  generation: 3,
  observedGeneration: 3,
  phase: 'Paused',
  strategy: 'canary',
  paused: false,
  aborted: false,
  stableRevision: 'stable',
  currentRevision: 'preview',
  activeService: 'web',
  previewService: 'web-preview',
  stableWeight: 80,
  canaryWeight: 20,
  currentStep: 1,
  totalSteps: 4,
  pauseReasons: ['CanaryPauseStep'],
  metrics: [
    {
      analysisRun: 'metrics',
      uid: 'analysis-uid',
      name: 'success',
      phase: 'Running',
      count: 1,
      successful: 1,
      failed: 0,
      interval: '10s',
      targetCount: 6,
      successCondition: 'result >= 0.99',
      value: '1',
    },
  ],
}
const task: ExecutionTask = {
  applicationId: 'app',
  providerKind: 'manifest',
  targetKind: 'kubernetes',
  maxRetries: 1,
  attemptCount: 1,
  timeoutSeconds: 300,
  createdAt: '2026-09-14T00:00:00Z',
  updatedAt: '2026-09-14T00:00:00Z',
  id: 'task:one',
  taskKind: 'manifest_apply',
  status: 'running',
  payload: {
    action: 'apply',
    documents: [{ kind: 'Rollout', apiVersion: 'argoproj.io/v1alpha1' }],
  },
  result: {},
}
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
let client: QueryClient

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi.fn().mockImplementation(() => ({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  mocks.allowed = true
  mocks.get.mockReset().mockResolvedValue({ data: state })
  mocks.post.mockReset().mockResolvedValue({ data: state })
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
})
async function render(value = task) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <App>
          <ExecutionRolloutPanel task={value} />
        </App>
      </QueryClientProvider>,
    ),
  )
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10))
  })
}
function button(label: string, scope: ParentNode = container) {
  const result = [...scope.querySelectorAll('button')].find(
    (item) => item.textContent?.replace(/\s/g, '') === label,
  )
  if (!result) throw new Error(`missing button ${label}`)
  return result
}

it.each([
  ['pause', '暂停发布', ''],
  ['promote', '提升阶段', '确认提升'],
  ['abort', '停止发布', '停止发布'],
])(
  'submits %s with fresh native identity and preserves the observation window',
  async (action, label, confirm) => {
    await render()
    expect(container.textContent).toContain('稳定 80 / 预览 20')
    expect(container.textContent).toContain('1 / 6 次')
    expect(container.textContent).toContain('运行中')
    mocks.get.mockResolvedValue({ data: { ...state, resourceVersion: '43' } })
    await act(async () => button(label).click())
    if (confirm)
      await act(async () => button(confirm, document.querySelector('.ant-popconfirm')!).click())
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    expect(mocks.post).toHaveBeenCalledWith('/delivery/execution-tasks/task%3Aone/rollout', {
      action,
      uid: 'native-uid',
      resourceVersion: '43',
    })
    expect(mocks.get.mock.calls.length).toBeGreaterThan(1)
  },
)

it('keeps historical task evidence and unknown weights without querying a newer rollout', async () => {
  await render({
    ...task,
    status: 'completed',
    result: { rollout: { ...state, stableWeight: 0, canaryWeight: undefined } },
  })
  expect(mocks.get).not.toHaveBeenCalled()
  expect(container.textContent).toContain('本次任务结束时的发布证据')
  expect(container.textContent).toContain('稳定 0 / 预览 —')
  expect(container.textContent).not.toContain('提升阶段')
})

it('hides controls without trigger permission and blocks writes after an observation error', async () => {
  mocks.allowed = false
  await render()
  expect(() => button('暂停发布')).toThrow('missing button 暂停发布')
  mocks.allowed = true
  await render()
  mocks.get.mockRejectedValue(new Error('原生状态冲突'))
  await act(async () => button('刷新').click())
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10))
  })
  expect(container.textContent).toContain('原生状态冲突')
  expect(button('暂停发布').disabled).toBe(true)
  expect(button('提升阶段').disabled).toBe(true)
  expect(mocks.post).not.toHaveBeenCalled()
})

it('keeps an uncertain native control visible and prevents repeat writes after a successful readback', async () => {
  await render()
  mocks.post.mockRejectedValueOnce(
    new ApiError(503, 'control response lost', {
      method: 'POST',
      requestId: 'req-control-unknown',
    }),
  )
  await act(async () => button('暂停发布').click())
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(container.textContent).toContain('请求结果不明')
  expect(container.textContent).toContain('req-control-unknown')
  await act(async () => button('刷新').click())
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(button('暂停发布').disabled).toBe(true)
  expect(button('提升阶段').disabled).toBe(true)
  expect(mocks.post).toHaveBeenCalledTimes(1)
})
