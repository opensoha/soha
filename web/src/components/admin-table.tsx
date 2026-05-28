import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Button, Checkbox, Empty, Popover, Table, Typography } from 'antd'
import { SettingOutlined } from '@ant-design/icons'

const DEFAULT_PAGE_SIZE_OPTIONS = [10, 20, 50, 100]
const { Text } = Typography

interface AdminTableProps {
  className?: string
  columns: any[]
  currentPageSelectionLabel?: ReactNode
  dataSource: any[]
  empty?: ReactNode
  enableColumnSelection?: boolean
  expandable?: any
  expandedRowRender?: (record: any, index?: number) => ReactNode
  headerExtra?: ReactNode
  hideExpandedColumn?: boolean
  loading?: boolean
  onRow?: any
  pageSize?: number
  pagination?: any
  rowKey: string | ((record: any) => string)
  rowSelection?: any
  shellClassName?: string
  scroll?: {
    x?: string | number
    y?: string | number
  }
  selectCurrentPageOnly?: boolean
  title?: ReactNode
  toolbar?: ReactNode
  toolbarExtra?: ReactNode
}

function getColumnId(column: any, index: number) {
  if (typeof column?.key === 'string' && column.key) return column.key
  if (typeof column?.dataIndex === 'string' && column.dataIndex) return `${column.dataIndex}:${index}`
  if (Array.isArray(column?.dataIndex) && column.dataIndex.length > 0) return `${column.dataIndex.join('.')}:${index}`
  if (typeof column?.title === 'string' && column.title) return `${column.title}:${index}`
  return `column:${index}`
}

function getColumnLabel(column: any, index: number) {
  if (typeof column?.title === 'string' && column.title) return column.title
  if (typeof column?.dataIndex === 'string' && column.dataIndex) return column.dataIndex
  if (Array.isArray(column?.dataIndex) && column.dataIndex.length > 0) return column.dataIndex.join('.')
  return `列 ${index + 1}`
}

function getColumnWidth(column: any) {
  if (typeof column?.width === 'number' && Number.isFinite(column.width)) {
    return column.width
  }
  if (typeof column?.width === 'string') {
    const parsed = Number.parseInt(column.width, 10)
    if (Number.isFinite(parsed)) {
      return parsed
    }
  }
  return null
}

