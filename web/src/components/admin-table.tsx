import { useEffect, useState } from 'react'
import { Button, CheckboxGroup, Empty, Popover, Table, Typography } from '@douyinfe/semi-ui'
import { IconSetting } from '@douyinfe/semi-icons'
import type { ReactNode } from 'react'

const DEFAULT_PAGE_SIZE_OPTIONS = [10, 20, 50, 100]
const { Text } = Typography

interface AdminTableProps {
  columns: any[]
  dataSource: any[]
  rowKey: string | ((record: any) => string)
  loading?: boolean
  pageSize?: number
  pagination?: any
  rowSelection?: any
  empty?: ReactNode
  className?: string
  scroll?: {
    x?: string | number
    y?: string | number
  }
  enableColumnSelection?: boolean
  expandedRowRender?: (record: any, index?: number) => ReactNode
  hideExpandedColumn?: boolean
  title?: ReactNode
  headerExtra?: ReactNode
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

export function AdminTable({
  columns,
  pageSize = 10,
  pagination,
  empty,
  className,
  scroll,
  enableColumnSelection = true,
  title,
  headerExtra,
  toolbar,
  toolbarExtra,
  ...rest
}: AdminTableProps) {
  const columnOptions = columns.map((column, index) => ({
    id: getColumnId(column, index),
    label: getColumnLabel(column, index),
    column,
  }))
  const columnSignature = columnOptions.map((option) => option.id).join('|')
  const [visibleColumnIds, setVisibleColumnIds] = useState<string[]>([])

  useEffect(() => {
    const nextIds = columnOptions.map((option) => option.id)
    setVisibleColumnIds((current) => {
      const filtered = current.filter((id) => nextIds.includes(id))
      const next = filtered.length > 0 ? filtered : nextIds
      if (current.length === next.length && current.every((id, index) => id === next[index])) {
        return current
      }
      return next
    })
  }, [columnSignature])

  const activeColumnIds = visibleColumnIds.length > 0 ? visibleColumnIds : columnOptions.map((option) => option.id)
  const activeColumns = columnOptions
    .filter((option) => activeColumnIds.includes(option.id))
    .map((option) => option.column)

  const mergedPagination = pagination === false
    ? false
    : {
        pageSize,
        showSizeChanger: true,
        pageSizeOpts: DEFAULT_PAGE_SIZE_OPTIONS,
        ...pagination,
      }

  const columnSetting = enableColumnSelection && columnOptions.length > 1
    ? (
      <Popover
        trigger="click"
        position="bottomRight"
        content={
          <div className="kc-admin-table-column-popover">
            <div className="kc-admin-table-column-actions">
              <Button size="small" theme="borderless" onClick={() => setVisibleColumnIds(columnOptions.map((option) => option.id))}>
                全选
              </Button>
            </div>
            <CheckboxGroup
              direction="vertical"
              options={columnOptions.map((option) => ({ label: option.label, value: option.id }))}
              value={activeColumnIds}
              onChange={(value) => {
                const next = value as string[]
                if (next.length === 0) return
                setVisibleColumnIds(next)
              }}
            />
            <Text type="tertiary" size="small">列设置仅影响当前页面会话。</Text>
          </div>
        }
      >
        <Button icon={<IconSetting />} theme="light">
          列设置
        </Button>
      </Popover>
    )
    : null

  const resolvedToolbarExtra = toolbarExtra || columnSetting
    ? (
      <>
        {toolbarExtra}
        {columnSetting}
      </>
    )
    : null
  const hasHeader = Boolean(title || headerExtra)
  const hasToolbar = Boolean(toolbar || resolvedToolbarExtra)

  return (
    <div className={['kc-admin-table-shell', className, hasHeader || hasToolbar ? 'is-panel' : ''].filter(Boolean).join(' ')}>
      {hasHeader ? (
        <div className="kc-admin-table-header">
          <div className="kc-admin-table-header-main">{title}</div>
          {headerExtra ? <div className="kc-admin-table-header-extra">{headerExtra}</div> : null}
        </div>
      ) : null}
      {hasToolbar ? (
        <div className="kc-admin-table-toolbar">
          {toolbar ? <div className="kc-admin-table-toolbar-main">{toolbar}</div> : null}
          {resolvedToolbarExtra ? <div className="kc-admin-table-toolbar-extra">{resolvedToolbarExtra}</div> : null}
        </div>
      ) : null}
      <Table
        {...rest}
        columns={activeColumns}
        className="kc-admin-table"
        size="middle"
        scroll={{ x: '100%', ...scroll }}
        pagination={mergedPagination}
        empty={empty ?? <Empty description="暂无数据" />}
      />
    </div>
  )
}
