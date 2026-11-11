import type { ComponentProps, ReactNode } from 'react'
import { useState } from 'react'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDensityButton,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { localeText, useI18n } from '@/i18n'

type AdminTableProps = ComponentProps<typeof AdminTable>

function classNames(...items: Array<string | false | null | undefined>) {
  return items.filter(Boolean).join(' ')
}

type VirtualizationAdminTableProps = Omit<
  AdminTableProps,
  | 'columnSettingIconOnly'
  | 'columnSettingPlacement'
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
  toolbarExtra?: ReactNode
  onRefresh?: () => void
}

export function VirtualizationAdminTable({
  actions,
  className,
  enableDensity = true,
  onRefresh,
  pagination,
  paginationSummary,
  refreshing,
  shellClassName,
  showColumnSettings = true,
  showRefresh = true,
  title,
  toolbarExtra,
  viewportScroll,
  ...tableProps
}: VirtualizationAdminTableProps) {
  const { localeCode } = useI18n()
  const [tableSize, setTableSize] = useState<NonNullable<AdminTableProps['tableSize']>>('small')
  const resolvedPaginationSummary =
    paginationSummary ??
    ((total: number, range: [number, number]) =>
      total <= 0
        ? localeText(localeCode, '当前 0 / 0 条', '0 of 0')
        : localeText(
            localeCode,
            `当前 ${range[0]}-${range[1]} / ${total} 条`,
            `${range[0]}-${range[1]} of ${total}`,
          ))
  const tableControls =
    toolbarExtra || actions || enableDensity || (showRefresh && onRefresh) ? (
      <ManagementTableToolbar>
        {toolbarExtra}
        {actions}
        {enableDensity ? (
          <ManagementDensityButton
            aria-label={localeText(localeCode, '切换表格密度', 'Toggle table density')}
            size="small"
            tooltip={
              tableSize === 'small'
                ? localeText(localeCode, '切换为宽松密度', 'Use relaxed density')
                : localeText(localeCode, '切换为紧凑密度', 'Use compact density')
            }
            onClick={() => setTableSize((current) => (current === 'small' ? 'middle' : 'small'))}
          />
        ) : null}
        {showRefresh && onRefresh ? (
          <ManagementRefreshButton
            aria-label={localeText(localeCode, '刷新列表', 'Refresh list')}
            loading={refreshing}
            size="small"
            tooltip={localeText(localeCode, '刷新', 'Refresh')}
            onClick={onRefresh}
          />
        ) : null}
      </ManagementTableToolbar>
    ) : undefined

  return (
    <AdminTable
      {...tableProps}
      className={classNames('soha-vrt-table', className)}
      columnSettingIconOnly
      columnSettingPlacement={showColumnSettings ? (title ? 'header' : 'toolbar') : 'hidden'}
      pagination={pagination}
      paginationSummary={pagination === false ? undefined : resolvedPaginationSummary}
      shellClassName={classNames('soha-management-table-shell', shellClassName)}
      tableSize={tableSize}
      title={title}
      toolbarExtra={tableControls}
      viewportScroll={viewportScroll ?? showColumnSettings}
    />
  )
}
