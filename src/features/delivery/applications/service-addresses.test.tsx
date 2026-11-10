/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ServiceAddresses } from './service-addresses'
import type { ApplicationRuntimeWorkload } from '../types'

const state = vi.hoisted(() => ({
  gatewayAllowed: true,
  grpcAllowed: true,
  gatewayAssigned: true,
  failure: false,
  get: vi.fn(async (path: string) => {
    if (path.endsWith('/workloads/api/runtime')) {
      if (state.failure) throw new Error('runtime unavailable')
      return {
        data: {
          deployment: {
            relatedResources: [
              { kind: 'Service', name: 'api', namespace: 'app' },
              { kind: 'Ingress', name: 'public', namespace: 'app' },
              { kind: 'HTTPRoute', name: 'route', namespace: 'app' },
              { kind: 'GRPCRoute', name: 'grpc', namespace: 'app' },
            ],
          },
          services: [
            { name: 'api', namespace: 'app', clusterIp: '10.43.0.8', ports: ['8080/TCP'] },
            { name: 'unrelated', namespace: 'app', clusterIp: '10.43.0.99', ports: [] },
          ],
          ingresses: [
            {
              name: 'public',
              namespace: 'app',
              hosts: ['api.example.test'],
              address: '192.0.2.10',
            },
          ],
        },
      }
    }
    if (
      path === '/clusters/c/network/httproutes/route/detail?namespace=app' ||
      path === '/clusters/c/network/grpcroutes/grpc/detail?namespace=app'
    ) {
      return {
        data: {
          name: 'route',
          namespace: 'app',
          parentRefs: ['edge/shared'],
          hostnames: ['unverified-route.example.test'],
        },
      }
    }
    if (path === '/clusters/c/network/gateways/shared/detail?namespace=edge')
      return {
        data: {
          name: 'shared',
          namespace: 'edge',
          addresses: state.gatewayAssigned ? ['192.0.2.20'] : [],
        },
      }
    throw new Error(`Unexpected request: ${path}`)
  }),
}))
vi.mock('@/services/api-client', () => ({ api: { get: (path: string) => state.get(path) } }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: (_: unknown, permission: string) =>
    (permission !== 'platform.network.gateways.view' || state.gatewayAllowed) &&
    (permission !== 'platform.network.grpc-routes.view' || state.grpcAllowed),
}))

const workload = {
  applicationEnvironmentId: 'env',
  clusterId: 'c',
  namespace: 'app',
  workloadName: 'api',
  workloadKind: 'Deployment',
  desiredReplicas: 1,
  readyReplicas: 1,
  updatedReplicas: 1,
  availableReplicas: 1,
} satisfies ApplicationRuntimeWorkload
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement

beforeEach(() => {
  state.gatewayAllowed = true
  state.grpcAllowed = true
  state.gatewayAssigned = true
  state.failure = false
  state.get.mockClear()
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
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })) as unknown as typeof window.matchMedia
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
})
async function renderAddresses(value: ApplicationRuntimeWorkload = workload) {
  await act(async () =>
    root.render(
      <App>
        <QueryClientProvider
          client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
        >
          <ServiceAddresses applicationId="application" workload={value} />
        </QueryClientProvider>
      </App>,
    ),
  )
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
}

describe('service addresses', () => {
  it('does not use the Deployment endpoint for a custom resource or replaced source', async () => {
    await renderAddresses({ ...workload, workloadKind: 'CustomResource', sourceStatus: 'linked' })
    expect(state.get).not.toHaveBeenCalled()
    expect(container.textContent).not.toContain('10.43.0.8')
    await renderAddresses({ ...workload, sourceStatus: 'replaced' })
    expect(state.get).not.toHaveBeenCalled()
  })
  it('uses verified workload relations and resolves a shared cross-namespace Gateway once', async () => {
    await renderAddresses()
    expect(container.textContent).toContain('10.43.0.8')
    expect(container.textContent).toContain('api.example.test')
    expect(container.textContent).toContain('192.0.2.10')
    expect(container.textContent).toContain('192.0.2.20')
    expect(container.textContent).not.toContain('10.43.0.99')
    expect(
      state.get.mock.calls.filter(([path]) => path.includes('/gateways/shared/')),
    ).toHaveLength(1)
  })
  it('shows authorized route addresses when another route kind is denied', async () => {
    state.grpcAllowed = false
    await renderAddresses()
    expect(container.textContent).toContain('192.0.2.20')
    expect(container.textContent).toContain('部分路由无查看权限')
    expect(state.get.mock.calls.some(([path]) => path.includes('/grpcroutes/'))).toBe(false)
  })
  it('does not manufacture a Gateway address from a Route hostname', async () => {
    state.gatewayAssigned = false
    await renderAddresses()
    expect(container.textContent).toContain('未分配地址')
    expect(container.textContent).not.toContain('unverified-route.example.test')
  })
  it('does not query Gateway resources without permission', async () => {
    state.gatewayAllowed = false
    await renderAddresses()
    expect(container.textContent).toContain('无查看权限')
    expect(container.textContent).toContain('10.43.0.8')
    expect(state.get.mock.calls.some(([path]) => path.includes('/network/'))).toBe(false)
  })
  it('distinguishes a failed request from missing addresses', async () => {
    state.failure = true
    await renderAddresses()
    expect(container.textContent).toContain('地址读取失败')
    expect(container.textContent).toContain('重试地址')
    expect(container.textContent).not.toContain('未关联')
  })
})
