/** @vitest-environment jsdom */
import { act, useState } from 'react'
import type { ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { PodResourceCell } from './pod-resource-cell'
import { PodContainersCell } from './pod-containers-cell'
import { parseCpuValue, parseMemoryValue } from '@/features/platform/workloads-model'

type OverlayProps = {
  children: ReactNode
  open?: boolean
  onOpenChange?: (open: boolean) => void
  title?: ReactNode
  content?: ReactNode
  trigger?: Array<'hover' | 'click' | 'focus'>
}

const renders = vi.hoisted(() => ({ resource: 0, popover: 0 }))
const overlays = vi.hoisted(() => ({
  tooltip: null as OverlayProps | null,
  popover: null as OverlayProps | null,
  realPopover: false,
  realTooltip: false,
}))
vi.mock('@/features/platform/workloads-model', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/platform/workloads-model')>()
  return {
    ...actual,
    parseCpuValue: vi.fn(actual.parseCpuValue),
    parseMemoryValue: vi.fn(actual.parseMemoryValue),
  }
})
vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  return {
    ...actual,
    Tooltip: (props: OverlayProps) => {
      renders.resource += 1
      overlays.tooltip = props
      if (overlays.realTooltip) return <actual.Tooltip {...props} />
      return (
        <div data-tooltip data-open={props.open}>
          {props.children}
          {props.open ? <div role="tooltip">{props.title}</div> : null}
        </div>
      )
    },
    Popover: (props: OverlayProps) => {
      renders.popover += 1
      overlays.popover = props
      if (overlays.realPopover) return <actual.Popover {...props} />
      return (
        <div data-popover data-open={props.open}>
          {props.children}
          {props.open ? props.content : null}
        </div>
      )
    },
  }
})
let root: ReturnType<typeof createRoot>
let host: HTMLDivElement
let focusFrame: FrameRequestCallback | undefined
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  renders.resource = 0
  renders.popover = 0
  overlays.tooltip = null
  overlays.popover = null
  overlays.realPopover = false
  overlays.realTooltip = false
  vi.mocked(parseCpuValue).mockClear()
  vi.mocked(parseMemoryValue).mockClear()
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    focusFrame = callback
    return 1
  })
  vi.stubGlobal('cancelAnimationFrame', () => {
    focusFrame = undefined
  })
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

it.each([300, 1000])('skips unchanged metric cells when %i Pod records refresh', async (count) => {
  const records = Array.from({ length: count }, (_, i) => ({
    name: `pod-${i}`,
    cpu: '10m',
    memory: '2Mi',
    requests: { cpu: '100m', memory: '4Mi' },
    limits: { cpu: '1', memory: '8Mi' },
    ageSeconds: 1,
  }))
  const render = async (rows: typeof records) =>
    act(async () =>
      root.render(
        <>
          {rows.map((pod) => (
            <div key={pod.name}>
              <PodResourceCell pod={pod} resource="cpu" localeCode="zh_CN" />
              <PodResourceCell pod={pod} resource="memory" localeCode="zh_CN" />
            </div>
          ))}
        </>,
      ),
    )
  await render(records)
  const initialCpu = vi.mocked(parseCpuValue).mock.calls.length
  const initialMemory = vi.mocked(parseMemoryValue).mock.calls.length
  const cpuCalculationsPerCell = initialCpu / count
  expect(cpuCalculationsPerCell).toBeGreaterThan(0)
  expect(initialMemory).toBeGreaterThan(0)
  expect(host.querySelectorAll('button')).toHaveLength(count * 2)
  expect(host.querySelector('button')?.getAttribute('aria-label')).toContain('pod-0 · CPU')
  expect(host.querySelectorAll('button')[1]?.getAttribute('aria-label')).toContain('内存')
  await render(
    records.map((pod) => ({
      ...pod,
      ageSeconds: 2,
      requests: { ...pod.requests },
      limits: { ...pod.limits },
    })),
  )
  console.info(
    'PODS_METRIC_CALCULATION_COUNT',
    JSON.stringify({
      rows: count,
      initialCpu,
      initialMemory,
      afterUnchangedRefresh: {
        cpu: vi.mocked(parseCpuValue).mock.calls.length,
        memory: vi.mocked(parseMemoryValue).mock.calls.length,
      },
    }),
  )
  expect(parseCpuValue).toHaveBeenCalledTimes(initialCpu)
  expect(parseMemoryValue).toHaveBeenCalledTimes(initialMemory)
  await render(records.map((pod, i) => (i === 0 ? { ...pod, cpu: '20m' } : { ...pod })))
  expect(parseCpuValue).toHaveBeenCalledTimes(initialCpu + cpuCalculationsPerCell)
  expect(parseMemoryValue).toHaveBeenCalledTimes(initialMemory)
  expect(host.querySelector('button')?.textContent).toBe('20m')
})

