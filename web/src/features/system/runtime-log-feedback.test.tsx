/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { ConfigProvider } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { systemApi } from './api'
import { AuditLogsPage } from './audit/page'
import { OperationLogsPage } from './operation-logs/page'

let client: QueryClient, root: ReturnType<typeof createRoot>, container: HTMLDivElement
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi.fn().mockReturnValue({
    matches: false,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
  })
  const computed = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => computed(element))
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
}
function Destination({ audit }: { audit: boolean }) {
  const navigate = useNavigate()
  return (
    <>
      <button
        onClick={() => navigate('?metadataKey=runtime.release_bundle.id&metadataValue=bundle-next')}
      >
        另一运行对象
      </button>
      {audit ? <AuditLogsPage /> : <OperationLogsPage />}
    </>
  )
}
it.each([true, false])(
  'retains a failed read, hides denied data and its open drawer, and follows runtime URL filters (audit=%s)',
  async (audit) => {
    const record = {
      id: 'log-1',
      actorId: 'tester',
      actorName: 'tester',
      action: 'delivery.build',
      operationType: 'delivery.build',
      targetScope: {},
      resourceKind: 'build',
      resourceName: 'build-first',
      result: 'success',
      summary: 'protected build evidence',
      createdAt: '2026-10-16T04:00:00Z',
    }
    const read = audit
      ? vi.spyOn(systemApi.audit, 'list').mockResolvedValue([record])
      : vi.spyOn(systemApi.operationLogs, 'list').mockResolvedValue([record])
    await act(async () => {
      root.render(
        <ConfigProvider theme={{ token: { motion: false } }}>
          <QueryClientProvider client={client}>
            <MemoryRouter
              initialEntries={[
                '/system/' +
                  (audit ? 'audit' : 'operation-logs') +
                  '?metadataKey=runtime.build.id&metadataValue=build-first',
              ]}
            >
              <Destination audit={audit} />
            </MemoryRouter>
          </QueryClientProvider>
        </ConfigProvider>,
      )
    })
    await settle()
    expect(document.body.textContent).toContain('protected build evidence')
    const row = container.querySelector<HTMLElement>('tr[data-row-key="log-1"]')!
    await act(async () => row.click())
    await settle()
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    read.mockRejectedValueOnce(
      new ApiError(503, 'logs temporarily unavailable', {
        method: 'GET',
        requestId: 'req-logs-503',
      }),
    )
    await act(async () => {
      await client.refetchQueries()
    })
    await settle()
    expect(document.body.textContent).toContain('上次读取结果')
    expect(document.body.textContent).toContain('req-logs-503')
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      'protected build evidence',
    )
    read.mockRejectedValueOnce(
      new ApiError(403, 'log access denied', { method: 'GET', requestId: 'req-logs-403' }),
    )
    await act(async () => {
      await client.refetchQueries()
    })
    await settle()
    expect(document.body.textContent).not.toContain('protected build evidence')
    expect(document.body.textContent).toContain('req-logs-403')
    await act(async () => container.querySelector('button')!.click())
    await settle()
    expect(
      read.mock.calls.some((call) => {
        const filters = (audit ? call[1] : call[0]) as {
          metadataKey?: string
          metadataValue?: string
        }
        return (
          filters.metadataKey === 'runtime.release_bundle.id' &&
          filters.metadataValue === 'bundle-next'
        )
      }),
    ).toBe(true)
  },
)
