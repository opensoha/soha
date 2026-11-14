/** @vitest-environment jsdom */
import { act, type ReactNode, type ChangeEventHandler } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { OutpostRuntimePanel } from './outpost-runtime-panel'
const state = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  permissionKeys: ['identity.outposts.view', 'identity.outposts.update'],
}))
vi.mock('@/services/api-client', () => ({ api: state }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: { permissionKeys: state.permissionKeys } } }),
  hasPermission: (snapshot: { permissionKeys: string[] } | undefined, permission: string) =>
    snapshot?.permissionKeys.includes(permission) ?? false,
}))
vi.mock('@/components/request-feedback', () => ({
  ReadFeedback: () => null,
  OperationFeedback: ({ error }: { error?: Error }) => <div>{error?.message}</div>,
}))
vi.mock('antd', () => ({
  Space: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Alert: ({ title, description }: { title: ReactNode; description: ReactNode }) => (
    <div>
      {title}
      {description}
    </div>
  ),
  Button: ({
    children,
    disabled,
    onClick,
  }: {
    children: ReactNode
    disabled?: boolean
    onClick: () => void
  }) => (
    <button disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
  Typography: { Text: ({ children }: { children: ReactNode }) => <span>{children}</span> },
  Descriptions: ({ items }: { items: { key: string; label: string; children: ReactNode }[] }) => (
    <dl>
      {items.map((item) => (
        <div key={item.key}>
          {item.label}: {item.children}
        </div>
      ))}
    </dl>
  ),
  Input: ({
    value,
    onChange,
    disabled,
    'aria-label': label,
  }: {
    value: string
    onChange: ChangeEventHandler<HTMLInputElement>
    disabled: boolean
    'aria-label': string
  }) => <input aria-label={label} value={value} onChange={onChange} disabled={disabled} />,
  InputNumber: ({
    value,
    onChange,
    disabled,
    'aria-label': label,
  }: {
    value: number
    onChange: (value: number) => void
    disabled: boolean
    'aria-label': string
  }) => (
    <input
      type="number"
      aria-label={label}
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      disabled={disabled}
    />
  ),
  Checkbox: ({
    children,
    checked,
    onChange,
    disabled,
  }: {
    children: ReactNode
    checked: boolean
    onChange: ChangeEventHandler<HTMLInputElement>
    disabled: boolean
  }) => (
    <label>
      <input type="checkbox" checked={checked} onChange={onChange} disabled={disabled} />
      {children}
    </label>
  ),
}))
const runtime = {
  outpostId: 'node',
  owner: 'soha',
  generation: 3,
  desiredReplicas: 2,
  phase: 'ready',
  capabilities: { supportsScale: true, supportsDrain: true },
}
let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
let client: QueryClient
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function mount(value: typeof runtime & { slotId?: string; latestTaskId?: string } = runtime) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  state.get.mockResolvedValue({ data: value })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <OutpostRuntimePanel id="node" />
      </QueryClientProvider>,
    )
  })
  await settle()
}
function button(label: string) {
  return Array.from(container.querySelectorAll('button')).find(
    (item) => item.textContent === label,
  )!
}
async function click(label: string) {
  await act(async () => {
    button(label).click()
  })
  await settle()
}
afterEach(async () => {
  if (root) await act(async () => root.unmount())
  container?.remove()
  client?.clear()
  vi.clearAllMocks()
  state.permissionKeys = ['identity.outposts.view', 'identity.outposts.update']
})
describe('Outpost runtime management', () => {
  it('keeps unobserved replicas unknown and blocks external unsupported control', async () => {
    await mount({
      ...runtime,
      owner: 'external',
      capabilities: { supportsScale: false, supportsDrain: false },
    })
    expect(container.textContent).toContain('2 / 未知 / 未知')
    expect(container.textContent).toContain('外部部署不会自动接管')
    expect(container.textContent).toContain('没有其他活动控制者')
    expect(container.textContent).toContain('含首次安装')
    expect(button('启动').disabled).toBe(true)
    expect(button('停止').disabled).toBe(true)
    expect(button('调整副本').disabled).toBe(true)
  })
  it('retries unknown outcomes with identical intent and fences new operations', async () => {
    await mount()
    state.post
      .mockRejectedValueOnce(new ApiError(0, 'lost', { method: 'POST' }))
      .mockResolvedValueOnce({ data: { runtime } })
    await click('启动')
    expect(button('停止').disabled).toBe(true)
    expect(container.textContent).toContain('结果未知')
    await click('重试原操作')
    expect(state.post.mock.calls).toHaveLength(2)
    expect(state.post.mock.calls[0]).toEqual(state.post.mock.calls[1])
    expect(state.post.mock.calls[0][1]).toMatchObject({ action: 'start', expectedGeneration: 3 })
  })
  it('restores unknown intent after closing and reopening the detail in the same session', async () => {
    await mount()
    state.post
      .mockRejectedValueOnce(new ApiError(0, 'lost', { method: 'POST' }))
      .mockResolvedValueOnce({ data: { runtime } })
    await click('启动')
    await act(async () => root.unmount())
    root = createRoot(container)
    await act(async () =>
      root.render(
        <QueryClientProvider client={client}>
          <OutpostRuntimePanel id="node" />
        </QueryClientProvider>,
      ),
    )
    await settle()
    expect(button('启动').disabled).toBe(true)
    await click('重试原操作')
    expect(state.post.mock.calls[0]).toEqual(state.post.mock.calls[1])
  })
  it('allows first start after binding an approved slot with no runtime task yet', async () => {
    await mount({
      ...runtime,
      phase: 'pending',
      slotId: 'approved-slot',
      latestTaskId: undefined,
    })
    expect(container.textContent).toContain('槽位已绑定，尚未观察到部署')
    expect(button('启动').disabled).toBe(false)
    state.post.mockResolvedValueOnce({ data: { runtime } })
    await click('启动')
    expect(state.post).toHaveBeenCalledWith(
      '/identity/outposts/node/runtime/control',
      expect.objectContaining({ action: 'start', expectedGeneration: 3 }),
    )
    state.get.mockResolvedValue({
      data: {
        ...runtime,
        phase: 'pending',
        slotId: 'approved-slot',
        latestTaskId: 'task-first-start',
      },
    })
    await click('重新读取部署')
    expect(button('启动').disabled).toBe(true)
  })
  it('reconciles unknown writes read-only and requires coherent fresh observations plus confirmation', async () => {
    await mount({ ...runtime, slotId: 'approved-slot' })
    state.post.mockRejectedValueOnce(new ApiError(0, 'write lost', { method: 'POST' }))
    await click('启动')
    state.get.mockResolvedValue({
      data: { ...runtime, slotId: 'approved-slot', phase: 'unknown', latestTaskId: 'old-write' },
    })
    await click('重新读取部署')
    expect(button('启动').disabled).toBe(true)
    expect(button('核对实际部署').disabled).toBe(false)
    state.post.mockRejectedValueOnce(new ApiError(0, 'read lost', { method: 'POST' }))
    await click('核对实际部署')
    expect(button('启动').disabled).toBe(true)
    expect(state.post.mock.calls[1][1]).toMatchObject({
      action: 'reconcile',
      expectedGeneration: 3,
    })
    expect(state.post.mock.calls[1][1]).not.toHaveProperty('replicas')
    expect(state.post.mock.calls[1][1]).not.toHaveProperty('drainTimeoutSeconds')
    expect(
      client
        .getMutationCache()
        .getAll()
        .filter((item) => item.state.status === 'error'),
    ).toHaveLength(2)
    const observed = {
      ...runtime,
      slotId: 'approved-slot',
      generation: 4,
      observedGeneration: 4,
      observedAt: '2026-11-07T12:00:00Z',
      actualReplicas: 0,
      readyReplicas: 0,
      phase: 'failed',
      reason: 'drain_held',
      latestTaskId: 'reconcile-task',
    }
    state.post.mockResolvedValueOnce({ data: { runtime: { ...observed, phase: 'pending' } } })
    state.get.mockResolvedValue({ data: { ...observed, observedGeneration: 3 } })
    await click('重试原核对')
    expect(state.post.mock.calls[1]).toEqual(state.post.mock.calls[2])
    expect(button('启动').disabled).toBe(true)
    expect(button('核对已完成，确认继续管理')).toBeUndefined()
    state.get.mockResolvedValue({ data: observed })
    await click('重新读取部署')
    expect(button('启动').disabled).toBe(true)
    expect(container.textContent).toContain('drain_held')
    await click('核对已完成，确认继续管理')
    expect(button('启动').disabled).toBe(false)
    expect(container.textContent).toContain('drain_held')
  })
  it('pauses new operations when the server reports an unknown managed deployment', async () => {
    await mount({ ...runtime, phase: 'unknown' })
    expect(button('启动').disabled).toBe(true)
    expect(container.textContent).toContain('部署仍在进行中或结果未知')
  })
  it('requires a read and explicit confirmation after generation conflict', async () => {
    await mount()
    state.post.mockRejectedValueOnce(new ApiError(409, 'conflict', { method: 'POST' }))
    await click('启动')
    expect(button('启动').disabled).toBe(true)
    expect(button('重试原操作')).toBeUndefined()
    state.get.mockResolvedValue({ data: { ...runtime, generation: 4 } })
    await click('重新读取并确认调整')
    expect(button('启动').disabled).toBe(false)
    state.post.mockResolvedValueOnce({ data: { runtime: { ...runtime, generation: 5 } } })
    await click('启动')
    expect(state.post.mock.calls[1][1].expectedGeneration).toBe(4)
    expect(state.post.mock.calls[1][1].requestId).not.toBe(state.post.mock.calls[0][1].requestId)
  })
  it('blocks stop and reduction without drain capability', async () => {
    await mount({ ...runtime, capabilities: { supportsScale: true, supportsDrain: false } })
    expect(button('启动').disabled).toBe(false)
    expect(button('停止').disabled).toBe(true)
    expect(button('调整副本').disabled).toBe(true)
  })
  it('allows canonical update permission without legacy manage permission', async () => {
    await mount({ ...runtime, slotId: 'approved-slot' })
    expect(state.permissionKeys).not.toContain('identity.outposts.manage')
    expect(button('启动').disabled).toBe(false)
    expect(button('调整副本').disabled).toBe(false)
    expect(button('停止').disabled).toBe(false)
    expect(button('核对实际部署').disabled).toBe(false)
    expect(container.querySelector<HTMLInputElement>('[aria-label="安装槽位 ID"]')?.disabled).toBe(
      false,
    )
    expect(container.querySelector<HTMLInputElement>('input[type="checkbox"]')?.disabled).toBe(
      false,
    )
    state.post.mockResolvedValueOnce({ data: { runtime } })
    await click('启动')
    expect(state.post).toHaveBeenCalledWith(
      '/identity/outposts/node/runtime/control',
      expect.objectContaining({ action: 'start', expectedGeneration: 3 }),
    )
  })
  it.each([
    ['identity.outposts.view'],
    ['identity.outposts.view', 'identity.outposts.create'],
    ['identity.outposts.create'],
  ])('does not grant writes for permissions %j', async (...permissionKeys) => {
    state.permissionKeys = permissionKeys
    await mount({ ...runtime, slotId: 'approved-slot' })
    for (const label of ['启动', '停止', '调整副本', '核对实际部署', '绑定批准的槽位']) {
      expect(button(label)?.disabled ?? true).toBe(true)
    }
    if (permissionKeys.includes('identity.outposts.view')) {
      expect(container.textContent).toContain('没有管理运行部署的权限')
    } else {
      expect(container.textContent).toContain('没有查看运行部署的权限')
      expect(state.get).not.toHaveBeenCalled()
    }
    expect(state.post).not.toHaveBeenCalled()
    expect(state.put).not.toHaveBeenCalled()
  })
})