it('updates requests, limits, names, locale and resource without retaining stale labels', async () => {
  const pod = {
    name: 'pod-a',
    cpu: '10m',
    memory: '2Mi',
    requests: { cpu: '100m' },
    limits: { cpu: '1' },
  }
  const render = async (
    record = pod,
    resource: 'cpu' | 'memory' = 'cpu',
    localeCode: 'zh_CN' | 'en_US' = 'zh_CN',
  ) =>
    act(async () =>
      root.render(<PodResourceCell pod={record} resource={resource} localeCode={localeCode} />),
    )
  await render()
  const initial = vi.mocked(parseCpuValue).mock.calls.length
  expect(initial).toBeGreaterThan(0)
  await render({ ...pod, requests: { cpu: '200m' } })
  expect(parseCpuValue).toHaveBeenCalledTimes(initial * 2)
  expect(host.querySelector('button')?.getAttribute('aria-label')).toContain('请求 200m')
  await render({ ...pod, limits: { cpu: '2' } })
  expect(parseCpuValue).toHaveBeenCalledTimes(initial * 3)
  expect(host.querySelector('button')?.getAttribute('aria-label')).toContain('限制 2000m')
  await render({ ...pod, name: 'pod-b' })
  expect(host.querySelector('button')?.getAttribute('aria-label')).toContain('pod-b')
  await render(pod, 'memory', 'en_US')
  expect(host.querySelector('button')?.getAttribute('aria-label')).toContain('Memory')
})

it('skips unrelated parent updates but refreshes container readiness', async () => {
  const render = async (ready = '1/1', zh = true) =>
    act(async () => root.render(<PodContainersCell readyContainers={ready} zh={zh} />))
  await render()
  expect(renders.popover).toBe(0)
  await act(async () => host.querySelector('button')!.click())
  const initial = renders.popover
  expect(initial).toBeGreaterThan(0)
  await render()
  expect(renders.popover).toBe(initial)
  await render('0/1')
  expect(renders.popover).toBe(initial + 1)
  expect(host.querySelector('button')?.getAttribute('aria-label')).toContain('0/1')
  await render('0/1', false)
  expect(host.querySelector('button')?.getAttribute('aria-label')).toContain('Container states')
})

it.each(['focus', 'click', 'hover'])(
  'creates and opens resource details on the first %s only',
  async (interaction) => {
    const pod = { name: 'pod-a', cpu: '10m' }
    await act(async () =>
      root.render(<PodResourceCell pod={pod} resource="cpu" localeCode="zh_CN" />),
    )
    expect(renders.resource).toBe(0)
    const original = host.querySelector('button')!
    await act(async () => {
      if (interaction === 'focus') original.focus()
      else if (interaction === 'click') original.click()
      else original.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
    })
    expect(overlays.tooltip?.open).toBe(true)
    const trigger = host.querySelector('button')!
    if (interaction === 'focus') expect(document.activeElement).toBe(trigger)
    await act(async () => overlays.tooltip?.onOpenChange?.(false))
    expect(host.querySelector('[data-tooltip]')).not.toBeNull()
    expect(host.querySelector('button')).toBe(trigger)
    await act(async () =>
      root.render(
        <PodResourceCell pod={{ ...pod, cpu: '20m' }} resource="cpu" localeCode="zh_CN" />,
      ),
    )
    expect(trigger.textContent).toBe('20m')
    expect(trigger.getAttribute('aria-label')).toContain('使用 20m')
  },
)

it.each(['Enter', ' '])(
  'opens container details on the first %j and returns focus on Escape',
  async (key) => {
    await act(async () => root.render(<PodContainersCell readyContainers="1/1" zh />))
    expect(renders.popover).toBe(0)
    const original = host.querySelector('button')!
    await act(async () => {
      original.focus()
      original.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
    })
    const trigger = host.querySelector('button')!
    expect(overlays.popover?.open).toBe(true)
    expect(document.activeElement).toBe(trigger)
    await act(async () => focusFrame?.(0))
    const details = host.querySelector<HTMLElement>('[role="region"]')!
    expect(document.activeElement).toBe(details)
    await act(async () =>
      details.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    expect(overlays.popover?.open).toBe(false)
    expect(host.querySelector('[data-popover]')).not.toBeNull()
    expect(host.querySelector('button')).toBe(trigger)
    expect(document.activeElement).toBe(trigger)
  },
)

it.each(['click', 'hover'])(
  'opens container details on first %s and updates visible readiness',
  async (interaction) => {
    await act(async () => root.render(<PodContainersCell readyContainers="1/1" zh />))
    const original = host.querySelector('button')!
    await act(async () => {
      if (interaction === 'click') {
        original.focus()
        original.click()
      } else original.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
    })
    if (interaction === 'hover')
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 110))
      })
    expect(overlays.popover?.open).toBe(true)
    if (interaction === 'click') expect(document.activeElement).toBe(host.querySelector('button'))
    await act(async () => root.render(<PodContainersCell readyContainers="0/1" zh={false} />))
    expect(host.querySelector('[role="region"]')?.textContent).toContain('App containers ready 0/1')
  },
)

