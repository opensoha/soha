/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { ResourceMonitoringEntry } from './page'
import { NetworkEntry, NetworkRoutingEntry } from '../network/group-entry'
const state = vi.hoisted(() => ({
  permissions: [] as string[],
  error: null as Error | null,
  refetch: vi.fn(),
}))
vi.mock('./runtime-page', () => ({ ResourceMonitoringRuntimePage: () => <Target /> }))
vi.mock('@/features/auth', async (original) => {
  const actual = await original<typeof import('@/features/auth')>()
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: state.error ? undefined : { data: { permissionKeys: state.permissions } },
      error: state.error,
      isError: Boolean(state.error),
      isLoading: false,
      isFetching: false,
      refetch: state.refetch,
    }),
  }
})
let root: Root | undefined
beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
afterEach(async () => {
  await act(async () => root?.unmount())
  document.body.innerHTML = ''
  state.error = null
  state.refetch.mockClear()
})
function Target() {
  const location = useLocation()
  return (
    <div data-target>
      {location.pathname}
      {location.search}
    </div>
  )
}
async function renderEntry(kind: 'monitoring' | 'network' | 'routing', permissions: string[]) {
  state.permissions = permissions
  const container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  const path =
    kind === 'monitoring'
      ? '/resource-monitoring'
      : kind === 'network'
        ? '/network'
        : '/network/routing'
  const entry =
    kind === 'monitoring' ? (
      <ResourceMonitoringEntry />
    ) : kind === 'network' ? (
      <NetworkEntry />
    ) : (
      <NetworkRoutingEntry />
    )
  await act(async () => {
    root!.render(
      <MemoryRouter initialEntries={[path + '?node=node-a&rangeMinutes=360']}>
        <Routes>
          <Route path={path} element={entry} />
          <Route path="*" element={<Target />} />
        </Routes>
      </MemoryRouter>,
    )
  })
  return container
}
describe('Kubernetes workbench navigation', () => {
  it('reports a failed permission read with retry instead of claiming access was denied', async () => {
    state.error = new Error('permission network unavailable')
    const container = await renderEntry('monitoring', [])
    expect(container.textContent).toContain('权限读取失败')
    expect(container.textContent).not.toContain('没有资源监控查看权限')
    const retry = container.querySelector<HTMLButtonElement>('button[aria-label="重试读取权限"]')
    expect(retry).not.toBeNull()
    await act(async () => retry!.click())
    expect(state.refetch).toHaveBeenCalledOnce()
  })
  it('opens the single monitoring dashboard and retains query state', async () => {
    const container = await renderEntry('monitoring', [
      'platform.nodes.view',
      'platform.resource-monitoring.cpu.view',
    ])
    expect(container.textContent).toBe('/resource-monitoring?node=node-a&rangeMinutes=360')
  })
  it('requires node access before entering a monitoring view', async () => {
    const container = await renderEntry('monitoring', ['platform.resource-monitoring.cpu.view'])
    expect(container.querySelector('[data-target]')).toBeNull()
  })
  it('opens services when topology permission is absent', async () => {
    const container = await renderEntry('network', ['platform.network.services.view'])
    expect(container.textContent).toContain('/network/services?')
  })
  it('does not expose a routing page for a services-only user', async () => {
    const container = await renderEntry('routing', ['platform.network.services.view'])
    expect(container.querySelector('[data-target]')).toBeNull()
    expect(container.textContent).toContain('没有可查看的网络资源')
  })
})
