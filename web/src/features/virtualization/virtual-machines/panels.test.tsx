/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { VMMetricsPanel } from './metrics-panel'
import { VMAccessPanel } from './access-panel'

const state = vi.hoisted(() => ({ read: vi.fn() }))
vi.mock('../queries', () => ({
  virtualizationQueries: {
    vmMetrics: () => ({ queryKey: ['metrics'], queryFn: state.read }),
    accessBinding: () => ({ queryKey: ['binding'], queryFn: state.read, retry: false }),
    platformClusterOptions: () => ({
      queryKey: ['clusters'],
      queryFn: async () => [],
      enabled: false,
    }),
  },
}))
vi.mock('../mutations', () => ({
  virtualizationMutations: { saveAccessBinding: () => ({ mutationFn: vi.fn() }) },
}))
vi.mock('../shared/use-virtualization-permissions', () => ({
  useVirtualizationPermissions: () => ({ canConnectVMs: true, canManageVMAccess: false }),
}))
vi.mock('@/features/auth', () => ({
  hasPermission: () => false,
  usePermissionSnapshot: () => ({ data: { data: { permissionKeys: [] } } }),
}))
vi.mock('@/features/secrets', () => ({
  secretQueries: {
    list: () => ({ queryKey: ['secrets'], queryFn: async () => [], enabled: false }),
  },
  buildSecretReference: () => '',
}))
vi.mock('./ssh-terminal', () => ({
  VMSSHTerminal: () => <div data-testid="ssh-engine">SSH engine</div>,
}))
vi.mock('@visactor/react-vchart', () => ({ LineChart: () => <div data-testid="metric-chart" /> }))
vi.mock('@/components/resource-metrics-panel', async () => {
  const format = await import('@/components/resource-metrics-format')
  return {
    ...format,
    compactMetricColors: { cpu: 'var(--soha-primary)' },
    buildCompactChartSpec: () => ({ axes: [{}, { expand: {} }] }),
  }
})

let root: Root
let container: HTMLDivElement
let client: QueryClient
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  state.read.mockReset()
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addListener: vi.fn(), removeListener: vi.fn() })
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
})
async function render(node: ReactNode) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <App>{node}</App>
      </QueryClientProvider>,
    )
    await new Promise((resolve) => setTimeout(resolve, 50))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 50))
  })
}
const metrics = {
  ready: true,
  source: 'provider-test',
  series: [
    { key: 'cpu', label: 'CPU', unit: 'percent', points: [{ timestamp: 1700000000, value: 1.25 }] },
  ],
}
describe('VM panel presentation and protected cache', () => {
  it('does not retain an SSH engine when the binding is confirmed absent', async () => {
    client.setQueryData(['binding'], {
      vmId: 'vm-test',
      revision: 1,
      username: 'soha',
      port: 22,
      address: '10.0.0.1',
    })
    state.read.mockRejectedValue(new ApiError(404, 'binding absent'))
    await render(
      <VMAccessPanel vm={{ id: 'vm-test', name: 'test', provider: 'pve', status: 'running' }} />,
    )
    expect(client.getQueryState(['binding'])?.status).toBe('error')
    expect(container.querySelector('[data-testid="ssh-engine"]')).toBeNull()
    expect(container.textContent).toContain('尚未配置 VM 登录绑定')
  })
  it('displays provider percent values without converting allocations into usage', async () => {
    state.read.mockResolvedValue(metrics)
    await render(<VMMetricsPanel range={60} vmId="vm-test" onRangeChange={vi.fn()} />)
    expect(container.textContent).toContain('1.25 %')
    expect(container.querySelector('[data-testid="metric-chart"]')).not.toBeNull()
  })
  for (const status of [401, 403]) {
    it(`removes cached metric series after ${status}`, async () => {
      client.setQueryData(['metrics'], metrics)
      state.read.mockRejectedValue(new ApiError(status, 'read denied'))
      await render(<VMMetricsPanel range={60} vmId="vm-test" onRangeChange={vi.fn()} />)
      expect(client.getQueryState(['metrics'])?.status).toBe('error')
      expect(container.querySelector('[data-testid="metric-chart"]')).toBeNull()
      expect(container.textContent).not.toContain('1.25 %')
      expect(container.textContent).not.toContain('provider-test')
    })
    it(`removes the cached SSH engine after ${status}`, async () => {
      client.setQueryData(['binding'], {
        vmId: 'vm-test',
        revision: 1,
        username: 'soha',
        port: 22,
        address: '10.0.0.1',
      })
      state.read.mockRejectedValue(new ApiError(status, 'read denied'))
      await render(
        <VMAccessPanel vm={{ id: 'vm-test', name: 'test', provider: 'pve', status: 'running' }} />,
      )
      expect(client.getQueryState(['binding'])?.status).toBe('error')
      expect(container.querySelector('[data-testid="ssh-engine"]')).toBeNull()
    })
  }
})
