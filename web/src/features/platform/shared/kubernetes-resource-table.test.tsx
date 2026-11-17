/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { AdminTableProps } from '@/components/admin-table'
import { KubernetesResourceTable } from './kubernetes-resource-table'

const state = vi.hoisted(() => ({
  user: { userId: 'user-a' },
  clusterId: 'cluster-a',
  namespace: 'monitoring' as string | null,
}))
let table: AdminTableProps
vi.mock('@/components/admin-table', () => ({
  AdminTable: (props: AdminTableProps) => {
    table = props
    return <div>{props.title}</div>
  },
}))
vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (select: (value: typeof state) => unknown) => select(state),
}))
vi.mock('@/stores/platform-scope-store', () => ({ usePlatformScopeStore: () => state }))
vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'zh_CN' }) }))
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
const rows = [
  { name: 'api', namespace: 'monitoring' },
  { name: 'worker', namespace: 'monitoring' },
]
const columns = [
  { title: '名称', dataIndex: 'name' },
  { title: '命名空间', dataIndex: 'namespace' },
]
async function render(overrides: Partial<Parameters<typeof KubernetesResourceTable>[0]> = {}) {
  await act(async () =>
    root.render(
      <KubernetesResourceTable
        resourceKey="deployments"
        resourceLabel="Deployments"
        columns={columns}
        allRows={rows}
        dataSource={rows}
        rowKey="name"
        {...overrides}
      />,
    ),
  )
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  state.user.userId = 'user-a'
  state.namespace = 'monitoring'
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.unstubAllGlobals()
})
it('owns continuous browsing, full-result count, visible namespace and per-resource user preferences', async () => {
  await render({ dataSource: rows.slice(0, 1), rowSelection: { selectedRowKeys: [] } })
  expect(container.querySelector('.soha-kubernetes-resource-count')?.getAttribute('role')).toBe(
    'status',
  )
  expect(table.pagination).toBe(false)
  expect(table.virtual).toBe(true)
  expect(table.viewportScroll).toBe(true)
  expect(container.textContent).toBe('匹配 1 / 共 2 个 Deployments')
  expect(table.defaultHiddenColumnKeys).toEqual([])
  expect(table.columnLayoutKey).toBe('deployments:user-a:zh_CN')
  expect(table.rowSelection.getTitleCheckboxProps()['aria-label']).toBe(
    '选择全部匹配的 1 个 Deployments',
  )
  state.user.userId = 'user-b'
  await render({ resourceKey: 'jobs' })
  expect(table.columnLayoutKey).toBe('jobs:user-b:zh_CN')
  state.namespace = null
  await render()
  expect(table.defaultHiddenColumnKeys).toEqual([])
})
it('keeps initial widths on refresh and refits explicitly from the full scope', async () => {
  state.namespace = null
  await render()
  const initialName = table.columns[0].width
  const initial = table.columns[1].width
  const changed = rows.map((row) => ({
    name: `${row.name}-${'a'.repeat(50)}`,
    namespace: 'a'.repeat(50),
  }))
  await render({ allRows: changed, dataSource: changed })
  expect(table.columns[1].width).toBe(initial)
  expect(table.columns[0].width).toBe(initialName)
  await act(async () => table.onFitColumns?.())
  expect(table.columns[1].width).toBeGreaterThan(initial as number)
  expect(table.columns[0].width).toBeGreaterThan(initialName as number)
  await act(async () => table.onResetColumns?.())
  expect(table.columns[0].width).toBe(initialName)
})
it('reuses active columns without scanning content on refresh and measures on explicit fit', async () => {
  const readNamespace = vi.fn(() => 'monitoring')
  const records = [
    {
      name: 'api',
      get namespace() {
        return readNamespace()
      },
    },
  ]
  await render({ allRows: records, dataSource: records })
  const initial = table.columns
  expect(readNamespace).toHaveBeenCalled()
  readNamespace.mockClear()
  const refreshed = records.slice()
  await render({ allRows: refreshed, dataSource: refreshed })
  expect(table.columns).toBe(initial)
  expect(readNamespace).not.toHaveBeenCalled()
  await act(async () => table.onFitColumns?.())
  expect(readNamespace).toHaveBeenCalled()
})
it('measures the first readable result and recomputes widths for a new scope', async () => {
  await render({ allRows: [], dataSource: [] })
  await render()
  const initial = table.columns[1].width
  state.namespace = 'another-scope'
  const changed = rows.map((row) => ({ ...row, namespace: 'a'.repeat(50) }))
  await render({ allRows: changed, dataSource: changed })
  expect(table.columns[1].width).toBeGreaterThan(initial)
})
it('keeps namespace visible by default and supports an explicit scoped fold', async () => {
  await render()
  expect(table.defaultHiddenColumnKeys).toEqual([])
  expect(table.columnLayoutKey).toBe('deployments:user-a:zh_CN')
  await render({ hideNamespaceWhenScoped: true })
  expect(table.defaultHiddenColumnKeys).toEqual(['namespace'])
})
it('preserves row operations and reports failed reads instead of an empty successful list', async () => {
  const onChange = vi.fn()
  const onRow = vi.fn()
  await render({ onChange, onRow, error: new Error('unavailable'), allRows: [], dataSource: [] })
  expect(container.textContent).toBe('Deployments 未加载')
  expect(table.onRow).toBe(onRow)
  table.onChange?.({} as never, {}, {} as never, { action: 'sort' } as never)
  expect(onChange).toHaveBeenCalledOnce()
})

it('releases wide fixed action groups on narrow screens while keeping compact menus fixed', async () => {
  const action = { key: 'actions', title: '', fixed: 'right' as const, width: 152 }
  await render({ columns: [columns[0], action] })
  expect(table.columns[1].fixed).toBe(false)
  await render({ resourceKey: 'pods', columns: [columns[0], { ...action, width: 64 }] })
  expect(table.columns[1].fixed).toBe('right')
})

it('composes selection actions beside the shared truthful count without replacing it', async () => {
  await render({ selectionToolbar: <button>批量操作</button> })
  expect(container.textContent).toBe('2 个 Deployments批量操作')
})

it('keeps table callbacks and selection stable on toolbar refresh while forwarding updated behavior', async () => {
  const onScroll = vi.fn()
  const initialChange = vi.fn()
  const updatedChange = vi.fn()
  const selection = { selectedRowKeys: ['api'] }
  await render({
    headerExtra: 'before',
    onScroll,
    onChange: initialChange,
    rowSelection: selection,
  })
  const before = { ...table }
  await render({
    headerExtra: 'refreshing',
    onScroll,
    onChange: initialChange,
    rowSelection: selection,
  })
  expect(table.columns).toBe(before.columns)
  expect(table.onScroll).toBe(before.onScroll)
  expect(table.onChange).toBe(before.onChange)
  expect(table.defaultHiddenColumnKeys).toBe(before.defaultHiddenColumnKeys)
  expect(table.rowSelection).toBe(before.rowSelection)
  await render({ onScroll, onChange: updatedChange, rowSelection: { selectedRowKeys: [] } })
  expect(table.onChange).not.toBe(before.onChange)
  expect(table.rowSelection).not.toBe(before.rowSelection)
  table.onChange?.({}, {}, {}, { action: 'sort', currentDataSource: rows })
  expect(updatedChange).toHaveBeenCalledOnce()
  expect(initialChange).not.toHaveBeenCalled()
})
