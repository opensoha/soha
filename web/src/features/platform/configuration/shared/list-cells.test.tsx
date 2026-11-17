/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { ConfigurationLeasesPage } from '../leases/list-page'
import { ResourceQuotaUsage } from '../resourcequotas/quota-usage'

const state = vi.hoisted(() => ({ rawTime: '2026-11-17T02:03:04.123456Z', tooltip: vi.fn() }))
vi.mock('antd', async (original) => {
  const actual = await original<typeof import('antd')>()
  return {
    ...actual,
    Tooltip: (props: import('antd').TooltipProps) => {
      state.tooltip()
      return <actual.Tooltip {...props} />
    },
  }
})
vi.mock('./list-page', () => ({
  ConfigurationNameLink: () => null,
  ConfigurationResourceListPage: ({
    columns,
  }: {
    columns: Array<{ dataIndex?: string; render?: (value: string) => ReactNode }>
  }) => <>{columns.find((col) => col.dataIndex === 'renewTime')?.render?.(state.rawTime)}</>,
}))
let root: Root
let container: HTMLDivElement
const flush = async () => {
  await new Promise((resolve) => setTimeout(resolve, 30))
}
async function render(node: ReactNode) {
  if (!container) {
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
  }
  await act(async () => {
    root.render(<MemoryRouter>{node}</MemoryRouter>)
    await flush()
  })
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
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
})
afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
  container = undefined!
  vi.clearAllMocks()
})

describe('configuration rich list cells', () => {
  it('defers the quota tooltip but retains progress, raw amounts and changed usage', async () => {
    const record = {
      name: 'budget',
      namespace: 'team',
      ageSeconds: 60,
      hard: { pods: '10' },
      used: { pods: '9' },
    }
    await render(<ResourceQuotaUsage record={record} />)
    expect(state.tooltip).not.toHaveBeenCalled()
    expect(container.textContent).toContain('90%')
    await act(async () => {
      container.querySelector<HTMLElement>('[tabindex]')!.focus()
      await flush()
    })
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe('pods: 9 / 10')
    expect(document.activeElement).toBe(container.querySelector('[tabindex]'))
    await act(async () => {
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      )
      await flush()
    })
    expect(document.querySelector('.ant-tooltip-hidden')).not.toBeNull()
    await render(<ResourceQuotaUsage record={{ ...record, used: { pods: '11' } }} />)
    expect(container.textContent).toContain('110%')
    expect(container.querySelector('.is-over')).not.toBeNull()
    await render(<ResourceQuotaUsage record={{ ...record, hard: undefined, used: undefined }} />)
    expect(container.querySelector('.ant-progress')).toBeNull()
  })
  it('opens the exact Lease timestamp on first focus and updates invalid and missing readings', async () => {
    await render(<ConfigurationLeasesPage />)
    expect(state.tooltip).not.toHaveBeenCalled()
    expect(container.textContent).not.toContain('.123456Z')
    await act(async () => {
      container.querySelector<HTMLElement>('[tabindex]')!.focus()
      await flush()
    })
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe(state.rawTime)
    await act(async () => {
      document.activeElement!.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      )
      await flush()
    })
    expect(document.querySelector('.ant-tooltip-hidden')).not.toBeNull()
    state.rawTime = 'invalid-timestamp'
    await render(<ConfigurationLeasesPage />)
    expect(container.textContent).toBe('invalid-timestamp')
    state.rawTime = ''
    await render(<ConfigurationLeasesPage />)
    expect(container.textContent).toBe('—')
  })
})
