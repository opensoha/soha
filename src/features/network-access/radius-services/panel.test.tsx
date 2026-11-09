/** @vitest-environment jsdom */
import { act } from 'react'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { RadiusServicesPanel } from './panel'
const api = vi.hoisted(() => ({
  options: vi.fn(),
  list: vi.fn(),
  create: vi.fn(),
  configure: vi.fn(),
  control: vi.fn(),
}))
vi.mock('./api', () => ({ radiusApi: api }))
const pool = {
  poolId: 'pool-1',
  name: 'RADIUS',
  siteId: 'site-1',
  nasBindingId: 'nas-1',
  configurationRef: 'approved-config',
  revision: 4,
  configurationRevision: 2,
  state: 'ready',
  desiredReplicas: 1,
  members: [
    {
      memberId: 'member-1',
      runtimeId: 'runtime-1',
      endpoint: '10.0.0.1:1812',
      ready: true,
      trafficReady: false,
      stateCountKnown: false,
    },
  ],
}
let root: Root
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
  const style = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => style(element))
})
beforeEach(() => {
  vi.clearAllMocks()
  api.options.mockResolvedValue({
    slots: [
      {
        slotId: 'slot-1',
        siteId: 'site-1',
        nasBindingId: 'nas-1',
        maxReplicas: 3,
        configurationRefs: ['approved-config', 'config-next'],
        available: false,
        reservedPoolId: 'pool-1',
        reasonCode: 'reserved',
      },
    ],
  })
  api.list.mockResolvedValue({ items: [pool] })
  api.control.mockResolvedValue({ ...pool, state: 'pending', operationId: 'op-1' })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const element = document.createElement('div')
  document.body.appendChild(element)
  root = createRoot(element)
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  document.body.innerHTML = ''
})
async function flush() {
  for (let i = 0; i < 10; i++)
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })
}
async function render(canUpdate = true, canRead = true, canCreate = false) {
  await act(async () =>
    root.render(
      <App>
        <QueryClientProvider client={client}>
          <RadiusServicesPanel canRead={canRead} canCreate={canCreate} canUpdate={canUpdate} />
        </QueryClientProvider>
      </App>,
    ),
  )
  await flush()
}
async function click(text: string) {
  const button = Array.from(document.querySelectorAll('button')).find(
    (b) => b.textContent?.replace(/\s/g, '') === text.replace(/\s/g, ''),
  )
  expect(button).toBeDefined()
  await act(async () => button?.click())
  await flush()
}
async function select(selector = '.ant-select', label?: string) {
  await act(async () =>
    document
      .querySelector(selector)
      ?.closest('.ant-select')
      ?.querySelector('.ant-select-content')
      ?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })),
  )
  await flush()
  await act(async () =>
    (label
      ? Array.from(document.querySelectorAll('.ant-select-item-option')).find(
          (element) => element.textContent === label,
        )
      : document.querySelector('.ant-select-item-option')
    )?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
  )
  await flush()
}
it('shows unknown observations and prevents scale before stop confirmation', async () => {
  await render()
  await select()
  expect(document.body.textContent).toContain('1 / 未知 / 未知')
  expect(document.body.textContent).toContain('待 NAS 使用观察')
  for (const label of ['调整副本', '启动'])
    expect(
      Array.from(document.querySelectorAll('button')).find(
        (b) => b.textContent?.replace(/\s/g, '') === label,
      )?.disabled,
    ).toBe(true)
  expect(document.body.textContent).toContain('stop_required')
})
it('creates only from approved slot references with the NAS binding CAS token', async () => {
  api.options.mockResolvedValue({
    slots: [
      {
        slotId: 'slot-new',
        siteId: 'site-1',
        nasBindingId: 'nas-1',
        maxReplicas: 2,
        configurationRefs: ['config-approved'],
        available: true,
        reasonCode: '',
        bindingExists: true,
        createApprovedBindingRequired: false,
        bindingUpdatedAt: '2026-11-08T00:00:00Z',
      },
    ],
  })
  api.create.mockResolvedValue({ ...pool, state: 'stopped' })
  await render(true, true, true)
  await click('创建托管池')
  const input = document.querySelector('#name') as HTMLInputElement
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(
      input,
      'New RADIUS',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await select('#slotId', 'slot-new / nas-1 / site-1')
  await select('#configurationRef', 'config-approved')
  await click('审核创建')
  expect(api.create).not.toHaveBeenCalled()
  await click('确定')
  expect(api.create).toHaveBeenCalledWith(
    expect.objectContaining({
      slotId: 'slot-new',
      configurationRef: 'config-approved',
      nasBindingId: 'nas-1',
      siteId: 'site-1',
      expectedBindingUpdatedAt: '2026-11-08T00:00:00Z',
      requestId: expect.any(String),
    }),
  )
})
it('requires observed zero before changing an approved configuration reference', async () => {
  api.list.mockResolvedValue({
    items: [
      {
        ...pool,
        slotId: 'slot-1',
        state: 'stopped',
        desiredReplicas: 0,
        observedReplicas: 0,
        members: [],
      },
    ],
  })
  api.configure.mockResolvedValue({ ...pool, state: 'stopped' })
  await render()
  await select()
  await select('[aria-label="新配置引用"]', 'config-next')
  await click('更新配置引用')
  await click('确定')
  expect(api.configure).toHaveBeenCalledWith(
    'pool-1',
    expect.objectContaining({
      configurationRef: 'config-next',
      expectedRevision: 4,
      requestId: expect.any(String),
    }),
  )
})
it.each(['accepted', 'rejected'])(
  'bootstrap NAS binding needs explicit consent: %s',
  async (result) => {
    api.options.mockResolvedValue({
      slots: [
        {
          slotId: 'slot-new',
          siteId: 'site-1',
          nasBindingId: 'nas-1',
          maxReplicas: 2,
          configurationRefs: ['config-approved'],
          available: true,
          reasonCode: '',
          bindingExists: false,
          createApprovedBindingRequired: true,
        },
      ],
    })
    if (result === 'rejected')
      api.create.mockRejectedValueOnce(new Error('approved_binding_conflict'))
    else api.create.mockResolvedValue({ ...pool, state: 'stopped' })
    await render(true, true, true)
    await click('创建托管池')
    const input = document.querySelector('#name') as HTMLInputElement
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(
        input,
        'Bootstrap RADIUS',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await select('#slotId', 'slot-new / nas-1 / site-1')
    await select('#configurationRef', 'config-approved')
    expect(
      Array.from(document.querySelectorAll('button')).find((b) => b.textContent === '审核创建')
        ?.disabled,
    ).toBe(true)
    expect(api.create).not.toHaveBeenCalled()
    await act(async () =>
      (document.querySelector('#createApprovedBinding') as HTMLInputElement).click(),
    )
    await click('审核创建')
    await click('确定')
    expect(api.create).toHaveBeenCalledWith(
      expect.objectContaining({
        createApprovedBinding: true,
        slotId: 'slot-new',
        nasBindingId: 'nas-1',
        siteId: 'site-1',
      }),
    )
    expect(api.create.mock.calls[0][0]).not.toHaveProperty('expectedBindingUpdatedAt')
    if (result === 'rejected') {
      expect(document.body.textContent).toContain('approved_binding_conflict')
      expect(document.querySelector('[role="dialog"]')).not.toBeNull()
      expect(api.create).toHaveBeenCalledTimes(1)
    }
  },
)
it('confirms a CAS request and describes accepted as pending observation', async () => {
  await render()
  await select()
  await click('停止新认证并排空')
  expect(api.control).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('版本：4')
  await click('确定')
  expect(api.control).toHaveBeenCalledWith(
    'pool-1',
    expect.objectContaining({
      action: 'stop',
      expectedRevision: 4,
      requestId: expect.any(String),
      drainSeconds: 90,
    }),
  )
  expect(document.body.textContent).toContain('当前状态 pending')
})
it('retains rejected CAS feedback without silent retry', async () => {
  api.control.mockRejectedValueOnce(new Error('revision_conflict'))
  await render()
  await select()
  await click('核对控制状态')
  await click('确定')
  expect(document.body.textContent).toContain('revision_conflict')
  expect(api.control).toHaveBeenCalledTimes(1)
})
it('clears confirmation and selection when permission is withdrawn', async () => {
  await render()
  await select()
  await click('停止新认证并排空')
  await render(false, false)
  expect(document.body.textContent).not.toContain('10.0.0.1:1812')
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(api.control).not.toHaveBeenCalled()
})
it.each(['never-started', 'expired'])(
  'RAD-F01 reconciled zero enables start/configure: %s',
  async (scenario) => {
    api.list.mockResolvedValue({
      items: [
        {
          ...pool,
          slotId: 'slot-1',
          state: 'stopped',
          desiredReplicas: 0,
          observedReplicas: 0,
          members: [
            {
              ...pool.members[0],
              ordinal: 0,
              leaseUntil: scenario === 'expired' ? '2000-01-01T00:00:00Z' : undefined,
              stateCountKnown: scenario === 'expired',
              activeStates: scenario === 'expired' ? 0 : undefined,
            },
          ],
        },
      ],
    })
    api.configure.mockResolvedValue({ ...pool, state: 'stopped' })
    await render()
    await select()
    expect(document.body.textContent).toContain('未知')
    const start = Array.from(document.querySelectorAll('button')).find(
      (b) => b.textContent?.replace(/\s/g, '') === '启动',
    )
    expect(start?.disabled).toBe(false)
    await click('启动')
    expect(api.control).not.toHaveBeenCalled()
    await click('确定')
    expect(api.control).toHaveBeenCalledWith(
      'pool-1',
      expect.objectContaining({ action: 'start', expectedRevision: 4 }),
    )
    await select('[aria-label="新配置引用"]', 'config-next')
    await click('更新配置引用')
    await click('确定')
    expect(api.configure).toHaveBeenCalledWith(
      'pool-1',
      expect.objectContaining({ configurationRef: 'config-next', expectedRevision: 4 }),
    )
  },
)
it.each([true, false])(
  'RAD-F01 scale ignores future unknown member, current known=%s',
  async (known) => {
    api.list.mockResolvedValue({
      items: [
        {
          ...pool,
          slotId: 'slot-1',
          observedReplicas: 1,
          members: [
            {
              ...pool.members[0],
              ordinal: 0,
              leaseUntil: new Date(Date.now() + 60000).toISOString(),
              stateCountKnown: known,
              activeStates: known ? 0 : undefined,
            },
            { ...pool.members[0], memberId: 'future-1', ordinal: 1, stateCountKnown: false },
          ],
        },
      ],
    })
    await render()
    await select()
    const scale = Array.from(document.querySelectorAll('button')).find(
      (b) => b.textContent?.replace(/\s/g, '') === '调整副本',
    )
    expect(scale?.disabled).toBe(!known)
    if (!known) {
      expect(api.control).not.toHaveBeenCalled()
      return
    }
    const input = document.querySelector('input[aria-label="目标 RADIUS 副本"]') as HTMLInputElement
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, '2')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await click('调整副本')
    expect(api.control).not.toHaveBeenCalled()
    await click('确定')
    expect(api.control).toHaveBeenCalledWith(
      'pool-1',
      expect.objectContaining({
        action: 'scale',
        replicas: 2,
        expectedRevision: 4,
        requestId: expect.any(String),
      }),
    )
  },
)
