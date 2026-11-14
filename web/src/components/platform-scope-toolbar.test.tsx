/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import { namespaceQueries } from '@/features/platform/cluster-resources/queries'
import { toClusterScope } from '@/features/platform/cluster-resources/scope'
import { clusterQueries } from '@/features/platform/clusters/queries'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { PlatformScopeTrigger } from './platform-scope-toolbar'

vi.mock('@/i18n', () => ({ useI18n: () => ({ t: (_key: string, fallback: string) => fallback }) }))

it('validates the selected namespace against the shared resource cache after namespace changes', async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
  }))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  const namespaceKey = namespaceQueries.list(toClusterScope('cluster-a')).queryKey
  client.setQueryData(clusterQueries.list().queryKey, [
    {
      id: 'cluster-a',
      name: 'Cluster A',
      region: 'local',
      environment: 'test',
      labels: {},
      connectionMode: 'direct_kubeconfig',
      version: 'v1',
      health: { status: 'healthy' },
    },
  ])
  client.setQueryData(['namespaces', 'cluster-a'], { data: [{ name: 'old' }] })
  const oldNamespace = { name: 'old', status: 'Active', labels: {} }
  const newNamespace = { name: 'new', status: 'Active', labels: {} }
  client.setQueryData(namespaceKey, [oldNamespace])
  usePlatformScopeStore.setState({ clusterId: 'cluster-a', namespace: null })
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  try {
    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          <MemoryRouter>
            <PlatformScopeTrigger scopeMode="cluster" />
          </MemoryRouter>
        </QueryClientProvider>,
      )
    })
    await act(async () => {
      client.setQueryData(namespaceKey, [oldNamespace, newNamespace])
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await act(async () => usePlatformScopeStore.getState().setNamespace('new'))
    expect(usePlatformScopeStore.getState().namespace).toBe('new')

    await act(async () => {
      client.setQueryData(namespaceKey, [oldNamespace])
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await vi.waitFor(() => expect(usePlatformScopeStore.getState().namespace).toBeNull())
  } finally {
    await act(async () => root.unmount())
    client.clear()
    container.remove()
    usePlatformScopeStore.setState({ clusterId: null, namespace: null })
    vi.unstubAllGlobals()
  }
})
