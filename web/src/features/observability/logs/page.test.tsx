/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { LogsPage } from './page'

const aiMocks = vi.hoisted(() => ({ useAIPageContext: vi.fn() }))
const scopeMocks = vi.hoisted(() => ({
  logExplorer: vi.fn(),
  setClusterId: vi.fn(),
  setNamespace: vi.fn(),
}))

vi.mock('@/features/copilot', () => aiMocks)
vi.mock('@/components/platform-scope-toolbar', () => ({ PlatformScopeToolbar: () => null }))
vi.mock('./log-explorer', () => ({
  LogExplorer: (props: unknown) => {
    scopeMocks.logExplorer(props)
    return null
  },
}))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => ({
    clusterId: 'fallback-cluster',
    namespace: 'fallback-namespace',
    setClusterId: scopeMocks.setClusterId,
    setNamespace: scopeMocks.setNamespace,
  }),
}))

beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))

afterEach(() => vi.clearAllMocks())

describe('LogsPage AI context', () => {
  it('keeps a deep-linked scope local instead of writing it back to the shared toolbar store', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <MemoryRouter
          initialEntries={[
            '/monitoring-workbench/logs?cluster=linked-cluster&namespace=linked-namespace',
          ]}
        >
          <LogsPage />
        </MemoryRouter>,
      )
    })

    expect(scopeMocks.setClusterId).not.toHaveBeenCalled()
    expect(scopeMocks.setNamespace).not.toHaveBeenCalled()
    expect(scopeMocks.logExplorer.mock.calls.slice(-1)[0]?.[0]).toEqual(
      expect.objectContaining({
        clusterId: 'linked-cluster',
        namespace: 'linked-namespace',
      }),
    )

    await act(async () => root.unmount())
    container.remove()
  })

  it('registers normalized delivery investigation context without widening the contract', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <MemoryRouter
          initialEntries={[
            '/monitoring-workbench/logs?source=delivery&cluster=cluster-a&namespace=apps&application=shop&environment=prod&workload=checkout&traceId=trace-1&spanId=span-1&from=2026-08-30T00%3A00%3A00Z&to=2026-08-30T00%3A15%3A00Z',
          ]}
        >
          <LogsPage />
        </MemoryRouter>,
      )
    })

    const context = aiMocks.useAIPageContext.mock.calls.slice(-1)[0]?.[0]
    expect(context).toEqual(
      expect.objectContaining({
        applicationId: 'shop',
        clusterId: 'cluster-a',
        entityKind: 'monitoring.logs.delivery',
        namespace: 'apps',
        sourceWorkbench: 'monitoring',
        visibleFilters: expect.objectContaining({
          environmentId: 'prod',
          from: '2026-08-30T00:00:00.000Z',
          spanId: 'span-1',
          to: '2026-08-30T00:15:00.000Z',
          traceId: 'trace-1',
        }),
        workload: 'checkout',
      }),
    )
    expect(context).not.toHaveProperty('environmentId')
    expect(context).not.toHaveProperty('sourceRoute')

    await act(async () => root.unmount())
    container.remove()
  })
})