export function AdminTable({
  className,
  columns,
  currentPageSelectionLabel,
  dataSource,
  empty,
  enableColumnSelection = true,
  headerExtra,
  loading,
  pageSize = 10,
  pagination,
  rowKey,
  rowSelection,
  shellClassName,
  scroll,
  selectCurrentPageOnly = false,
  title,
  toolbar,
  toolbarExtra,
  ...rest
}: AdminTableProps) {
  const columnOptions = useMemo(() => columns.map((column, index) => ({
    id: getColumnId(column, index),
    label: getColumnLabel(column, index),
    column,
  })), [columns])
  const columnSignature = columnOptions.map((option) => option.id).join('|')
  const [visibleColumnIds, setVisibleColumnIds] = useState<string[]>([])
  const [currentPage, setCurrentPage] = useState(1)
  const [currentPageSize, setCurrentPageSize] = useState(pageSize)

  useEffect(() => {
    const nextIds = columnOptions.map((option) => option.id)
    setVisibleColumnIds((current) => {
      const filtered = current.filter((id) => nextIds.includes(id))
      return filtered.length > 0 ? filtered : nextIds
    })
  }, [columnSignature, columnOptions])

  useEffect(() => {
    if (pagination && pagination !== false && typeof pagination.currentPage === 'number') {
      setCurrentPage(pagination.currentPage)
    }
  }, [pagination])

  useEffect(() => {
    const nextPageSize = pagination && pagination !== false && typeof pagination.pageSize === 'number'
      ? pagination.pageSize
      : pageSize
    setCurrentPageSize(nextPageSize)
  }, [pageSize, pagination])

  const activeColumnIds = visibleColumnIds.length > 0 ? visibleColumnIds : columnOptions.map((option) => option.id)
  const activeColumns = columnOptions
    .filter((option) => activeColumnIds.includes(option.id))
    .map((option) => option.column)
  const estimatedScrollWidth = useMemo(() => {
    const columnWidth = activeColumns.reduce((total, column) => total + (getColumnWidth(column) ?? 168), 0)
    const selectionWidth = rowSelection ? 56 : 0
    return Math.max(960, columnWidth + selectionWidth)
  }, [activeColumns, rowSelection])

  const getRowKeyValue = (record: any) => (typeof rowKey === 'function' ? rowKey(record) : record?.[rowKey])
  const activeRowSelection = rowSelection && typeof rowSelection === 'object' ? rowSelection : undefined
  const currentPageRows = !selectCurrentPageOnly || pagination === false
    ? dataSource
    : dataSource.slice((currentPage - 1) * currentPageSize, currentPage * currentPageSize)
  const pageSelectableRows = activeRowSelection
    ? currentPageRows.filter((record) => !activeRowSelection.getCheckboxProps?.(record)?.disabled)
    : []
  const pageSelectableKeys = pageSelectableRows.map((record) => getRowKeyValue(record))
  const selectedKeySet = new Set(activeRowSelection?.selectedRowKeys ?? [])
  const pageAllSelected = pageSelectableKeys.length > 0 && pageSelectableKeys.every((key) => selectedKeySet.has(key))
  const pageIndeterminate = pageSelectableKeys.some((key) => selectedKeySet.has(key)) && !pageAllSelected

  const resolvedRowSelection = selectCurrentPageOnly && activeRowSelection
    ? {
        ...activeRowSelection,
        columnTitle: currentPageSelectionLabel ? <span className="soha-admin-table-selection-label">{currentPageSelectionLabel}</span> : activeRowSelection.columnTitle,
        title: currentPageSelectionLabel ? <span className="soha-admin-table-selection-label">{currentPageSelectionLabel}</span> : undefined,
        getTitleCheckboxProps: () => ({
          checked: pageAllSelected,
          indeterminate: pageIndeterminate,
        }),
        onSelectAll: (selected: boolean) => {
          const selectedRowKeys = Array.isArray(activeRowSelection.selectedRowKeys) ? [...activeRowSelection.selectedRowKeys] : []
          const pageKeySet = new Set(pageSelectableKeys)
          const nextSelectedRowKeys = selected
            ? Array.from(new Set(selectedRowKeys.concat(pageSelectableKeys)))
            : selectedRowKeys.filter((key) => !pageKeySet.has(key))
          const nextSelectedKeySet = new Set(nextSelectedRowKeys)
          const nextSelectedRows = dataSource.filter((record) => nextSelectedKeySet.has(getRowKeyValue(record)))
          activeRowSelection.onChange?.(nextSelectedRowKeys, nextSelectedRows)
        },
      }
    : rowSelection

  const resolvedPagination = pagination === false
    ? false
    : {
        pageSize: currentPageSize,
        current: currentPage,
        showSizeChanger: true,
        pageSizeOptions: DEFAULT_PAGE_SIZE_OPTIONS,
        ...pagination,
        onChange: (nextPage: number, nextPageSize: number) => {
          setCurrentPage(nextPage)
          setCurrentPageSize(nextPageSize)
          pagination?.onPageChange?.(nextPage)
          pagination?.onPageSizeChange?.(nextPageSize)
        },
      }

  const resolvedScroll = useMemo(() => ({
    x: scroll?.x ?? estimatedScrollWidth,
    y: scroll?.y,
  }), [estimatedScrollWidth, scroll?.x, scroll?.y])

  const columnSetting = enableColumnSelection && columnOptions.length > 1 ? (
    <Popover
      trigger="click"
      placement="bottomRight"
      content={
        <div className="soha-admin-table-column-popover">
          <div className="soha-admin-table-column-actions">
            <Button size="small" type="text" onClick={() => setVisibleColumnIds(columnOptions.map((option) => option.id))}>
              全选
            </Button>
          </div>
          <Checkbox.Group
            options={columnOptions.map((option) => ({ label: option.label, value: option.id }))}
            value={activeColumnIds}
            onChange={(value) => {
              const next = value as string[]
              if (next.length === 0) return
              setVisibleColumnIds(next)
            }}
          />
          <Text type="secondary" style={{ display: 'block', marginTop: 8 }}>列设置仅影响当前页面会话。</Text>
        </div>
      }
    >
      <Button size="small" icon={<SettingOutlined />}>列设置</Button>
    </Popover>
  ) : null

  const resolvedToolbarExtra = toolbarExtra || columnSetting ? (
    <>
      {toolbarExtra}
      {columnSetting}
    </>
  ) : null

  const hasHeader = Boolean(title || headerExtra)
  const hasToolbar = Boolean(toolbar || resolvedToolbarExtra)
  const resolvedShellClassName = ['soha-admin-table-shell', shellClassName, hasHeader || hasToolbar ? 'is-panel' : ''].filter(Boolean).join(' ')
  const resolvedTableClassName = ['soha-admin-table', className].filter(Boolean).join(' ')

  return (
    <div className={resolvedShellClassName}>
      {hasHeader ? (
        <div className="soha-admin-table-header">
          <div className="soha-admin-table-header-main">{title}</div>
          {headerExtra ? <div className="soha-admin-table-header-extra">{headerExtra}</div> : null}
        </div>
      ) : null}
      {hasToolbar ? (
        <div className="soha-admin-table-toolbar">
          {toolbar ? <div className="soha-admin-table-toolbar-main">{toolbar}</div> : null}
          {resolvedToolbarExtra ? <div className="soha-admin-table-toolbar-extra">{resolvedToolbarExtra}</div> : null}
        </div>
      ) : null}
      <Table
        {...rest}
        className={resolvedTableClassName}
        columns={activeColumns}
        dataSource={dataSource}
        loading={loading}
        locale={{ emptyText: empty ?? <Empty description="暂无数据" /> }}
        pagination={resolvedPagination}
        rowKey={rowKey}
        rowSelection={resolvedRowSelection}
        scroll={resolvedScroll}
        size="middle"
      />
    </div>
  )
}
