import { useState } from 'react'
import type { ComponentProps, ReactNode } from 'react'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDensityButton,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import './delivery-pages.css'

type AdminTableProps = ComponentProps<typeof AdminTable>

type DeliveryTableProps = Omit<
  AdminTableProps,
  | 'columnSettingIconOnly'
  | 'columnSettingPlacement'
  | 'headerExtra'
  | 'paginationSummary'
  | 'shellClassName'
  | 'tableSize'
  | 'toolbarExtra'
> & {
  actions?: ReactNode
  enableDensity?: boolean
  paginationSummary?: AdminTableProps['paginationSummary']
  refreshing?: boolean
  showColumnSettings?: boolean
  showRefresh?: boolean
  shellClassName?: string
  onRefresh?: () => void
}

const DEFAULT_PAGINATION_SUMMARY: NonNullable<AdminTableProps['paginationSummary']> = (total, range) => {
  if (total <= 0) return '当前 0 / 0 条'
  return `当前 ${range[0]}-${range[1]} / ${total} 条`
}

function classNames(...items: Array<string | false | null | undefined>) {
  return items.filter(Boolean).join(' ')
}

export function DeliveryTable({
  actions,
  enableColumnSelection,
  enableDensity = true,
  pagination,
  paginationSummary,
  refreshing,
  scroll,
  shellClassName,
  showColumnSettings = true,
  showRefresh,
  onRefresh,
  ...tableProps
}: DeliveryTableProps) {
  const [tableSize, setTableSize] = useState<AdminTableProps['tableSize']>('small')
  const resolvedShowRefresh = showRefresh ?? Boolean(onRefresh)
  const hasUtilities = Boolean(actions || enableDensity || resolvedShowRefresh)
  const utilityToolbar = hasUtilities ? (
    <ManagementTableToolbar>
      {actions}
      {enableDensity ? (
        <ManagementDensityButton
          aria-label="切换表格密度"
          tooltip={tableSize === 'small' ? '切换为舒展密度' : '切换为紧凑密度'}
          onClick={() => setTableSize((current) => current === 'small' ? 'middle' : 'small')}
        />
      ) : null}
      {resolvedShowRefresh ? (
        <ManagementRefreshButton
          aria-label="刷新"
          loading={refreshing}
          tooltip="刷新"
          onClick={onRefresh}
        />
      ) : null}
    </ManagementTableToolbar>
  ) : undefined
  const hasTitle = Boolean(tableProps.title)

  return (
    <AdminTable
      {...tableProps}
      columnSettingIconOnly
      columnSettingPlacement={showColumnSettings ? (hasTitle ? 'header' : 'toolbar') : 'hidden'}
      enableColumnSelection={showColumnSettings && enableColumnSelection !== false}
      headerExtra={hasTitle ? utilityToolbar : undefined}
      pagination={pagination}
      paginationSummary={pagination === false ? undefined : paginationSummary ?? DEFAULT_PAGINATION_SUMMARY}
      scroll={scroll ?? { x: 'max-content' }}
      shellClassName={classNames('soha-management-table-shell', 'soha-delivery-table-shell', shellClassName)}
      tableSize={tableSize}
      toolbarExtra={hasTitle ? undefined : utilityToolbar}
    />
  )
}
