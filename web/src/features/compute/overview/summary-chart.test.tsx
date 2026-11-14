/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Bar, Pie } from '@ant-design/charts'
import { I18nProvider } from '@/i18n'
import { usePreferencesStore } from '@/stores/preferences-store'
import SummaryChart, { type SummaryChartProps } from './summary-chart'

vi.mock('@ant-design/charts', () => ({
  Pie: vi.fn(() => <div data-chart="pie" />),
  Bar: vi.fn(() => <div data-chart="bar" />),
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
let reducedMotion = false
const listeners = new Set<() => void>()

beforeEach(() => {
  reducedMotion = false
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === '(prefers-reduced-motion: reduce)' && reducedMotion,
    addEventListener: (_: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_: string, listener: () => void) => listeners.delete(listener),
  }))
  usePreferencesStore.setState({ localeCode: 'zh_CN', themeMode: 'light' })
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(() => {
  act(() => root.unmount())
  expect(listeners.size).toBe(0)
  container.remove()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  usePreferencesStore.setState({ localeCode: 'zh_CN', themeMode: 'light' })
})

function render(props: SummaryChartProps) {
  act(() =>
    root.render(
      <I18nProvider>
        <SummaryChart {...props} />
      </I18nProvider>,
    ),
  )
}

const hostItems: SummaryChartProps['items'] = [
  { label: '可用', value: 0, tone: 'success' },
  { label: '异常', value: 2, tone: 'danger' },
  { label: '等待 Agent', value: 0, tone: 'neutral' },
]

describe('compute summary chart', () => {
  it('plots real nonzero counts and updates the distribution after a refresh', () => {
    render({ items: hostItems, total: 2 })
    expect(vi.mocked(Pie).mock.lastCall?.[0]).toMatchObject({
      data: [{ label: '异常', value: 2 }],
      scale: { color: { domain: ['可用', '异常', '等待 Agent'] } },
      animate: { enter: { type: 'waveIn', duration: 700 }, update: { duration: 400 } },
    })
    render({
      items: [
        { ...hostItems[0], value: 3 },
        { ...hostItems[1], value: 1 },
      ],
      total: 4,
    })
    expect(vi.mocked(Pie).mock.lastCall?.[0].data).toEqual([
      { label: '可用', value: 3 },
      { label: '异常', value: 1 },
    ])
  })

  it('compares service and project counts without inventing a combined total', () => {
    render({
      kind: 'bars',
      items: [
        { label: '项目', value: 3, tone: 'neutral' },
        { label: '服务', value: 5, tone: 'violet' },
      ],
    })
    expect(Pie).not.toHaveBeenCalled()
    expect(vi.mocked(Bar).mock.lastCall?.[0]).toMatchObject({
      data: [
        { label: '项目', value: 3 },
        { label: '服务', value: 5 },
      ],
      xField: 'label',
      yField: 'value',
      animate: { enter: { type: 'growInX' } },
    })
  })

  it.each([
    { items: hostItems, total: 3 },
    { items: [{ ...hostItems[0], value: undefined }], total: 0 },
    { items: [{ ...hostItems[0], value: -1 }], total: -1 },
    { items: [{ ...hostItems[0], value: Number.NaN }], total: 0 },
  ])('does not draw a misleading proportion for incomplete or invalid data', (props) => {
    render(props)
    expect(container.textContent).toBe('数据不完整')
    expect(Pie).not.toHaveBeenCalled()
    expect(Bar).not.toHaveBeenCalled()
  })

  it('keeps real zero empty instead of manufacturing a chart segment', () => {
    render({ items: hostItems.map((item) => ({ ...item, value: 0 })), total: 0 })
    expect(container.textContent).toBe('暂无资源')
    expect(Pie).not.toHaveBeenCalled()
  })

  it('responds to reduced-motion changes and disables chart animation', () => {
    render({ items: hostItems, total: 2 })
    act(() => {
      reducedMotion = true
      listeners.forEach((listener) => listener())
    })
    expect(vi.mocked(Pie).mock.lastCall?.[0]).toMatchObject({ animate: false })
    render({ kind: 'bars', items: hostItems })
    expect(vi.mocked(Bar).mock.lastCall?.[0]).toMatchObject({ animate: false })
  })

  it('uses the current theme and localizes empty states', () => {
    usePreferencesStore.setState({ localeCode: 'en_US', themeMode: 'dark' })
    render({ items: hostItems, total: 2 })
    expect(vi.mocked(Pie).mock.lastCall?.[0].theme).toBe('dark')
    render({ items: [], total: 0 })
    expect(container.textContent).toBe('Incomplete data')
    render({ items: [{ ...hostItems[0], value: 0 }], total: 0 })
    expect(container.textContent).toBe('No resources')
  })
})
