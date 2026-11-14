/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { EventsPage } from './page'

const apiMocks = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

vi.mock('@/components/status-tag', () => ({
  StatusTag: ({ value }: { value?: string }) => <span>{value || '-'}</span>,
}))

interface MockColumn {
  dataIndex?: string
  key?: string
  render?: (value: unknown, record: Record<string, unknown>) => ReactNode
}

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    pageSize,
    title,
  }: {
    columns: MockColumn[]
    dataSource: Array<Record<string, unknown>>
    pageSize?: number
    title?: ReactNode
  }) => (
    <div data-page-size={pageSize}>
      <h1>{title}</h1>
      {dataSource.map((record) => (
        <div data-testid={`row-${String(record.id)}`} key={String(record.id)}>
          {columns.map((column, index) => {
            const value = record[column.dataIndex ?? '']
            return (
              <span key={column.key ?? `${String(column.dataIndex)}-${index}`}>
                {column.render ? column.render(value, record) : String(value ?? '')}
              </span>
            )
          })}
        </div>
      ))}
    </div>
  ),
}))

const roots: Root[] = []
const containers: HTMLElement[] = []

beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const container of containers.splice(0)) container.remove()
  vi.clearAllMocks()
})

async function renderPage() {
  apiMocks.get.mockResolvedValueOnce({
    data: [
      {
        id: 'event-1',
        source: 'alertmanager',
        category: 'alert',
        severity: 'warning',
        clusterId: 'cluster-a',
        namespace: 'default',
        summary: 'CPU pressure detected',
        payload: { alertId: 'alert-1' },
      },
    ],
  })
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <EventsPage />
      </QueryClientProvider>,
    )
  })
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (queryClient.isFetching() === 0) break
  }
  return container
}

describe('EventsPage', () => {
  it('renders event stream envelopes without changing the request or table behavior', async () => {
    const container = await renderPage()

    expect(apiMocks.get).toHaveBeenCalledWith('/events')
    expect(container.textContent).toContain('事件流')
    expect(container.textContent).toContain('alertmanager')
    expect(container.textContent).toContain('warning')
    expect(container.textContent).toContain('cluster-a / default')
    expect(container.textContent).toContain('CPU pressure detected')
    expect(container.textContent).toContain('{"alertId":"alert-1"}')
    expect(container.querySelector('[data-page-size]')?.getAttribute('data-page-size')).toBe('50')
  })
})
