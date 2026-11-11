/** @vitest-environment jsdom */
import { act } from 'react'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { HealingPage } from './page'

const mocks = vi.hoisted(() => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn() },
  allowActions: true,
}))
vi.mock('@/services/api-client', () => ({ api: mocks.api }))
vi.mock('@/features/auth', () => ({
  hasPermission: () => mocks.allowActions,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))

const policy = {
  id: 'policy-1',
  name: '重启异常工作负载',
  triggerMode: 'approval_then_auto',
  workflowTemplateId: 'restart',
  cooldownSeconds: 300,
  safetyWindowSeconds: 600,
  enabled: true,
}
const run = {
  id: 'run-1',
  policyId: 'policy-1',
  eventId: 'event-1',
  status: 'pending',
  approvalStatus: 'pending',
  createdAt: '2026-09-22T02:00:00Z',
}
let root: Root
let container: HTMLDivElement
let client: QueryClient

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
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
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
beforeEach(() => {
  mocks.allowActions = true
  mocks.api.get.mockImplementation(async (path: string) => ({
    data: path === '/healing-policies' ? [policy] : [run],
  }))
  mocks.api.post.mockResolvedValue({ data: {} })
})
afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
  client?.clear()
  vi.clearAllMocks()
})

async function settle(action: () => void = () => {}) {
  await act(async () => {
    action()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function render() {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <App>
        <QueryClientProvider client={client}>
          <HealingPage />
        </QueryClientProvider>
      </App>,
    ),
  )
  await settle()
}
function panel() {
  const selected = container.querySelector('[role="tab"][aria-selected="true"]')!
  return document.getElementById(selected.getAttribute('aria-controls')!)!
}
async function selectTab(label: string) {
  const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find(
    (item) => item.textContent === label,
  )!
  expect(tab).toBeTruthy()
  await settle(() => tab.click())
}
function button(label: string) {
  return Array.from(panel().querySelectorAll('button')).find(
    (item) =>
      item.getAttribute('aria-label') === label || item.textContent?.replace(/\s/g, '') === label,
  )
}

describe('HealingPage task tabs', () => {
  it('separates policies from execution records while keeping refresh and approval actions', async () => {
    await render()
    expect(panel().textContent).toContain(policy.name)
    expect(panel().textContent).not.toContain(run.id)
    expect(button('新建自愈策略')).toBeTruthy()
    expect(panel().querySelector('.soha-admin-table-header-main')?.textContent).toBe('')
    mocks.api.get.mockClear()
    await settle(() => button('刷新列表')!.click())
    expect(mocks.api.get).toHaveBeenCalledWith('/healing-policies')

    await selectTab('执行记录')
    expect(panel().textContent).toContain(run.id)
    expect(button('新建自愈策略')).toBeUndefined()
    expect(panel().querySelector('.soha-admin-table-header-main')?.textContent).toBe('')
    mocks.api.get.mockClear()
    await settle(() => button('刷新列表')!.click())
    expect(mocks.api.get).toHaveBeenCalledWith('/healing-runs')
    await settle(() => button('通过')!.click())
    expect(mocks.api.post).toHaveBeenCalledWith('/healing-runs/run-1/approve', {
      comment: 'approved from console',
    })
    await selectTab('自愈策略')
    expect(panel().textContent).toContain(policy.name)
  })

  it('keeps failed policy loading independent from the execution records tab', async () => {
    let rejectPolicies!: (reason: Error) => void
    mocks.api.get.mockImplementation((path: string) =>
      path === '/healing-policies'
        ? new Promise((_resolve, reject) => {
            rejectPolicies = reject
          })
        : Promise.resolve({ data: [run] }),
    )
    await render()
    expect(panel().querySelector('.ant-spin-spinning')).not.toBeNull()
    await settle(() => rejectPolicies(new Error('策略读取失败')))
    expect(panel().textContent).toContain('策略读取失败')
    expect(button('刷新列表')).toBeTruthy()
    await selectTab('执行记录')
    expect(panel().textContent).toContain(run.id)
    expect(panel().textContent).not.toContain('策略读取失败')
  })

  it('preserves action permissions and empty states across both tabs', async () => {
    mocks.allowActions = false
    await render()
    expect(button('新建自愈策略')).toBeUndefined()
    expect(button('编辑自愈策略')).toBeUndefined()
    await selectTab('执行记录')
    for (const action of ['通过', '拒绝', '重试']) expect(button(action)).toBeUndefined()
    mocks.api.get.mockResolvedValue({ data: [] })
    await settle(() => button('刷新列表')!.click())
    expect(panel().textContent).toContain('暂无数据')
    expect(panel().textContent).not.toContain(run.id)
    await selectTab('自愈策略')
    await settle(() => button('刷新列表')!.click())
    expect(panel().textContent).toContain('暂无数据')
  })
})
