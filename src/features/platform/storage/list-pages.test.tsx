/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, type ReactNode } from 'react'
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
vi.mock('@/components/management-list', () => ({
  ManagementDensityButton: () => null,
  ManagementIconButton: ({
    'aria-label': ariaLabel,
    onClick,
  }: {
    'aria-label': string
    onClick?: () => void
  }) => (
    <button aria-label={ariaLabel} onClick={onClick}>
      {ariaLabel}
    </button>
  ),
  ManagementKeywordField: () => null,
  ManagementQueryActions: () => null,
  ManagementRefreshButton: () => null,
  ManagementState: ({ description }: { description?: ReactNode }) => <div>{description}</div>,
  ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <>{children}</>,
}))
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

beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
beforeEach(() => {
  vi.clearAllMocks()
  testState.editorModuleLoads = 0
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
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>{page}</MemoryRouter>
      </QueryClientProvider>,
    )
  })
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  return container
}

describe('storage list pages', () => {
  it('uses selected namespace for PVC list and row namespace for delete', async () => {
    const container = await renderPage(<StoragePvcPage />)
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/storage/persistentvolumeclaims?namespace=selected-ns',
    )
    const deleteButton = container.querySelector('button[aria-label="删除"]')
    await act(async () => deleteButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await act(async () => Promise.resolve())
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
