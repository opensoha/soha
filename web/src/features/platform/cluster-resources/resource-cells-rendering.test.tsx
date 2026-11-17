/** @vitest-environment jsdom */
import { act, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { NodeTaints, NodeUsageCell } from './node-cells'
import { NamespaceLabelsCell } from './namespace-labels-cell'
import { namespaceLabels } from './namespace-list-model'
import { formatMetricValue } from '@/components/resource-metrics-format'

const renders = vi.hoisted(() => ({ popover: 0, tooltip: 0, progress: [] as number[] }))
vi.mock('@/i18n', () => ({
  useI18n: () => ({ localeCode: 'zh_CN', t: (_key: string, fallback: string) => fallback }),
}))
vi.mock('./namespace-list-model', async (original) => {
  const actual = await original<typeof import('./namespace-list-model')>()
  return { ...actual, namespaceLabels: vi.fn(actual.namespaceLabels) }
})
vi.mock('@/components/resource-metrics-format', async (original) => {
  const actual = await original<typeof import('@/components/resource-metrics-format')>()
  return { ...actual, formatMetricValue: vi.fn(actual.formatMetricValue) }
})
vi.mock('antd', async (original) => {
  const actual = await original<typeof import('antd')>()
  return {
    ...actual,
    Popover: (props: ComponentProps<typeof actual.Popover>) => {
      renders.popover++
      return <actual.Popover {...props} />
    },
    Tooltip: (props: ComponentProps<typeof actual.Tooltip>) => {
      renders.tooltip++
      return <actual.Tooltip {...props} />
    },
    Progress: (props: ComponentProps<typeof actual.Progress>) => {
      renders.progress.push(props.percent ?? 0)
      return <actual.Progress {...props} />
    },
  }
})

let root: ReturnType<typeof createRoot>
let host: HTMLDivElement
const usage: ComponentProps<typeof NodeUsageCell>['usage'] = {
  value: 1,
  quality: 'valid',
  reason: undefined,
  sampledAt: '2026-11-17T01:00:00Z',
  total: 4,
  percent: 25,
}
const taints = [{ key: 'example.com/maintenance', value: 'task111', effect: 'NoSchedule' }]
async function key(element: Element, value: string) {
  await act(async () =>
    element.dispatchEvent(new KeyboardEvent('keydown', { key: value, bubbles: true })),
  )
}
async function advance(ms: number) {
  await act(async () => vi.advanceTimersByTimeAsync(ms))
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.useFakeTimers()
  renders.popover = 0
  renders.tooltip = 0
  renders.progress = []
  vi.mocked(namespaceLabels).mockClear()
  vi.mocked(formatMetricValue).mockClear()
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

it('mounts namespace labels and copy controls only on click and restores focus on Escape', async () => {
  const labels = { owner: 'team-a', 'kubernetes.io/metadata.name': 'team-a' }
  const render = () => act(async () => root.render(<NamespaceLabelsCell labels={labels} zh />))
  await render()
  await render()
  expect(namespaceLabels).toHaveBeenCalledTimes(1)
  expect(renders.popover).toBe(0)
  expect(host.querySelector('.ant-typography-copy')).toBeNull()
  const original = host.querySelector('button')!
  await act(async () => {
    original.focus()
    original.click()
  })
  await advance(50)
  expect(renders.popover).toBeGreaterThan(0)
  const trigger = host.querySelector<HTMLButtonElement>('.soha-namespace-label-preview')!
  expect(trigger.getAttribute('aria-expanded')).toBe('true')
  expect(document.activeElement).toBe(trigger)
  const details = document.querySelector('.soha-namespace-label-details')!
  expect(details.textContent).toContain('kubernetes.io/metadata.name')
  expect(details.querySelectorAll('.ant-typography-copy')).toHaveLength(2)
  const copy = details.querySelector<HTMLButtonElement>('.ant-typography-copy')!
  await act(async () => copy.focus())
  await key(copy, 'Escape')
  expect(trigger.getAttribute('aria-expanded')).toBe('false')
  expect(document.activeElement).toBe(trigger)
  await key(trigger, 'Enter')
  expect(trigger.getAttribute('aria-expanded')).toBe('true')
  await key(trigger, ' ')
  expect(trigger.getAttribute('aria-expanded')).toBe('false')
})

it('keeps namespace label changes visible after activation', async () => {
  await act(async () => root.render(<NamespaceLabelsCell labels={{ owner: 'team-a' }} zh />))
  await act(async () => host.querySelector<HTMLButtonElement>('button')!.click())
  await act(async () =>
    root.render(<NamespaceLabelsCell labels={{ owner: 'team-b', env: 'prod' }} zh={false} />),
  )
  expect(host.textContent).toContain('env=prod')
  expect(host.querySelector('button')?.getAttribute('aria-label')).toBe('View all 2 labels')
  expect(document.querySelector('.soha-namespace-label-details')?.textContent).toContain('team-b')
})

it('waits 100 ms on first taint hover, cancels short hover and keeps hover-then-click open', async () => {
  await act(async () => root.render(<NodeTaints taints={taints} compact />))
  const original = host.querySelector('button')!
  expect(renders.popover).toBe(0)
  await act(async () => original.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
  await advance(50)
  await act(async () => original.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })))
  await advance(100)
  expect(renders.popover).toBe(0)
  await act(async () => original.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
  await advance(99)
  expect(renders.popover).toBe(0)
  await act(async () => original.click())
  await advance(150)
  const trigger = host.querySelector<HTMLButtonElement>('button')!
  expect(trigger.getAttribute('aria-expanded')).toBe('true')
  expect(document.querySelector('.soha-node-taint-details')?.textContent).toContain(
    'example.com/maintenance=task111:NoSchedule',
  )
  await key(trigger, 'Escape')
  expect(trigger.getAttribute('aria-expanded')).toBe('false')
  expect(document.activeElement).toBe(trigger)
  await act(async () => trigger.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
  await advance(100)
  expect(trigger.getAttribute('aria-expanded')).toBe('true')
})

it('opens taints on the first delayed hover and through the keyboard while keeping unknown and empty states', async () => {
  await act(async () => root.render(<NodeTaints taints={taints} />))
  await act(async () =>
    host.querySelector('button')!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })),
  )
  await advance(100)
  let trigger = host.querySelector<HTMLButtonElement>('button')!
  expect(trigger.getAttribute('aria-expanded')).toBe('true')
  await key(trigger, 'Escape')
  await key(trigger, ' ')
  expect(trigger.getAttribute('aria-expanded')).toBe('true')
  await act(async () => root.render(<NodeTaints key="keyboard" taints={taints} compact />))
  trigger = host.querySelector<HTMLButtonElement>('button')!
  await act(async () => trigger.focus())
  await key(trigger, 'Enter')
  trigger = host.querySelector<HTMLButtonElement>('button')!
  expect(trigger.getAttribute('aria-expanded')).toBe('true')
  expect(document.activeElement).toBe(trigger)
  await act(async () => root.render(<NodeTaints taints={null} />))
  expect(host.textContent).toBe('未提供')
  await act(async () => root.render(<NodeTaints taints={[]} />))
  expect(host.textContent).toBe('无污点')
  await act(async () => root.render(<NodeTaints taints={taints} />))
  expect(host.querySelector('button')?.getAttribute('aria-expanded')).toBe('false')
})

