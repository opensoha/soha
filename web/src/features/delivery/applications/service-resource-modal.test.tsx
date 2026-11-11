/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ServiceResourceModal } from './service-resource-modal'

const state = vi.hoisted(() => ({ denied: '', get: vi.fn() }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: (_: unknown, permission: string) => permission !== state.denied,
}))
vi.mock('@/services/api-client', () => ({ api: { get: state.get } }))
let root: ReturnType<typeof createRoot>
beforeEach(() => {
  state.denied = ''
  state.get.mockReset().mockImplementation(async (path: string) => {
    if (path.endsWith('/runtime'))
      return {
        data: {
          workload: { namespace: 'team-a' },
          deployment: {
            relatedResources: [
              { kind: 'ConfigMap', name: 'settings', namespace: 'team-a' },
              { kind: 'Secret', name: 'credentials', namespace: 'team-b' },
            ],
          },
        },
      }
    if (path.includes('/yaml?'))
      return {
        data: {
          content: path.includes('/secrets/')
            ? 'kind: Secret\nmetadata:\n  name: credentials'
            : 'kind: ConfigMap\nmetadata:\n  name: settings',
        },
      }
    if (path.startsWith('/delivery/manifest-packages?'))
      return {
        data: {
          items: [
            {
              id: 'manifest-1',
              name: 'Service manifests',
              files: [
                { path: 'service.yaml', content: 'kind: Service' },
                { path: 'deployment.yaml', content: 'kind: Deployment' },
              ],
            },
          ],
          total: 1,
        },
      }
    throw new Error(path)
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
    }),
  })
})
afterEach(async () => {
  if (root) await act(async () => root.unmount())
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})
async function render(mode: 'resources' | 'related-resources') {
  const host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <ServiceResourceModal
          applicationId="app-1"
          serviceId="svc-1"
          mode={mode}
          workloads={[
            {
              applicationEnvironmentId: 'env-1',
              clusterId: 'cluster-a',
              namespace: 'team-a',
              workloadKind: 'Deployment',
              workloadName: 'web',
              desiredReplicas: 1,
              readyReplicas: 1,
              updatedReplicas: 1,
              availableReplicas: 1,
            },
          ]}
          onClose={() => {}}
        />
      </QueryClientProvider>,
    ),
  )
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}
it('switches related YAML with the exact resource namespace', async () => {
  await render('related-resources')
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
  expect(document.querySelector('[aria-label="资源 YAML"]')?.textContent).toContain(
    'kind: ConfigMap',
  )
  await act(async () =>
    Array.from(document.querySelectorAll<HTMLButtonElement>('[aria-label="关联资源列表"] button'))
      .find((button) => button.textContent?.includes('credentials'))!
      .click(),
  )
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  expect(state.get).toHaveBeenCalledWith(
    '/clusters/cluster-a/configuration/secrets/credentials/yaml?namespace=team-b',
  )
  expect(document.querySelector('[aria-label="资源 YAML"]')?.textContent).toContain('kind: Secret')
})
it('does not fetch YAML without resource permission', async () => {
  state.denied = 'platform.configuration.config-maps.view'
  await render('related-resources')
  expect(state.get.mock.calls.some(([path]) => path.includes('/yaml'))).toBe(false)
  expect(document.body.textContent).toContain('权限')
})
it('shows service-scoped manifest files in the YAML pane', async () => {
  await render('resources')
  expect(state.get).toHaveBeenCalledWith(
    '/delivery/manifest-packages?applicationId=app-1&serviceId=svc-1&page=1&pageSize=20',
  )
  expect(document.querySelector('[aria-label="清单 YAML"]')?.textContent).toBe('kind: Service')
  await act(async () =>
    Array.from(document.querySelectorAll<HTMLButtonElement>('[aria-label="清单文件列表"] button'))
      .find((button) => button.textContent?.includes('deployment.yaml'))!
      .click(),
  )
  expect(document.querySelector('[aria-label="清单 YAML"]')?.textContent).toBe('kind: Deployment')
})
it('does not load resources without application access', async () => {
  state.denied = 'delivery.applications.view'
  await render('resources')
  expect(state.get).not.toHaveBeenCalled()
})
