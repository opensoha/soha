/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { WorkloadRefresh, useWorkloadRefresh } from './workload-refresh'

vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'zh_CN' }) }))
let host: HTMLDivElement
let root: ReturnType<typeof createRoot>
let settings: ReturnType<typeof useWorkloadRefresh>
let query = { isFetching: false, isError: false, isEnabled: true }
const refresh = vi.fn()
function Harness({
  compact = false,
  disabled = false,
  beforeRefresh,
}: {
  compact?: boolean
  disabled?: boolean
  beforeRefresh?: ReactNode
}) {
  settings = useWorkloadRefresh()
  return (
    <WorkloadRefresh
      settings={settings}
      queries={[query]}
      onRefresh={refresh}
      resourceLabel="DaemonSet"
      compact={compact}
      disabled={disabled}
      beforeRefresh={beforeRefresh}
    />
  )
}
beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
afterEach(async () => {
  await act(async () => root?.unmount())
  host?.remove()
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  refresh.mockReset()
})
async function render(fields: Partial<typeof query> = {}) {
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  query = { isFetching: false, isError: false, isEnabled: true, ...fields }
  host = document.createElement('div')
  root = createRoot(host)
  await act(async () => root.render(<Harness />))
  return host.querySelector('[role="switch"]') as HTMLButtonElement
}
describe('shared workload refresh', () => {
  it('places status before supplied resource actions in keyboard order and retains both callbacks', async () => {
    await render()
    const scale = vi.fn()
    await act(async () =>
      root.render(<Harness beforeRefresh={<button onClick={scale}>扩缩容</button>} />),
    )
    const buttons = Array.from(host.querySelectorAll('button'))
    expect(buttons[0].getAttribute('role')).toBe('switch')
    expect(buttons[1].textContent).toBe('扩缩容')
    expect(buttons[2].getAttribute('aria-label')).toBe('刷新 DaemonSet')
    await act(async () => buttons[1].click())
    await act(async () => buttons[2].click())
    expect(scale).toHaveBeenCalledOnce()
    expect(refresh).toHaveBeenCalledOnce()
  })
  it('keeps collection actions compact and disables controls without a readable scope', async () => {
    await render({ isEnabled: false })
    await act(async () => root.render(<Harness compact disabled />))
    const toggle = host.querySelector<HTMLButtonElement>('[role="switch"]')!
    const manual = host.querySelector<HTMLButtonElement>('[aria-label="刷新 DaemonSet"]')!
    expect(toggle.disabled).toBe(true)
    expect(manual.disabled).toBe(true)
    expect(manual.textContent).toBe('')
    expect(toggle.dataset.refreshState).toBe('paused')
    await act(async () => manual.click())
    expect(refresh).not.toHaveBeenCalled()
  })
  it.each([
    [{}, 'live', 'success'],
    [{ isFetching: true }, 'refreshing', 'warning'],
    [{ isError: true }, 'failed', 'error'],
    [{ isFetching: true, isError: true }, 'refreshing', 'warning'],
    [{ isEnabled: false, isError: true }, 'paused', 'default'],
  ] as const)('shows query-backed state %s', async (fields, state, color) => {
    const toggle = await render(fields)
    expect(toggle.dataset.refreshState).toBe(state)
    expect(host.querySelector(`.ant-badge-status-${color}`)).not.toBeNull()
    expect(toggle.getAttribute('aria-label')).toContain('自动刷新')
    const manual = host.querySelector('[aria-label="刷新 DaemonSet"]') as HTMLButtonElement
    expect(manual.classList.contains('ant-btn-loading')).toBe(Boolean(query.isFetching))
    await act(async () => manual.click())
    expect(refresh).toHaveBeenCalledTimes(query.isFetching ? 0 : 1)
  })
  it('starts a real 15 second cadence and stops that query after errors', async () => {
    await render()
    expect(settings.interval({ state: { status: 'success' } })).toBe(15_000)
    expect(settings.interval({ state: { status: 'error' } })).toBe(false)
  })
  it('lets keyboard-compatible button activation pause and resume auto refresh', async () => {
    const toggle = await render()
    await act(async () => toggle.click())
    expect(toggle.getAttribute('aria-checked')).toBe('false')
    expect(toggle.dataset.refreshState).toBe('paused')
    expect(settings.interval({ state: { status: 'success' } })).toBe(false)
    await act(async () => toggle.click())
    expect(toggle.getAttribute('aria-checked')).toBe('true')
    expect(settings.interval({ state: { status: 'success' } })).toBe(15_000)
  })
  it('pauses in background pages and resumes when visible', async () => {
    const toggle = await render()
    await act(async () => {
      Object.defineProperty(document, 'hidden', { configurable: true, value: true })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(toggle.dataset.refreshState).toBe('paused')
    expect(settings.interval({ state: { status: 'success' } })).toBe(false)
    await act(async () => {
      Object.defineProperty(document, 'hidden', { configurable: true, value: false })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(toggle.dataset.refreshState).toBe('live')
    expect(settings.interval({ state: { status: 'success' } })).toBe(15_000)
  })
  it('keeps failed data red when automatic refresh is switched off and retains manual retry', async () => {
    const toggle = await render({ isError: true })
    await act(async () => toggle.click())
    expect(toggle.dataset.refreshState).toBe('failed')
    await act(async () =>
      (host.querySelector('[aria-label="刷新 DaemonSet"]') as HTMLButtonElement).click(),
    )
    expect(refresh).toHaveBeenCalledOnce()
  })
})
