// @vitest-environment jsdom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { ApplicationEntryReads } from './entry-reads'

const state = vi.hoisted(() => ({
  permissions: [] as string[],
  app: vi.fn(),
  environments: vi.fn(),
  services: vi.fn(),
  runtime: vi.fn(),
  pods: vi.fn(),
}))
vi.mock('@/features/auth/permission-snapshot', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth/permission-snapshot')>()),
  usePermissionSnapshot: () => ({ data: { data: { permissionKeys: state.permissions } } }),
}))
vi.mock('../api', () => ({
  deliveryApi: {
    applications: {
      get: state.app,
      environmentNavigation: state.environments,
      serviceDirectory: state.services,
      environmentRuntimeSummary: state.runtime,
    },
    workloads: { servicePods: state.pods },
  },
}))

it.each([
  { appAccess: true, serviceAccess: true, applicationId: 'demo' },
  { appAccess: true, serviceAccess: false, applicationId: 'demo' },
  { appAccess: true, serviceAccess: true, applicationId: ' ' },
  { appAccess: false, serviceAccess: false, applicationId: 'demo' },
])(
  'warms only authorized directory reads ($appAccess, $serviceAccess, "$applicationId")',
  async ({ appAccess, serviceAccess, applicationId }) => {
    const root = createRoot(document.createElement('div'))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    state.permissions = [
      ...(appAccess ? ['delivery.applications.view'] : []),
      ...(serviceAccess ? ['delivery.application-services.view'] : []),
    ]
    state.app.mockReset().mockResolvedValue({ id: 'demo' })
    state.environments
      .mockReset()
      .mockResolvedValue([
        { applicationEnvironmentId: 'first' },
        { applicationEnvironmentId: 'target' },
      ])
    state.services.mockReset().mockResolvedValue([])
    state.runtime.mockReset().mockResolvedValue({
      environment: { applicationEnvironmentId: 'first' },
      observationStatus: 'available',
    })
    try {
      await act(async () => {
        root.render(
          <QueryClientProvider client={client}>
            <MemoryRouter>
              <ApplicationEntryReads applicationId={applicationId} />
            </MemoryRouter>
          </QueryClientProvider>,
        )
      })
      const appReads = Boolean(appAccess && applicationId.trim())
      const serviceReads = appReads && serviceAccess
      expect(state.app).toHaveBeenCalledTimes(appReads ? 1 : 0)
      expect(state.environments).toHaveBeenCalledTimes(appReads ? 1 : 0)
      expect(state.services).toHaveBeenCalledTimes(serviceReads ? 1 : 0)
      if (appReads) {
        await vi.waitFor(() =>
          expect(state.runtime).toHaveBeenCalledWith('demo', 'first', expect.any(AbortSignal)),
        )
      }
      expect(state.runtime).toHaveBeenCalledTimes(appReads ? 1 : 0)
      if (serviceReads) expect(state.services).toHaveBeenCalledWith('demo', expect.any(AbortSignal))
    } finally {
      await act(async () => root.unmount())
      client.clear()
    }
  },
)

it('observes the authorized URL environment once when a later observer mounts after failure', async () => {
  const root = createRoot(document.createElement('div'))
  const client = new QueryClient()
  state.permissions = ['delivery.applications.view', 'delivery.application-services.view']
  state.app.mockReset().mockResolvedValue({ id: 'demo' })
  state.environments
    .mockReset()
    .mockResolvedValue([
      { applicationEnvironmentId: 'first' },
      { applicationEnvironmentId: 'target' },
    ])
  state.services.mockReset().mockResolvedValue([])
  state.runtime.mockReset().mockRejectedValue(new Error('cluster unavailable'))
  const render = (shown: boolean) => (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/applications/demo?applicationEnvironmentId=target']}>
        <ApplicationEntryReads applicationId="demo" />
        {shown && <ApplicationEntryReads applicationId="demo" />}
      </MemoryRouter>
    </QueryClientProvider>
  )
  try {
    await act(async () => root.render(render(false)))
    await vi.waitFor(() =>
      expect(state.runtime).toHaveBeenCalledWith('demo', 'target', expect.any(AbortSignal)),
    )
    await act(async () => root.render(render(true)))
    expect(state.runtime).toHaveBeenCalledTimes(1)
    expect(state.pods).not.toHaveBeenCalled()
  } finally {
    await act(async () => root.unmount())
    client.clear()
  }
})
