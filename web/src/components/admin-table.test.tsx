/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { act } from 'react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { AdminTable } from './admin-table'
import { tableColumnPresets } from '@/utils/table-columns'
import { ApiError } from '@/services/api-error'

const captured = vi.hoisted(() => ({
  tableProps: null as any,
}))
const i18n = vi.hoisted(() => ({
  localeCode: 'zh_CN' as 'zh_CN' | 'en_US',
}))

vi.mock('@/i18n', () => ({
  localeText: (locale: string, zh: string, en: string) => (locale === 'zh_CN' ? zh : en),
  useI18n: () => ({
    localeCode: i18n.localeCode,
    t: (_key: string, fallback = '') => fallback,
  }),
}))

vi.mock('antd', () => ({
  Alert: ({
    description,
    message,
    title,
    action,
  }: {
    description?: ReactNode
    message?: ReactNode
    title?: ReactNode
    action?: ReactNode
  }) => (
    <div>
      {title}
      {message}
      {description}
      {action}
    </div>
  ),
  Button: ({ children, ...props }: { children?: ReactNode }) => (
    <button {...props}>{children}</button>
  ),
  Checkbox: {
    Group: () => <div data-testid="checkbox-group" />,
  },
  Empty: Object.assign(
    ({ children, description }: { children?: ReactNode; description?: ReactNode }) => (
      <div>
        {description}
        {children}
      </div>
    ),
    { PRESENTED_IMAGE_SIMPLE: 'simple-empty' },
  ),
  Form: {
    Item: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  },
  Popover: ({ children }: { children?: ReactNode }) => <>{children}</>,
  Space: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  Table: (props: any) => {
    captured.tableProps = props
    return (
      <div data-testid="table-proxy">
        <table>
          <thead className="ant-table-thead" />
          <tbody>
            {props.dataSource.map((record: any, rowIndex: number) => (
              <tr key={rowIndex}>
                {props.columns.map((column: any, columnIndex: number) => (
                  <td key={columnIndex} {...column.onCell?.(record, rowIndex)}>
                    {column.render?.(record[column.dataIndex], record, rowIndex)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        <ul className="ant-table-pagination" />
      </div>
    )
  },
  Tooltip: ({ children }: { children?: ReactNode }) => <>{children}</>,
  Typography: {
    Text: ({ children }: { children?: ReactNode }) => <span>{children}</span>,
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderNode(node: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  await act(async () => {
    root.render(<>{node}</>)
  })

  return container
}

describe('AdminTable', () => {
  it.each([401, 403])('hides cached records when HTTP %s denies the read', async (status) => {
    const container = await renderNode(
      <AdminTable
        columns={[{ title: '名称', dataIndex: 'name', render: (name: string) => name }]}
        dataSource={[{ id: 'protected', name: 'protected record' }]}
        error={new ApiError(status, 'denied')}
        rowKey="id"
      />,
    )
    expect(container.textContent).not.toContain('protected record')
    expect(container.textContent).toContain('denied')
    expect(container.querySelector('[data-testid="table-proxy"]')).toBeNull()
  })

  it('keeps opt-in density, refresh and column controls on one header row', async () => {
    const refresh = vi.fn()
    const container = await renderNode(
      <AdminTable
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '标识', dataIndex: 'id' },
        ]}
        dataSource={[{ id: 'one', name: 'One' }]}
        rowKey="id"
        enableDensity
        onRefresh={refresh}
        columnSettingPlacement="header"
        columnSettingIconOnly
        headerExtra={<button>新增</button>}
      />,
    )
    expect(container.querySelector('.soha-admin-table-toolbar')).toBeNull()
    expect(container.querySelectorAll('.soha-admin-table-header button')).toHaveLength(4)
    await act(async () =>
      (container.querySelector('[aria-label="切换表格密度"]') as HTMLButtonElement).click(),
    )
    expect(captured.tableProps.size).toBe('middle')
    await act(async () =>
      (container.querySelector('[aria-label="刷新列表"]') as HTMLButtonElement).click(),
    )
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('preserves refresh access on failures without reporting an empty successful list', async () => {
    const container = await renderNode(
      <AdminTable
        columns={[{ title: '名称', dataIndex: 'name' }]}
        dataSource={[]}
        rowKey="id"
        error={new Error('请求失败')}
        onRefresh={() => {}}
        columnSettingPlacement="header"
      />,
    )
    expect(container.textContent).toContain('请求失败')
    expect(container.textContent).not.toContain('上次读取结果')
    expect(container.querySelector('[data-testid="table-proxy"]')).toBeNull()
    expect(container.querySelector('[aria-label="刷新列表"]')).not.toBeNull()
  })

  it('keeps authorized cached rows and offers a local retry after a temporary failure', async () => {
    const retry = vi.fn()
    const records = [{ id: 'old', name: 'previously read record' }]
    const container = await renderNode(
      <AdminTable
        columns={[{ title: '名称', dataIndex: 'name', render: (value: string) => value }]}
        dataSource={records}
        rowKey="id"
        error={new ApiError(503, 'temporarily unavailable')}
        onRetry={retry}
      />,
    )
    expect(container.textContent).toContain('previously read record')
    expect(container.querySelector('[data-testid="table-proxy"]')).not.toBeNull()
    await act(async () =>
      container.querySelector<HTMLButtonElement>('[aria-label="重试读取列表"]')!.click(),
    )
    expect(retry).toHaveBeenCalledOnce()
  })

  it('recognizes a previously read empty list after a temporary failure', async () => {
    const container = await renderNode(
      <AdminTable
        columns={[]}
        dataSource={[]}
        hasReadData
        rowKey="id"
        error={new ApiError(503, 'temporarily unavailable')}
      />,
    )
    expect(container.textContent).toContain('上次读取结果')
    expect(container.querySelector('[data-testid="table-proxy"]')).not.toBeNull()
  })

  it('forwards native pagination events when migrating an embedded table', async () => {
    const onChange = vi.fn()
    await renderNode(
      <AdminTable
        columns={[{ title: '名称', dataIndex: 'name' }]}
        dataSource={[]}
        rowKey="id"
        pagination={{ current: 2, pageSize: 10, total: 50, onChange }}
      />,
    )
    await act(async () => captured.tableProps.pagination.onChange(3, 10))
    expect(onChange).toHaveBeenCalledWith(3, 10)
    await act(async () => captured.tableProps.pagination.onChange(1, 20))
    expect(onChange).toHaveBeenCalledWith(1, 20)
  })
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  })

  afterEach(async () => {
    captured.tableProps = null
    i18n.localeCode = 'zh_CN'
    await act(async () => {
      for (const root of roots) {
        root.unmount()
      }
    })
    roots = []
    for (const container of containers) {
      container.remove()
    }
    containers = []
  })

  it('derives a default horizontal scroll width from active columns', async () => {
    await renderNode(
      <AdminTable
        columns={[
          { title: 'A', dataIndex: 'a', width: 220 },
          { title: 'B', dataIndex: 'b', width: 180 },
          { title: 'C', dataIndex: 'c' },
        ]}
        dataSource={[{ id: '1', a: 'a', b: 'b', c: 'c' }]}
        rowKey="id"
      />,
    )

    expect(captured.tableProps?.scroll).toEqual({ x: 960, y: undefined })
    expect(captured.tableProps?.size).toBe('small')
    expect(captured.tableProps?.pagination).toMatchObject({
      current: 1,
      pageSize: 15,
      pageSizeOptions: [10, 15, 20, 50, 100],
      showLessItems: true,
      showSizeChanger: true,
      size: 'small',
    })
  })

  it('keeps an explicit horizontal scroll width when provided', async () => {
    await renderNode(
      <AdminTable
        columns={[{ title: 'A', dataIndex: 'a', width: 220 }]}
        dataSource={[{ id: '1', a: 'a' }]}
        rowKey="id"
        scroll={{ x: 1440 }}
      />,
    )

    expect(captured.tableProps?.scroll).toEqual({ x: 1440, y: undefined })
  })

  it('passes pagination summary into Ant Design showTotal', async () => {
    await renderNode(
      <AdminTable
        columns={[{ title: 'A', dataIndex: 'a', width: 220 }]}
        dataSource={[{ id: '1', a: 'a' }]}
        paginationSummary="当前 1 / 3 条"
        rowKey="id"
      />,
    )

    expect(captured.tableProps?.pagination.showTotal(1, [1, 1])).toBe('当前 1 / 3 条')
  })

  it('shows the shared pagination range summary by default', async () => {
    await renderNode(
      <AdminTable
        columns={[{ title: 'A', dataIndex: 'a' }]}
        dataSource={[{ id: '1', a: 'a' }]}
        rowKey="id"
      />,
    )

    expect(captured.tableProps?.pagination.showTotal(24, [1, 10])).toBe('当前 1-10 / 24 条')
    expect(captured.tableProps?.pagination.showTotal(0, [0, 0])).toBe('当前 0 / 0 条')
  })

  it('shows the shared pagination range summary in English', async () => {
    i18n.localeCode = 'en_US'
    await renderNode(
      <AdminTable
        columns={[{ title: 'A', dataIndex: 'a' }]}
        dataSource={[{ id: '1', a: 'a' }]}
        rowKey="id"
      />,
    )

    expect(captured.tableProps?.pagination.showTotal(24, [1, 10])).toBe('1-10 / 24 items')
    expect(captured.tableProps?.pagination.showTotal(0, [0, 0])).toBe('0 / 0 items')
  })

  it('fits a viewport-scrolling table inside the content area', async () => {
    const container = await renderNode(
      <main className="soha-content" style={{ paddingBottom: 24 }}>
        <AdminTable
          columns={[{ title: 'A', dataIndex: 'a' }]}
          dataSource={[{ id: '1', a: 'a' }]}
          rowKey="id"
          viewportScroll
        />
      </main>,
    )
    const content = container.querySelector<HTMLElement>('.soha-content')!
    const header = container.querySelector<HTMLElement>('.ant-table-thead')!
    const pagination = container.querySelector<HTMLElement>('.ant-table-pagination')!
    Object.defineProperty(content, 'clientHeight', { configurable: true, value: 923 })
    content.getBoundingClientRect = () => ({ bottom: 979, top: 56 }) as DOMRect
    header.getBoundingClientRect = () => ({ bottom: 230 }) as DOMRect
    pagination.getBoundingClientRect = () => ({ height: 36 }) as DOMRect

    await act(async () => window.dispatchEvent(new Event('resize')))

    expect(captured.tableProps?.scroll.y).toBe(681)
    expect(
      container.querySelector('.soha-admin-table-shell')?.classList.contains('is-viewport-scroll'),
    ).toBe(true)

    Object.defineProperty(content, 'clientHeight', { configurable: true, value: 500 })
    await act(async () => window.dispatchEvent(new Event('resize')))
    expect(captured.tableProps?.scroll.y).toBeUndefined()
  })

  it('opts into a bounded virtual body without changing the paginated default', async () => {
    await renderNode(<AdminTable columns={[]} dataSource={[]} rowKey="id" />)
    expect(captured.tableProps.virtual).toBe(false)
    expect(captured.tableProps.pagination.pageSize).toBe(15)
    expect(captured.tableProps.scroll.y).toBeUndefined()

    await renderNode(
      <AdminTable
        columns={[]}
        dataSource={[]}
        rowKey="id"
        virtual
        viewportScroll
        pagination={false}
        scroll={{ x: 'max-content', scrollToFirstRowOnChange: false }}
      />,
    )
    expect(captured.tableProps.virtual).toBe(true)
    expect(captured.tableProps.pagination).toBe(false)
    expect(captured.tableProps.scroll).toEqual({
      x: 960,
      y: 240,
      scrollToFirstRowOnChange: false,
    })
    const Body = captured.tableProps.components.body.wrapper
    const body = await renderNode(<Body />)
    expect(body.querySelector('[role="region"]')?.getAttribute('tabindex')).toBe('0')
    expect(body.textContent).toBe('')
    expect(body.querySelector('[aria-label="资源列表，可连续滚动"]')).not.toBeNull()
  })

  it('adds local sorting to scalar columns without replacing explicit sorters', async () => {
    const explicitSorter = vi.fn(() => 0)
    await renderNode(
      <AdminTable
        columns={[
          { title: 'Name', dataIndex: 'name' },
          { title: 'Namespace', dataIndex: 'namespace' },
          { title: 'Age', dataIndex: 'ageSeconds' },
          { title: 'Selector', dataIndex: 'selector' },
          { title: 'Status', dataIndex: 'status', sorter: explicitSorter },
        ]}
        dataSource={[
          {
            id: '1',
            name: 'service-10',
            namespace: 'zeta',
            ageSeconds: 10,
            selector: { app: 'api' },
            status: 'ready',
          },
          {
            id: '2',
            name: 'service-2',
            namespace: 'alpha',
            ageSeconds: 2,
            selector: { app: 'web' },
            status: 'pending',
          },
        ]}
        localSorting
        rowKey="id"
      />,
    )

    const [name, namespace, age, selector, status] = captured.tableProps.columns
    expect(captured.tableProps.showSorterTooltip).toBe(false)
    expect(name.sorter({ name: 'service-10' }, { name: 'service-2' })).toBeGreaterThan(0)
    expect(namespace.sorter({ namespace: 'zeta' }, { namespace: 'alpha' })).toBeGreaterThan(0)
    expect(age.sorter({ ageSeconds: 2 }, { ageSeconds: 10 })).toBeLessThan(0)
    expect(selector.sorter).toBeUndefined()
    expect(status.sorter).toBe(explicitSorter)
  })

  it('removes native header hints while preserving sorting accessibility', async () => {
    await renderNode(<AdminTable columns={[]} dataSource={[]} rowKey="id" />)
    const HeaderCell = captured.tableProps.components.header.cell
    const container = await renderNode(
      <table>
        <thead>
          <tr>
            <HeaderCell title="Pod" aria-label="Pod" aria-sort="ascending" tabIndex={0}>
              Pod
            </HeaderCell>
          </tr>
        </thead>
      </table>,
    )
    const header = container.querySelector('th')
    expect(header?.hasAttribute('title')).toBe(false)
    expect(header?.getAttribute('aria-label')).toBe('Pod')
    expect(header?.getAttribute('aria-sort')).toBe('ascending')
    expect(header?.tabIndex).toBe(0)
  })

  it('separates server page and page-size callbacks', async () => {
    const onPageChange = vi.fn()
    const onPageSizeChange = vi.fn()
    await renderNode(
      <AdminTable
        columns={[{ title: 'A', dataIndex: 'a' }]}
        dataSource={[{ id: '1', a: 'a' }]}
        pagination={{
          current: 2,
          currentPage: 2,
          pageSize: 20,
          total: 41,
          onPageChange,
          onPageSizeChange,
        }}
        rowKey="id"
      />,
    )

    await act(async () => {
      captured.tableProps.pagination.onChange(3, 20)
    })
    expect(onPageChange).toHaveBeenCalledWith(3)
    expect(onPageSizeChange).not.toHaveBeenCalled()

    await act(async () => {
      captured.tableProps.pagination.onChange(1, 50)
    })
    expect(onPageSizeChange).toHaveBeenCalledWith(50)
    expect(onPageChange).toHaveBeenCalledTimes(1)
  })

  it('pins the shared action preset to the right side', () => {
    expect(tableColumnPresets.action.fixed).toBe('right')
    expect(tableColumnPresets.action.className).toBe('soha-table-actions-column')
    expect(tableColumnPresets.action.width).toBe(140)
  })

  it('keeps normalized name-backed actions unsortable, including explicit sort state', async () => {
    await renderNode(
      <AdminTable
        columns={[
          { title: '名称', dataIndex: 'name' },
          {
            ...tableColumnPresets.action,
            title: '操作',
            dataIndex: 'name',
            sorter: true,
            sortOrder: 'ascend',
            defaultSortOrder: 'descend',
          },
        ]}
        dataSource={[{ id: '1', name: 'demo' }]}
        rowKey="id"
        localSorting
      />,
    )
    expect(captured.tableProps.columns[0].sorter).toBeTypeOf('function')
    expect(captured.tableProps.columns[1]).toMatchObject({
      title: '',
      sorter: false,
      sortOrder: null,
      defaultSortOrder: undefined,
    })
  })

  it('sizes icon actions to the widest visible group and shrinks when permissions change', async () => {
    const bounds = vi
      .spyOn(HTMLElement.prototype, 'getBoundingClientRect')
      .mockImplementation(function (this: HTMLElement) {
        return {
          width: this.classList.contains('soha-row-action-icons') ? this.children.length * 24 : 0,
        } as DOMRect
      })
    const columns = [
      {
        ...tableColumnPresets.action,
        title: '操作',
        render: (_: unknown, record: { actions: number }) => (
          <span className="soha-row-action-icons">
            {Array.from({ length: record.actions }, (_, index) => (
              <button key={index}>Action</button>
            ))}
          </span>
        ),
      },
    ]
    try {
      await renderNode(
        <AdminTable
          columns={columns}
          dataSource={[
            { id: '1', actions: 1 },
            { id: '2', actions: 4 },
          ]}
          rowKey="id"
        />,
      )
      expect(captured.tableProps.columns[0].width).toBe(97)
      expect(
        captured.tableProps.columns[0].onCell().style['--soha-table-actions-column-width'],
      ).toBe('97px')
      await act(async () =>
        roots[0].render(
          <AdminTable columns={columns} dataSource={[{ id: '1', actions: 1 }]} rowKey="id" />,
        ),
      )
      expect(captured.tableProps.columns[0].width).toBe(40)
    } finally {
      bounds.mockRestore()
    }
  })

  it('preserves the explicit width from the shared action preset', async () => {
    await renderNode(
      <AdminTable
        columns={[
          { title: '名称', dataIndex: 'name' },
          { ...tableColumnPresets.action, title: '操作', render: () => null },
        ]}
        dataSource={[{ id: '1', name: 'demo' }]}
        rowKey="id"
      />,
    )

    expect(captured.tableProps.columns[1]).toMatchObject({
      className: expect.stringContaining('soha-table-actions-column'),
      fixed: 'right',
      width: 140,
    })
  })

  it('normalizes legacy action columns through the shared table contract', async () => {
    await renderNode(
      <AdminTable
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '操作', key: 'actions', fixed: 'right', width: 180, render: () => null },
        ]}
        dataSource={[{ id: '1', name: 'demo' }]}
        rowKey="id"
      />,
    )

    const actionColumn = captured.tableProps.columns[1]
    expect(actionColumn).toMatchObject({
      title: '',
      fixed: 'right',
      align: 'center',
      minWidth: 52,
      width: undefined,
    })
    expect(actionColumn.className).toContain('soha-table-actions-column')
    expect(actionColumn.className).toContain('soha-table-actions-column--auto')
    expect(actionColumn.onHeaderCell().className).toContain('soha-table-actions-column--auto')
    expect(actionColumn.onCell({}, 0).className).toContain('soha-table-actions-column--auto')
  })

  it('keeps business action data as a regular visible column', async () => {
    await renderNode(
      <AdminTable
        columns={[
          { title: '动作', dataIndex: 'actions', render: () => null },
          { title: '方式', key: 'action', width: 130, render: () => '滚动发布' },
        ]}
        dataSource={[{ id: '1', actions: ['view'] }]}
        rowKey="id"
      />,
    )

    expect(captured.tableProps.columns[0]).toMatchObject({
      title: '动作',
      dataIndex: 'actions',
    })
    expect(captured.tableProps.columns[0].fixed).toBeUndefined()
    expect(captured.tableProps.columns[1]).toMatchObject({
      title: '方式',
      key: 'action',
      width: 130,
    })
    expect(captured.tableProps.columns[1].fixed).toBeUndefined()
  })

  it('keeps action columns visible and out of column selection', async () => {
    const container = await renderNode(
      <AdminTable
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '状态', dataIndex: 'status' },
          { title: '操作', key: 'actions', fixed: 'right', render: () => null },
        ]}
        dataSource={[{ id: '1', name: 'demo', status: 'ready' }]}
        rowKey="id"
      />,
    )

    expect(captured.tableProps.columns).toHaveLength(3)
    expect(captured.tableProps.columns[2].fixed).toBe('right')
    expect(container.textContent).not.toContain('操作')
  })

  it('preserves explicit K8s action widths while hiding the column title', async () => {
    await renderNode(
      <AdminTable
        columns={[
          {
            title: '操作',
            key: 'actions',
            fixed: 'right',
            width: 112,
            onHeaderCell: () => ({ className: 'soha-table-actions-column k8s-actions' }),
            render: () => null,
          },
        ]}
        dataSource={[{ id: '1' }]}
        rowKey="id"
      />,
    )

    const actionColumn = captured.tableProps.columns[0]
    expect(actionColumn.title).toBe('')
    expect(actionColumn.width).toBe(112)
    expect(actionColumn.className).not.toContain('soha-table-actions-column--auto')
    expect(actionColumn.onHeaderCell().className).toContain('k8s-actions')
  })

  it('honors unfixed actions only for the opted-in column layout', async () => {
    const columns = [
      { ...tableColumnPresets.action, key: 'actions', fixed: false, render: () => null },
    ]
    await renderNode(
      <AdminTable
        columns={columns}
        dataSource={[{ id: '1' }]}
        rowKey="id"
        columnLayoutKey="k8s-mobile"
      />,
    )
    expect(captured.tableProps.columns[0].fixed).toBe(false)
    await renderNode(<AdminTable columns={columns} dataSource={[{ id: '1' }]} rowKey="id" />)
    expect(captured.tableProps.columns[0].fixed).toBe('right')
  })
})
