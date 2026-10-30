/** @vitest-environment jsdom */
import { App as AntdApp } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { PodDetailMetrics, podMetricLatest } from './pod-detail-metrics'
import { PodDetailEvents, podEventTimestamp } from './pod-detail-events'
import type { ResourceMetrics } from '@/types'

vi.mock('@visactor/react-vchart', () => ({ LineChart: () => <div data-testid="metric-chart" /> }))
vi.mock('@/i18n', () => ({
  localeText: (_locale: string, zh: string) => zh,
  useI18n: () => ({ localeCode: 'zh_CN', t: (_key: string, fallback: string) => fallback }),
}))
const roots: Root[] = []
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
  window.matchMedia = vi.fn(() => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
  })) as unknown as typeof window.matchMedia
})
afterEach(async () => {
  await act(async () => roots.splice(0).forEach((root) => root.unmount()))
  document.body.replaceChildren()
})
async function render(node: ReactNode) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  roots.push(root)
  await act(async () => root.render(<AntdApp>{node}</AntdApp>))
  return container
}
const metrics: ResourceMetrics = {
  resourceKind: 'Pod',
  resourceName: 'pod-a',
  configured: true,
  source: 'prometheus',
  generatedAt: '2026-10-30T10:00:00Z',
  rangeMinutes: 60,
  stepSeconds: 60,
  series: [
    {
      key: 'cpu',
      label: 'CPU',
      unit: 'cores',
      latest: 0,
      points: [{ timestamp: '2026-10-30T10:00:00Z', value: 0 }],
    },
    {
      key: 'network_rx',
      label: 'RX',
      unit: 'bytes/s',
      latest: 2048,
      points: [{ timestamp: '2026-10-30T10:00:00Z', value: 2048 }],
    },
  ],
}
describe('Pod detail real-data panels', () => {
  it('renders exactly six cards and distinguishes known zero from missing series', async () => {
    const container = await render(
      <PodDetailMetrics
        data={metrics}
        rangeMinutes={60}
        onRangeChange={vi.fn()}
        onRefresh={vi.fn()}
      />,
    )
    expect(container.querySelectorAll('.soha-pod-detail-metric-card')).toHaveLength(6)
    expect(container.textContent).toContain('0 mCPU')
    expect(container.querySelectorAll('[data-testid="metric-chart"]')).toHaveLength(2)
    expect(container.querySelectorAll('.soha-pod-detail-metric-missing')).toHaveLength(4)
    expect(container.textContent).not.toContain('计算与内存')
    expect(podMetricLatest(undefined)).toBeUndefined()
    expect(podMetricLatest({ ...metrics.series![0], latest: Number.NaN })).toBeUndefined()
  })
  it('keeps a failed read separate from an empty monitoring result', async () => {
    const container = await render(
      <PodDetailMetrics
        data={metrics}
        errorMessage="endpoint unreachable"
        rangeMinutes={60}
        onRangeChange={vi.fn()}
        onRefresh={vi.fn()}
      />,
    )
    expect(container.textContent).toContain('endpoint unreachable')
    expect(container.querySelector('[data-testid="metric-chart"]')).toBeNull()
  })
  it('uses the event timestamp, preserves repeat counts and never creates condition history', async () => {
    const observedAt = Date.parse('2026-10-30T20:00:00Z')
    const event = {
      name: 'backoff',
      namespace: 'monitoring',
      type: 'Warning',
      reason: 'BackOff',
      message: 'Restart failed',
      count: 7,
      ageSeconds: 5,
      lastTimestamp: '2026-10-30T19:24:12Z',
    }
    expect(podEventTimestamp(event, observedAt)).toBe(Date.parse(event.lastTimestamp))
    expect(podEventTimestamp({ ...event, lastTimestamp: undefined }, observedAt)).toBe(
      observedAt - 5000,
    )
    const container = await render(
      <PodDetailEvents
        events={[event]}
        observedAt={observedAt}
        loading={false}
        onRefresh={vi.fn()}
      />,
    )
    expect(container.textContent).toContain('BackOff')
    expect(container.textContent).toContain('7 次')
    expect(container.querySelector('time')?.getAttribute('datetime')).toBe(
      event.lastTimestamp.replace('Z', '.000Z'),
    )
    expect(container.textContent).not.toContain('Initialized')
  })
})
