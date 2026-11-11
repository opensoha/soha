/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { MonitoringPanel } from './types'
import { MonitoringChart, MonitoringSparkline } from './chart'

type ChartProps = {
  spec: Record<string, unknown>
  data: { id: string; values: Record<string, unknown>[] }
}
const state = vi.hoisted(() => ({
  area: [] as ChartProps[],
  line: [] as ChartProps[],
  motion: true,
  mode: 'light' as 'light' | 'dark',
}))
vi.mock('@visactor/react-vchart', () => ({
  AreaChart: (props: ChartProps) => {
    state.area.push(props)
    return <canvas />
  },
  LineChart: (props: ChartProps) => {
    state.line.push(props)
    return <canvas />
  },
}))
vi.mock('antd', () => ({
  theme: {
    useToken: () => ({
      token: {
        colorBgContainer: '#fff',
        colorPrimary: '#1677ff',
        motion: state.motion,
        motionDurationSlow: '0.3s',
      },
    }),
  },
}))
vi.mock('@/i18n', () => ({
  localeText: (_locale: string, _zh: string, en: string) => en,
  useI18n: () => ({ localeCode: 'en_US' }),
}))
vi.mock('@/stores/preferences-store', () => ({
  usePreferencesStore: (select: (state: { themeMode: string }) => unknown) =>
    select({ themeMode: state.mode }),
}))
vi.mock('@/theme/app-theme', () => ({
  resolveThemeMode: (value: string) => value,
  resolveThemeColorReference: (value: string) => value,
}))
let root: Root
beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
afterEach(async () => {
  await act(async () => root?.unmount())
  document.body.innerHTML = ''
  state.area = []
  state.line = []
  state.motion = true
  state.mode = 'light'
})
function mount() {
  const el = document.createElement('div')
  document.body.append(el)
  root = createRoot(el)
}

it('keeps configuration and canvas stable, only sends changed real samples through data', async () => {
  mount()
  const initial = [
    { time: '2026-10-19T07:58:00Z', value: null },
    { time: '2026-10-19T07:59:00Z', value: 0 },
  ]
  const render = (points: typeof initial) =>
    act(async () =>
      root.render(<MonitoringSparkline points={points} label="CPU" tone="metric" unit="cores" />),
    )
  await render(initial)
  const first = state.area.at(-1)!
  const canvas = document.querySelector('canvas')
  expect(first.spec).toMatchObject({
    data: { id: 'observations', values: [] },
    dataKey: 'time',
    invalidType: 'break',
    animationAppear: false,
    animationEnter: false,
    animationExit: false,
    animationUpdate: { duration: 300 },
  })
  await render(initial.map((point) => ({ ...point })))
  expect(state.area).toHaveLength(1)
  const next = [...initial.slice(1), { time: '2026-10-19T08:00:00Z', value: 2 }]
  await render(next)
  const second = state.area.at(-1)!
  expect(second.spec).toBe(first.spec)
  expect(second.data.values).toEqual(next)
  expect(document.querySelector('canvas')).toBe(canvas)
})

it('disables transition for historical data and reduced motion', async () => {
  mount()
  const points = [{ time: '2026-10-19T08:00:00Z', value: 1 }]
  await act(async () =>
    root.render(
      <MonitoringSparkline
        points={points}
        label="CPU"
        tone="metric"
        unit="cores"
        animated={false}
      />,
    ),
  )
  expect(state.area.at(-1)!.spec.animation).toBe(false)
  state.motion = false
  await act(async () =>
    root.render(
      <MonitoringSparkline points={points} label="CPU" tone="metric" unit="cores" animated />,
    ),
  )
  expect(state.area.at(-1)!.spec.animation).toBe(false)
})

it('shows ten node series, preserves UID and timestamp keys and updates only data', async () => {
  mount()
  const names = Array.from({ length: 11 }, (_, i) => `worker-${i}`)
  const panel: MonitoringPanel = {
    key: 'host.cpu.used',
    label: 'CPU',
    unit: 'cores',
    quality: 'valid',
    series: names.map((name, i) => ({
      nodeRef: {
        name,
        uid: `uid-${i}`,
        clusterId: 'prod',
        apiVersion: 'v1',
        kind: 'Node',
        scopeMode: 'cluster',
      },
      metricKey: 'host.cpu.used',
      unit: 'cores',
      source: 'prometheus',
      quality: 'valid',
      latest: i,
      sampledAt: '2026-10-19T08:00:00Z',
      points: [{ timestamp: '2026-10-19T08:00:00Z', value: i }],
    })),
  }
  await act(async () => root.render(<MonitoringChart panel={panel} names={names} />))
  const first = state.line.at(-1)!
  expect(new Set(first.data.values.map((point) => point.node))).toEqual(new Set(names.slice(0, 10)))
  expect(first.data.values[9].id).toBe('prod:worker-9:uid-9:2026-10-19T08:00:00Z')
  expect(first.spec).toMatchObject({ dataKey: 'id', invalidType: 'break', data: { values: [] } })
  const updated = {
    ...panel,
    series: panel.series.map((item) => ({
      ...item,
      points: [...item.points, { timestamp: '2026-10-19T08:01:00Z', value: 0 }],
    })),
  }
  await act(async () => root.render(<MonitoringChart panel={updated} names={names} />))
  expect(state.line.at(-1)!.spec).toBe(first.spec)
  expect(state.line.at(-1)!.data.values).toHaveLength(20)
  const noUID = {
    ...panel,
    series: panel.series.map((item) => ({ ...item, nodeRef: { ...item.nodeRef, uid: undefined } })),
  }
  await act(async () => root.render(<MonitoringChart panel={noUID} names={names} />))
  expect(new Set(state.line.at(-1)!.data.values.map((point) => point.id)).size).toBe(10)
})

it('updates the native chart theme without replacing the canvas', async () => {
  mount()
  const points = [{ time: '2026-10-19T08:00:00Z', value: 1 }]
  await act(async () =>
    root.render(<MonitoringSparkline points={points} label="CPU" tone="metric" unit="cores" />),
  )
  const canvas = document.querySelector('canvas')
  expect(state.area.at(-1)!.spec.theme).toBe('light')
  state.mode = 'dark'
  await act(async () =>
    root.render(
      <MonitoringSparkline points={points} label="CPU dark" tone="metric" unit="cores" />,
    ),
  )
  expect(state.area.at(-1)!.spec.theme).toBe('dark')
  expect(document.querySelector('canvas')).toBe(canvas)
})
