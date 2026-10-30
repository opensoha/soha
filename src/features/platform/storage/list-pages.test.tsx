/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cloneElement, isValidElement, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { StoragePvcPage } from './persistent-volume-claims/list-page'
import { StoragePvPage } from './persistent-volumes/list-page'
import { StorageClassesPage } from './storage-classes/list-page'

const testState = vi.hoisted(() => ({
  editorModuleLoads: 0,
  scope: { clusterId: 'cluster-a', namespace: 'selected-ns' },
}))
const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path.includes('persistentvolumeclaims')) {
      return {
        data: [
          {
            name: 'claim-a',
            namespace: 'row-ns',
            status: 'Bound',
            ageSeconds: 60,
            allowedActions: ['delete'],
          },
        ],
      }
    }
    return { data: [] }
  }),
)
const apiDeleteMock = vi.hoisted(() => vi.fn(async () => ({ data: null })))

vi.mock('@/services/api-client', () => ({
  api: { delete: apiDeleteMock, get: apiGetMock, post: vi.fn(), put: vi.fn() },
}))
vi.mock('@/components/k8s-yaml-editor', () => {
  testState.editorModuleLoads += 1
  return { K8sYamlEditor: () => <div>editor</div> }
})
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))
vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))
vi.mock('@/features/auth', () => ({
  hasAllowedAction: (actions: string[] | undefined, action: string) =>
    actions?.includes(action) ?? false,
}))
vi.mock('@/components/status-tag', () => ({
  BooleanTag: ({ value }: { value: boolean }) => <span>{String(value)}</span>,
  StatusTag: ({ value }: { value: string }) => <span>{value}</span>,
}))
vi.mock('@/components/management-list', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/management-list')>()
  return {
    ManagementDensityButton: () => null,
    ManagementIconButton: actual.ManagementIconButton,
    ManagementKeywordField: () => null,
    ManagementQueryActions: () => null,
    ManagementRefreshButton: () => null,
    ManagementState: ({ description }: { description?: ReactNode }) => <div>{description}</div>,
    ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <>{children}</>,
  }
})
vi.mock('@/components/admin-table', () => ({
  AdminTable: (table: {
    columns: Array<{
      dataIndex?: string
      key?: string
      render?: (value: unknown, record: Record<string, unknown>) => ReactNode
    }>
    dataSource: Array<Record<string, unknown>>
    headerExtra?: ReactNode
  }) => (
    <div>
      {table.headerExtra}
      {table.dataSource.map((record, rowIndex) => (
        <div key={rowIndex}>
          {table.columns.map((column, columnIndex) => (
            <span key={column.key ?? `${column.dataIndex}-${columnIndex}`}>
              {column.render
                ? column.render(record[column.dataIndex ?? ''], record)
                : (record[column.dataIndex ?? ''] as ReactNode)}
            </span>
          ))}
        </div>
      ))}
    </div>
  ),
}))
vi.mock('@/components/management-data-page', () => ({
  ManagementDataPage: ({
    beforeQuery,
    tableNode,
  }: {
    beforeQuery?: ReactNode
    tableNode?: ReactNode
  }) => (
    <div>
      {beforeQuery}
      {tableNode}
    </div>
  ),
}))
vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  const message = { error: vi.fn(), success: vi.fn() }
  return {
    ...actual,
    App: { ...actual.App, useApp: () => ({ message }) },
    Popconfirm: ({ children, onConfirm }: { children?: ReactNode; onConfirm?: () => void }) => (
      <span onClick={onConfirm}>{children}</span>
    ),
    Tooltip: ({ children }: { children?: ReactNode }) => <>{children}</>,
    message,
  }
})

const mountedRoots: Root[] = []

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  window.matchMedia = vi.fn().mockReturnValue({
    matches: false,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
  })
  const computedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => computedStyle(element))
})
beforeEach(() => {
  vi.clearAllMocks()
  testState.editorModuleLoads = 0
  testState.scope.clusterId = 'cluster-a'
})
afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function renderPage(page: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  const rerender = async () => {
    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>{isValidElement(page) ? cloneElement(page) : page}</MemoryRouter>
        </QueryClientProvider>,
      )
    })
    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
  await rerender()
  return { container, rerender }
}

describe('storage list pages', () => {
  it('uses selected namespace for PVC list and row namespace for delete', async () => {
    const { container } = await renderPage(<StoragePvcPage />)
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/storage/persistentvolumeclaims?namespace=selected-ns',
    )
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="更多操作：claim-a"]')!.click()
    })
    await act(async () => {
      document.querySelector<HTMLElement>('[role="menuitem"]')!.click()
    })
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('row-ns / claim-a')
    expect(apiDeleteMock).not.toHaveBeenCalled()
    await act(async () => {
      document.querySelector<HTMLButtonElement>('.ant-modal-footer .ant-btn-primary')!.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(apiDeleteMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/storage/persistentvolumeclaims/claim-a?namespace=row-ns',
    )
    expect(testState.editorModuleLoads).toBe(0)
  })

  it('keeps PV and StorageClass lists cluster scoped', async () => {
    await renderPage(<StoragePvPage />)
    await renderPage(<StorageClassesPage />)
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/storage/persistentvolumes')
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/storage/storageclasses')
    expect(testState.editorModuleLoads).toBe(0)
  })
})

it('does not show a previous cluster delete as loading on an identically named PVC', async () => {
  let finish!: () => void
  apiDeleteMock.mockImplementationOnce(
    () =>
      new Promise<{ data: null }>((resolve) => {
        finish = () => resolve({ data: null })
      }),
  )
  const { container, rerender } = await renderPage(<StoragePvcPage />)
  await act(async () =>
    container.querySelector<HTMLButtonElement>('[aria-label="更多操作：claim-a"]')!.click(),
  )
  await act(async () => document.querySelector<HTMLElement>('[role="menuitem"]')!.click())
  await act(async () =>
    document.querySelector<HTMLButtonElement>('.ant-modal-footer .ant-btn-primary')!.click(),
  )
  testState.scope.clusterId = 'cluster-b'
  await rerender()
  expect(apiGetMock).toHaveBeenCalledWith(
    '/clusters/cluster-b/storage/persistentvolumeclaims?namespace=selected-ns',
  )
  await act(async () =>
    container.querySelector<HTMLButtonElement>('[aria-label="更多操作：claim-a"]')!.click(),
  )
  const remove = document.querySelector('[role="menuitem"]')!
  expect(remove.getAttribute('aria-disabled')).toBe('true')
  expect(remove.querySelector('.anticon-loading')).toBeNull()
  await act(async () => {
    finish()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  expect(apiDeleteMock).toHaveBeenCalledTimes(1)
  expect(apiDeleteMock).toHaveBeenCalledWith(
    '/clusters/cluster-a/storage/persistentvolumeclaims/claim-a?namespace=row-ns',
  )
})
