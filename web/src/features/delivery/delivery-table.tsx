import { useState } from 'react'
import type { ComponentProps, ReactNode } from 'react'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDensityButton,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import './styles/shared.css'

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
  errorDescription?: ReactNode
  isError?: boolean
  onRetry?: () => void
  paginationSummary?: AdminTableProps['paginationSummary']
  refreshing?: boolean
  showColumnSettings?: boolean
  showRefresh?: boolean
  shellClassName?: string
  onRefresh?: () => void
}

const DEFAULT_PAGINATION_SUMMARY: NonNullable<AdminTableProps['paginationSummary']> = (
  total,
  range,
) => {
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
  errorDescription,
  isError = false,
  error,
  onRetry,
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
          onClick={() => setTableSize((current) => (current === 'small' ? 'middle' : 'small'))}
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

  return (
    <AdminTable
      {...tableProps}
      columnSettingIconOnly
      columnSettingPlacement={showColumnSettings ? 'header' : 'hidden'}
      enableColumnSelection={showColumnSettings && enableColumnSelection !== false}
      error={
        error ??
        (isError
          ? new Error(
              typeof errorDescription === 'string' ? errorDescription : '暂时无法读取列表。',
            )
          : null)
      }
      onRetry={onRetry}
      empty={tableProps.empty}
      headerExtra={utilityToolbar}
      pagination={pagination}
      paginationSummary={
        pagination === false ? undefined : (paginationSummary ?? DEFAULT_PAGINATION_SUMMARY)
      }
      scroll={scroll ?? { x: 'max-content' }}
      shellClassName={classNames(
        'soha-management-table-shell',
        'soha-delivery-table-shell',
        shellClassName,
      )}
      tableSize={tableSize}
    />
  )
}
