/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, beforeAll, expect, it, vi } from 'vitest'
import { CapacityPolicyDrawer } from './capacity-policy'
import type { VirtualizationCluster } from '../virtualization-types'

const state = vi.hoisted(() => ({
  save: vi.fn(),
  check: vi.fn(),
  policy: {
    mode: 'enforced',
    sourceId: 'source-one',
    sourceConfirmed: true,
    revision: 2,
    cpu: { enabled: true, percent: 200 },
    memory: { enabled: false, percent: 100 },
  },
}))
vi.mock('../queries', () => ({
  virtualizationQueries: {
    capacityPolicy: (id: string) => ({
      queryKey: ['policy', id],
      queryFn: async () => state.policy,
    }),
  },
}))
vi.mock('../mutations', () => ({
  virtualizationMutations: {
    saveCapacityPolicy: () => ({ mutationFn: state.save }),
    checkCapacity: () => ({ mutationFn: state.check }),
  },
}))
vi.mock('../shared/ui', () => ({
  VirtualizationAdminTable: ({ dataSource }: { dataSource: unknown[] }) => (
    <pre>{JSON.stringify(dataSource)}</pre>
  ),
}))
beforeAll(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  const computed = window.getComputedStyle
  window.getComputedStyle = (element) => computed(element)
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: () => ({
      matches: false,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }),
  })
})
let root: Root, node: HTMLDivElement, client: QueryClient
beforeEach(() => {
  vi.clearAllMocks()
  state.policy.sourceConfirmed = true
  state.save.mockResolvedValue(state.policy)
  state.check.mockResolvedValue({
    status: 'insufficient',
    reason: 'runtime memory exhausted',
    diagnostics: [
      {
        node: 'node-one',
        checks: [
          {
            check: 'runtime',
            dimension: 'memory',
            status: 'rejected',
            limit: 4096,
            pending: 1024,
            demand: 4096,
          },
        ],
      },
    ],
  })
  node = document.createElement('div')
  document.body.appendChild(node)
  root = createRoot(node)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  node.remove()
})
async function render(canUpdate = true) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <App>
          <CapacityPolicyDrawer
            connection={{ id: 'connection-one', name: 'PVE' } as VirtualizationCluster}
            canUpdate={canUpdate}
            onClose={() => undefined}
          />
        </App>
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
}
it('CPU disable submits 100 percent while preserving the independent memory policy', async () => {
  await render()
  const cpu = document.querySelector<HTMLButtonElement>('#cpu_enabled')
  expect(cpu?.getAttribute('aria-checked')).toBe('true')
  await act(async () => cpu?.click())
  const save = Array.from(document.querySelectorAll('button')).find(
    (button) => button.textContent === '保存容量策略',
  )
  await act(async () => {
    save?.click()
    await new Promise((resolve) => setTimeout(resolve, 40))
  })
  expect(state.save).toHaveBeenCalledWith(
    {
      id: 'connection-one',
      input: expect.objectContaining({
        expectedRevision: 2,
        cpu: { enabled: false, percent: 100 },
        memory: { enabled: false, percent: 100 },
      }),
    },
    expect.anything(),
  )
})
it('readonly source confirmation and preflight display actual-capacity rejection', async () => {
  state.policy.sourceConfirmed = false
  await render(false)
  expect(document.body.textContent).toContain('物理集群标识')
  expect(document.body.textContent).not.toContain('保存容量策略')
  const check = Array.from(document.querySelectorAll('button')).find(
    (button) => button.textContent === '检查两道准入',
  )
  await act(async () => {
    check?.click()
    await new Promise((resolve) => setTimeout(resolve, 40))
  })
  expect(state.check).toHaveBeenCalledWith(
    expect.objectContaining({ connectionId: 'connection-one', startAfterCreate: true }),
    expect.anything(),
  )
  expect(document.body.textContent).toContain('runtime memory exhausted')
  expect(document.body.textContent).toContain('"pending":1024')
})

it.each(['completed', 'pending'])(
  'does not show a %s preflight after its inputs change',
  async (mode) => {
    let finish!: (value: unknown) => void
    if (mode === 'pending')
      state.check.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve
          }),
      )
    await render(false)
    const check = Array.from(document.querySelectorAll('button')).find(
      (button) => button.textContent === '检查两道准入',
    )!
    await act(async () => {
      check.click()
      await new Promise((resolve) => setTimeout(resolve, 40))
    })
    if (mode === 'completed')
      expect(document.body.textContent).toContain('runtime memory exhausted')
    await act(async () => {
      const input = document.querySelector<HTMLInputElement>('input#cpu')!
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, '8')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 40))
    })
    if (mode === 'pending')
      await act(async () => {
        finish({ status: 'available', reason: 'old input result', diagnostics: [] })
        await new Promise((resolve) => setTimeout(resolve, 40))
      })
    expect(document.body.textContent).not.toContain('runtime memory exhausted')
    expect(document.body.textContent).not.toContain('old input result')
    expect(document.body.textContent).not.toContain('预检通过，提交时会重新检查')
  },
)