it('skips equal usage objects and updates every metric field, kind, permission and failure', async () => {
  let props: ComponentProps<typeof NodeUsageCell> = { usage, kind: 'cpu' }
  const render = (patch: Partial<typeof props> = {}) => {
    props = { ...props, ...patch }
    return act(async () => root.render(<NodeUsageCell {...props} />))
  }
  await render()
  const calls = vi.mocked(formatMetricValue).mock.calls.length
  expect(renders.progress).toEqual([25])
  expect(renders.tooltip).toBe(0)
  await render({ usage: { ...usage } })
  expect(formatMetricValue).toHaveBeenCalledTimes(calls)
  for (const patch of [
    { value: 2 },
    { total: 8 },
    { percent: 50 },
    { quality: 'stale' as const },
    { reason: 'query_failed' as const },
    { sampledAt: '2026-11-17T02:00:00Z' },
  ]) {
    const before = vi.mocked(formatMetricValue).mock.calls.length
    await render({ usage: { ...usage, ...patch } })
    expect(vi.mocked(formatMetricValue).mock.calls.length).toBeGreaterThan(before)
  }
  await render({ usage: { ...usage, percent: 150 } })
  expect(renders.progress.at(-1)).toBe(100)
  await render({ usage: { ...usage, percent: -10 } })
  expect(renders.progress.at(-1)).toBe(0)
  await render({ kind: 'memory', usage: { ...usage, value: 1048576 } })
  expect(host.textContent).toContain('1.0 MB')
  await render({ canRead: false, usage: { ...usage, value: null, percent: null } })
  expect(host.querySelector('.soha-node-usage')?.getAttribute('aria-label')).toContain(
    '无监控查看权限',
  )
  await render({ canRead: true, failed: true })
  expect(host.querySelector('.soha-node-usage')?.getAttribute('aria-label')).toContain(
    '监控读取失败',
  )
  await render({ failed: false })
  expect(host.querySelector('.soha-node-usage')?.getAttribute('aria-label')).not.toContain(
    '监控读取失败',
  )
})

it('defers usage tooltips until first hover or focus and preserves capacity, sample and Escape', async () => {
  await act(async () => root.render(<NodeUsageCell kind="cpu" usage={usage} />))
  const original = host.querySelector<HTMLElement>('.soha-node-usage')!
  await act(async () => original.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })))
  await advance(99)
  expect(renders.tooltip).toBe(0)
  await advance(1)
  expect(renders.tooltip).toBeGreaterThan(0)
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('节点 CPU 容量快照')
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('采样')
  let trigger = host.querySelector<HTMLElement>('.soha-node-usage')!
  await act(async () => trigger.focus())
  await key(trigger, 'Escape')
  expect(document.activeElement).toBe(trigger)
  await act(async () => root.render(<NodeUsageCell key="focus" kind="cpu" usage={usage} />))
  trigger = host.querySelector<HTMLElement>('.soha-node-usage')!
  await act(async () => trigger.focus())
  trigger = host.querySelector<HTMLElement>('.soha-node-usage')!
  expect(document.activeElement).toBe(trigger)
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('主机实际使用')
  await key(trigger, 'Escape')
  await advance(300)
  expect(document.querySelector('.ant-tooltip-hidden')).not.toBeNull()
})
