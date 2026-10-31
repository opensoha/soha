import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { CronJobDetailPage } from './detail-page'

const testDetail = vi.hoisted(() => ({
  name: 'nightly-report',
  namespace: 'operations',
  schedule: '0 2 * * *',
  suspend: false,
  activeJobs: 0,
  jobs: [
    {
      name: 'nightly-report-123',
      namespace: 'operations',
      succeeded: 1,
      failed: 0,
      active: 0,
      ageSeconds: 60,
    },
  ],
}))

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({
    localeCode: 'en_US' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => ({ clusterId: 'cluster-a' }),
}))
vi.mock('@/features/platform/workloads/shared/workload-relations', () => ({
  WorkloadRelationsCard: () => null,
  WorkloadPodsCard: () => null,
}))
vi.mock('@/features/auth', async (original) => ({
  ...(await original<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: ['platform.workloads.jobs.view'] } },
  }),
}))

vi.mock('@/features/platform/workloads/shared/detail-shell', () => ({
  WorkloadDetailShell: ({
    extraOverview,
  }: {
    extraOverview?: ReactNode | ((detail: unknown) => ReactNode)
  }) => <>{typeof extraOverview === 'function' ? extraOverview(testDetail) : extraOverview}</>,
}))

describe('CronJobDetailPage', () => {
  beforeAll(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
    window.matchMedia = () =>
      ({
        addEventListener: () => undefined,
        addListener: () => undefined,
        dispatchEvent: () => false,
        matches: false,
        media: '',
        onchange: null,
        removeEventListener: () => undefined,
        removeListener: () => undefined,
      }) as MediaQueryList
  })
  afterEach(() => document.body.replaceChildren())

  it('renders related jobs and their lifecycle counts', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <QueryClientProvider
          client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
        >
          <MemoryRouter>
            <CronJobDetailPage />
          </MemoryRouter>
        </QueryClientProvider>,
      )
    })

    expect(container.querySelector('.ant-pagination')).toBeNull()
    expect(container.querySelector('a')?.getAttribute('href')).toContain(
      '/workloads/jobs/nightly-report-123?',
    )
    expect(container.textContent).toContain('nightly-report-123')
    expect(container.textContent).toContain('Succeeded')
    expect(container.textContent).toContain('Failed')
    expect(container.textContent).toContain('Active')

    await act(async () => root.unmount())
  })
})