it('matches the original Popover for a first mouseenter, mousedown, focus and click sequence', async () => {
  vi.useFakeTimers()
  overlays.realPopover = true
  const { Popover } = await import('antd')
  function Original() {
    const [open, setOpen] = useState(false)
    return (
      <Popover
        open={open}
        onOpenChange={setOpen}
        trigger={['hover', 'click']}
        content="Original details"
      >
        <button data-original aria-expanded={open}>
          Original
        </button>
      </Popover>
    )
  }
  const renderPair = async (key: string) =>
    act(async () =>
      root.render(
        <div key={key}>
          <Original />
          <PodContainersCell readyContainers="1/1" zh />
        </div>,
      ),
    )
  const enter = async (selector: string) =>
    act(async () =>
      host.querySelector(selector)!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })),
    )
  const click = async (selector: string) => {
    await act(async () => {
      const button = host.querySelector<HTMLButtonElement>(selector)!
      button.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      button.focus()
      button.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }))
      button.click()
    })
  }
  await renderPair('quick-click')
  await enter('[data-original]')
  await click('[data-original]')
  expect(host.querySelector('[data-original]')?.getAttribute('aria-expanded')).toBe('true')
  await enter('.soha-pod-container-cell')
  await click('.soha-pod-container-cell')
  expect(host.querySelector('.soha-pod-container-cell')?.getAttribute('aria-expanded')).toBe('true')
  for (const selector of ['[data-original]', '.soha-pod-container-cell']) {
    await renderPair(`hover-open:${selector}`)
    await enter(selector)
    await act(async () => vi.advanceTimersByTime(110))
    expect(host.querySelector(selector)?.getAttribute('aria-expanded')).toBe('true')
    await click(selector)
    expect(host.querySelector(selector)?.getAttribute('aria-expanded')).toBe('false')
  }
  await renderPair('cancel-hover')
  const trigger = host.querySelector('.soha-pod-container-cell')!
  await enter('.soha-pod-container-cell')
  await act(async () =>
    trigger.dispatchEvent(
      new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body }),
    ),
  )
  await act(async () => vi.advanceTimersByTime(110))
  expect(host.querySelector('.soha-pod-container-cell')).toBe(trigger)
  expect(trigger.getAttribute('aria-expanded')).toBe('false')
  await enter('.soha-pod-container-cell')
  await act(async () =>
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
  )
  await act(async () => vi.advanceTimersByTime(110))
  expect(host.querySelector('.soha-pod-container-cell')).toBe(trigger)
  expect(trigger.getAttribute('aria-expanded')).toBe('false')
  await enter('.soha-pod-container-cell')
  await act(async () => root.render(null))
  const mountedOverlays = renders.popover
  await act(async () => vi.advanceTimersByTime(110))
  expect(renders.popover).toBe(mountedOverlays)
})

it('matches the original Tooltip after the first mouseenter, mousedown, focus and click sequence', async () => {
  overlays.realTooltip = true
  const { Tooltip } = await import('antd')
  function Original() {
    const [open, setOpen] = useState(false)
    return (
      <Tooltip
        title="Original details"
        trigger={['hover', 'focus', 'click']}
        onOpenChange={setOpen}
      >
        <button data-original-resource aria-expanded={open}>
          Original
        </button>
      </Tooltip>
    )
  }
  await act(async () =>
    root.render(
      <>
        <Original />
        <PodResourceCell pod={{ name: 'pod-a', cpu: '10m' }} resource="cpu" localeCode="zh_CN" />
      </>,
    ),
  )
  const enterAndClick = async (selector: string) => {
    await act(async () =>
      host.querySelector(selector)!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })),
    )
    await act(async () => {
      const button = host.querySelector<HTMLButtonElement>(selector)!
      button.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      button.focus()
      button.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }))
      button.click()
    })
  }
  await enterAndClick('[data-original-resource]')
  const originalOpen =
    host.querySelector('[data-original-resource]')?.getAttribute('aria-expanded') === 'true'
  await enterAndClick('.soha-pod-resource-limit-cell')
  expect(overlays.tooltip?.open).toBe(originalOpen)
  expect(originalOpen).toBe(false)
})
