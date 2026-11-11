/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { VirtualizationAdminTable } from './ui'

const captured = vi.hoisted(() => ({
  tableProps: null as any,
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: (props: any) => {
    captured.tableProps = props
    return <div data-testid="admin-table">{props.toolbarExtra}</div>
  },
}))

vi.mock('@/components/management-list', () => ({
  ManagementDensityButton: ({ onClick }: { onClick?: () => void }) => (
    <button aria-label="切换表格密度" onClick={onClick} />
  ),
  ManagementRefreshButton: ({ onClick }: { onClick?: () => void }) => (
    <button aria-label="刷新列表" onClick={onClick} />
  ),
  ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

describe('VirtualizationAdminTable', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  })

  afterEach(async () => {
    await act(async () => root?.unmount())
    container?.remove()
    captured.tableProps = null
  })

  it('provides the route-level table controls and refresh behavior', async () => {
    const onRefresh = vi.fn()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    await act(async () => {
      root.render(
        <VirtualizationAdminTable
          actions={<button aria-label="新增资源" />}
          columns={[{ title: '名称', dataIndex: 'name' }]}
          dataSource={[]}
          onRefresh={onRefresh}
          rowKey="id"
        />,
      )
    })

    expect(captured.tableProps).toMatchObject({
      columnSettingIconOnly: true,
      columnSettingPlacement: 'toolbar',
      tableSize: 'small',
    })
    expect(container.querySelectorAll('button')).toHaveLength(3)

    await act(async () => {
      ;(container.querySelector('[aria-label="切换表格密度"]') as HTMLButtonElement).click()
    })
    expect(captured.tableProps.tableSize).toBe('middle')

    await act(async () => {
      ;(container.querySelector('[aria-label="刷新列表"]') as HTMLButtonElement).click()
    })
    expect(onRefresh).toHaveBeenCalledOnce()
  })

  it('keeps embedded tables compact without route-level controls', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    await act(async () => {
      root.render(
        <VirtualizationAdminTable
          columns={[{ title: '名称', dataIndex: 'name' }]}
          dataSource={[]}
          enableDensity={false}
          rowKey="id"
          showColumnSettings={false}
          showRefresh={false}
        />,
      )
    })

    expect(captured.tableProps.columnSettingPlacement).toBe('hidden')
    expect(container.querySelectorAll('button')).toHaveLength(0)
  })
})
