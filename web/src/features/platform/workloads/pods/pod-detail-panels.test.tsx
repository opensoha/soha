/** @vitest-environment jsdom */
import { App as AntdApp } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { PodDetailMetrics, podMetricLatest } from './pod-detail-metrics'
import { PodDetailEvents, podEventTimestamp } from './pod-detail-events'
import { PodDetailOverview, podContainerTone } from './pod-detail-overview'
import { podContainerTerminated } from './container-status'
import { PodDetailStorage } from './pod-detail-storage'
import { MemoryRouter } from 'react-router-dom'
import type { PodDetail, ResourceMetrics } from '@/types'

vi.mock('@visactor/react-vchart', () => ({
  LineChart: ({ spec }: { spec: { background?: string } }) => (
    <div data-testid="metric-chart" data-background={spec.background} />
  ),
}))
vi.mock('@/i18n', () => ({
  localeText: (_locale: string, zh: string) => zh,
  useI18n: () => ({ localeCode: 'zh_CN', t: (_key: string, fallback: string) => fallback }),
}))
const roots: Root[] = []
beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  // JSDOM cannot measure pseudo-element scrollbars; browser tests own that geometry.
  const computedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => computedStyle(element))
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
  it('understands the actual terminated:reason detail format', () => {
    const init = {
      name: 'init',
      image: 'init:v1',
      role: 'init',
      ready: false,
      restartCount: 0,
      state: 'terminated:Completed',
    }
    expect(podContainerTerminated(init)).toBe(true)
    expect(podContainerTone(init)).toBe('success')
    expect(podContainerTone({ ...init, state: 'terminated:OOMKilled' })).toBe('danger')
    expect(podContainerTone({ ...init, state: 'waiting:CrashLoopBackOff' })).toBe('danger')
  })
  it('exposes a local metric refresh and preserves charts during a background read', async () => {
    const refresh = vi.fn()
    const container = await render(
      <PodDetailMetrics
        data={metrics}
        rangeMinutes={60}
        onRangeChange={vi.fn()}
        onRefresh={refresh}
        refreshing
      />,
    )
    expect(container.textContent).toContain('Prometheus')
    expect(container.querySelectorAll('[data-testid="metric-chart"]')).toHaveLength(2)
    expect(container.querySelectorAll('.ant-card-loading')).toHaveLength(0)
    const button = container.querySelector<HTMLButtonElement>('button[aria-label="刷新指标"]')!
    expect(button).not.toBeNull()
    expect(button.className).toContain('ant-btn-loading')
    // Loading blocks duplicate requests; the same control works again after completion.
    const ready = await render(
      <PodDetailMetrics
        data={metrics}
        rangeMinutes={60}
        onRangeChange={vi.fn()}
        onRefresh={refresh}
      />,
    )
    await act(async () =>
      ready.querySelector<HTMLButtonElement>('button[aria-label="刷新指标"]')!.click(),
    )
    expect(refresh).toHaveBeenCalledOnce()
  })
  it('shows unconfigured monitoring separately from a successful zero sample', async () => {
    const container = await render(
      <PodDetailMetrics
        data={{ ...metrics, configured: false, series: [] }}
        rangeMinutes={60}
        onRangeChange={vi.fn()}
        onRefresh={vi.fn()}
      />,
    )
    expect(container.textContent).toContain('尚未接入监控')
    expect(container.querySelector('[data-testid="metric-chart"]')).toBeNull()
    expect(container.textContent).not.toContain('0 mCPU')
  })
  it('preserves tiny CPU readings and lets the card theme show through the chart', async () => {
    const container = await render(
      <PodDetailMetrics
        data={{ ...metrics, series: [{ ...metrics.series![0], latest: 0.0000005 }] }}
        rangeMinutes={60}
        onRangeChange={vi.fn()}
        onRefresh={vi.fn()}
      />,
    )
    expect(container.textContent).toContain('0.0005 mCPU')
    expect(
      container.querySelector('[data-testid="metric-chart"]')?.getAttribute('data-background'),
    ).toBe('transparent')
  })
  it('uses the shared overview and does not call a failed init container healthy', async () => {
    const pod = {
      name: 'pod-a',
      namespace: 'team-a',
      phase: 'Running',
      containers: [
        {
          name: 'app',
          image: 'app:v1',
          role: 'main',
          state: 'running',
          ready: true,
          restartCount: 0,
        },
        {
          name: 'init',
          image: 'init:v1',
          role: 'init',
          state: 'terminated',
          reason: 'Error',
          ready: false,
          restartCount: 1,
        },
      ],
      conditions: [{ type: 'Ready', status: 'True' }],
      labels: { app: 'api' },
    } as PodDetail
    const container = await render(
      <PodDetailOverview
        pod={pod}
        zh
        expanded={[]}
        onExpand={vi.fn()}
        onContainerAction={vi.fn()}
        canLogs
        canExec
        nodeLink="worker-a"
        namespaceLink="team-a"
        accountLink="runner"
        ownerLink="owner"
        onEvents={vi.fn()}
      />,
    )
    expect(container.querySelector('.soha-workload-overview')).not.toBeNull()
    expect(container.textContent).toContain('检查 Pod 运行状态')
    expect(container.textContent).not.toContain('Pod 运行正常')
    expect(container.querySelector('.soha-pod-detail-container-label')?.textContent).toContain(
      'init',
    )
    const labels = Array.from(
      container.querySelectorAll<HTMLButtonElement>('[role="button"]'),
    ).find((button) => button.textContent?.includes('标签 · 1'))!
    await act(async () => labels.click())
    expect(container.querySelector('.soha-workload-overview-metadata')?.textContent).toContain(
      'api',
    )
    expect(
      container.querySelector('.soha-workload-overview-metadata .ant-typography-copy'),
    ).not.toBeNull()
  })
  it('shows volume source, per-container paths and effective read-only state without pagination', async () => {
    const container = await render(
      <MemoryRouter>
        <PodDetailStorage
          zh
          clusterId="cluster-a"
          namespace="team-a"
          volumes={[
            {
              name: 'data',
              type: 'PersistentVolumeClaim',
              sourceName: 'data-claim',
              details: ['PVC: data-claim', 'Storage class: standard'],
              readOnly: false,
              volumeMounts: [
                { name: 'app', mountPath: '/data', subPath: 'logs', readOnly: true },
                { name: 'init', mountPath: '/seed', readOnly: false },
              ],
            },
            { name: 'cache', type: 'EmptyDir', readOnly: false },
            {
              name: 'secret',
              type: 'Secret',
              sourceName: 'credentials',
              readOnly: false,
              volumeMounts: [{ name: 'app', mountPath: '/etc/credentials', readOnly: false }],
            },
          ]}
        />
      </MemoryRouter>,
    )
    expect(container.textContent).toContain('3 个卷')
    expect(container.querySelector('a')?.getAttribute('href')).toBe(
      '/storage/persistentvolumeclaims/data-claim?clusterId=cluster-a&namespace=team-a',
    )
    expect(container.querySelectorAll('.soha-pod-storage-mounts > div')).toHaveLength(3)
    expect(container.querySelectorAll('.soha-pod-storage-mounts > div')[2].textContent).toContain(
      '只读',
    )
    expect(container.textContent).toContain('/data')
    expect(container.textContent).toContain('subPath: logs')
    expect(container.textContent).toContain('只读')
    expect(container.textContent).toContain('读写')
    expect(container.textContent).toContain('未挂载')
    expect(container.textContent).toContain('Storage class: standard')
    expect(container.textContent).not.toContain('PVC: data-claim')
    expect(container.querySelector('.ant-pagination')).toBeNull()
  })
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
  it('keeps event refresh available after success and distinguishes unavailable counts', async () => {
    const refresh = vi.fn()
    const container = await render(
      <PodDetailEvents events={[]} observedAt={Date.now()} loading={false} onRefresh={refresh} />,
    )
    await act(async () =>
      container.querySelector<HTMLButtonElement>('button[aria-label="刷新 Pod 事件"]')!.click(),
    )
    expect(refresh).toHaveBeenCalledOnce()
    expect(container.textContent).toContain('0 条返回记录')
    const failed = await render(
      <PodDetailEvents
        events={[]}
        observedAt={Date.now()}
        loading={false}
        error="read failed"
        onRefresh={refresh}
      />,
    )
    expect(failed.textContent).toContain('— 条返回记录')
    expect(failed.textContent).toContain('read failed')
    expect(failed.textContent).not.toContain('没有匹配')
  })
})
