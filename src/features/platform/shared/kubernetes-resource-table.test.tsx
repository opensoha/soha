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
it('owns continuous browsing, full-result count, namespace folding and per-resource user preferences', async () => {
  await render({ dataSource: rows.slice(0, 1), rowSelection: { selectedRowKeys: [] } })
  expect(container.querySelector('.soha-kubernetes-resource-count')?.getAttribute('role')).toBe(
    'status',
  )
  expect(table.pagination).toBe(false)
  expect(table.virtual).toBe(true)
  expect(table.viewportScroll).toBe(true)
  expect(container.textContent).toBe('匹配 1 / 共 2 个 Deployments')
  expect(table.defaultHiddenColumnKeys).toEqual(['namespace'])
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
