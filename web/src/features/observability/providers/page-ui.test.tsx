/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { ObservabilityProvidersPage } from './page'

const queryMocks = vi.hoisted(() => ({ useQuery: vi.fn() }))

vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useQuery: queryMocks.useQuery,
}))
vi.mock('@/components/status-tag', () => ({
  MetadataTag: ({ label }: { label: React.ReactNode }) => <span>{label}</span>,
  StatusTag: ({ label, value }: { label?: React.ReactNode; value?: string }) => (
    <span>{label ?? value}</span>
  ),
}))
vi.mock('@/utils/time', () => ({ formatDateTime: (value: string) => `time:${value}` }))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ columns, dataSource, title }: any) => (
    <div>
      <h1>{title}</h1>
      {columns.map((column: any) => (
        <div key={column.key}>
          <strong>{column.title}</strong>
          {column.render?.(dataSource[0]?.[column.dataIndex], dataSource[0], 0)}
        </div>
      ))}
    </div>
  ),
}))

beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))

describe('ObservabilityProvidersPage', () => {
  it('shows runtime validation evidence and the unavailable OTel stage boundary', async () => {
    queryMocks.useQuery.mockReturnValue({
      data: [
        {
          builtIn: true,
          capabilities: [],
          configured: true,
          displayName: 'Prometheus',
          lastValidatedAt: '2026-08-30T00:15:00Z',
          protocolVersion: 'v1',
          providerKey: 'prometheus',
          runtimeMode: 'builtin',
          runtimeStatus: 'failed',
          runtimeStatusReason: 'query smoke failed',
          signals: ['metrics'],
          status: 'supported',
        },
      ],
      isError: false,
      isLoading: false,
    })
    const container = document.createElement('div')
    const root = createRoot(container)

    await act(async () => root.render(<ObservabilityProvidersPage />))

    expect(container.textContent).toContain('运行验证')
    expect(container.textContent).toContain('failed')
    expect(container.textContent).toContain('query smoke failed')
    expect(container.textContent).toContain('time:2026-08-30T00:15:00Z')
    expect(container.textContent).toContain(
      'instrumentation、receiver、processor、exporter、freshness',
    )

    await act(async () => root.unmount())
  })
})
