/** @vitest-environment jsdom */
import { act } from 'react'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { CapabilityTasksPage } from './page'

const mocks = vi.hoisted(() => ({
  allowed: true,
  get: vi.fn(),
  getEnvelope: vi.fn(),
  post: vi.fn(),
}))
vi.mock('@/services/api-client', () => ({ api: mocks }))
vi.mock('@/features/auth/permission-snapshot', async () => ({
  ...(await vi.importActual('@/features/auth/permission-snapshot')),
  usePermissionSnapshot: () => ({
    data: {
      data: { permissionKeys: mocks.allowed ? ['ai.gateway.invoke'] : [], visibleMenuIds: [] },
    },
    isLoading: false,
  }),
}))
const task = {
  id: 'goal-1',
  version: 7,
  planVersion: 2,
  status: 'waiting_approval',
  createdBy: 'user',
  createdAt: '2026-09-15T01:00:00Z',
  updatedAt: '2026-09-15T01:00:00Z',
  plan: { goal: '部署并验证服务', steps: [], verificationSteps: [] },
  nodes: [{ id: 'deploy', status: 'waiting_approval', approvalRequestId: 'approval-1' }],
  assessment: { verdict: 'inconclusive', summary: '等待发布后的证据', evidence: [] },
}
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
let client: QueryClient
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}
async function render(path = '/ai-workbench/tasks') {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <App>
          <MemoryRouter initialEntries={[path]}>
            <CapabilityTasksPage />
          </MemoryRouter>
        </App>
      </QueryClientProvider>,
    ),
  )
  await flush()
}
function button(label: string) {
  return Array.from(document.body.querySelectorAll('button')).find(
    (element) => element.textContent?.replace(/\s/g, '') === label,
  ) as HTMLButtonElement
}
beforeAll(() => {
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
    value: vi.fn(() => ({
      matches: false,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  Object.defineProperty(window, 'getComputedStyle', {
    writable: true,
    value: vi.fn(() => ({
      width: '0px',
      height: '0px',
      overflow: 'auto',
      getPropertyValue: () => '',
    })),
  })
})
beforeEach(() => {
  mocks.allowed = true
  vi.clearAllMocks()
  mocks.getEnvelope.mockResolvedValue({ items: [task] })
  mocks.get.mockResolvedValue({ data: task })
  mocks.post.mockResolvedValue({ data: { valid: true, digest: 'validated', issues: [] } })
})
afterEach(async () => {
  if (root) await act(async () => root.unmount())
  client?.clear()
  container?.remove()
})

it('opens the same task ID and links its pending approval without claiming verification succeeded', async () => {
  await render('/ai-workbench/tasks?taskId=goal-1')
  expect(mocks.get).toHaveBeenCalledWith('/ai-gateway/tasks/goal-1')
  expect(document.body.textContent).toContain('等待发布后的证据')
  expect(
    document.body.querySelector(
      'a[href="/ai-gateway/governance?tab=approvals&approvalRequestId=approval-1"]',
    ),
  ).not.toBeNull()
  expect(button('取消任务')).toBeTruthy()
})

it('reads historical revisions without exposing actions for the old plan', async () => {
  await render('/ai-workbench/tasks?taskId=goal-1&planVersion=1')
  expect(mocks.get).toHaveBeenCalledWith('/ai-gateway/tasks/goal-1?planVersion=1')
  expect(button('取消任务')).toBeUndefined()
  expect(button('修订并续接')).toBeUndefined()
  expect(document.body.querySelector('a[href*="approvalRequestId"]')).toBeNull()
})

it('invalidates successful validation when the plan text changes', async () => {
  await render()
  await act(async () => button('提交计划').click())
  await flush()
  expect(button('提交').disabled).toBe(true)
  await act(async () => button('校验计划').click())
  await flush()
  expect(button('提交').disabled).toBe(false)
  const input = document.querySelector<HTMLTextAreaElement>('#capability-plan-json')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      input,
      '{"changed":true}',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await flush()
  expect(button('提交').disabled).toBe(true)
  expect(mocks.post.mock.calls.every(([path]) => path === '/ai-gateway/plans/validate')).toBe(true)
})

it('does not fetch tasks without invoke permission', async () => {
  mocks.allowed = false
  await render('/ai-workbench/tasks?taskId=goal-1')
  expect(mocks.get).not.toHaveBeenCalled()
  expect(mocks.getEnvelope).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('缺少目标任务访问权限')
})
