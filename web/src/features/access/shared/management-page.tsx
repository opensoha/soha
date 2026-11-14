import { useState, type ReactNode } from 'react'
import type { TableColumnsType } from 'antd'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementKeywordField,
  ManagementDensityButton,
  ManagementQueryActions,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { useI18n } from '@/i18n'

interface AccessManagementTablePageProps<T extends object> {
  children?: ReactNode
  columns: TableColumnsType<T>
  createAction?: ReactNode
  dataSource: T[]
  loading?: boolean
  onRefresh: () => void
  placeholder: string
  refreshing?: boolean
  error?: Error | null
  hasReadData?: boolean
  rowKey: string | ((record: T) => string)
  searchKeyword: string
  setSearchKeyword: (value: string) => void
  resourceName: string
}

export function AccessManagementTablePage<T extends object>({
  children,
  columns,
  createAction,
  dataSource,
  loading,
  onRefresh,
  placeholder,
  refreshing,
  rowKey,
  searchKeyword,
  setSearchKeyword,
  resourceName,
  error,
  hasReadData,
}: AccessManagementTablePageProps<T>) {
  const { localeCode } = useI18n()
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')

  return (
    <ManagementDataPage
      query={{
        onFinish: () => undefined,
        actions: (
          <ManagementQueryActions
            disabledReset={!searchKeyword.trim()}
            onReset={() => setSearchKeyword('')}
            resetLabel={localeCode === 'zh_CN' ? '重置' : 'Reset'}
            submitLabel={localeCode === 'zh_CN' ? '查询' : 'Search'}
          />
        ),
        children: (
          <ManagementKeywordField
            label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
            placeholder={placeholder}
            value={searchKeyword}
            onChange={setSearchKeyword}
            inputProps={{
              className: 'soha-platform-compact-field soha-workload-search-input',
            }}
          />
        ),
      }}
      table={{
        columnSettingIconOnly: true,
        columnSettingPlacement: 'header',
        className: 'soha-access-table',
        headerExtra: (
          <ManagementTableToolbar>
            {createAction}
            <ManagementDensityButton
              aria-label={
                localeCode === 'zh_CN'
                  ? `切换${resourceName}表格密度`
                  : `Toggle ${resourceName.toLowerCase()} table density`
              }
              size="small"
              tooltip={
                localeCode === 'zh_CN'
                  ? tableSize === 'small'
                    ? '切换为宽松密度'
                    : '切换为紧凑密度'
                  : tableSize === 'small'
                    ? 'Use comfortable density'
                    : 'Use compact density'
              }
              onClick={() => setTableSize((current) => (current === 'small' ? 'middle' : 'small'))}
            />
            <ManagementRefreshButton
              aria-label={
                localeCode === 'zh_CN'
                  ? `刷新${resourceName}`
                  : `Refresh ${resourceName.toLowerCase()}`
              }
              loading={refreshing}
              size="small"
              tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onClick={onRefresh}
            />
          </ManagementTableToolbar>
        ),
        columns,
        dataSource,
        rowKey,
        loading,
        error,
        hasReadData,
        onRetry: onRefresh,
        scroll: { x: 'max-content' },
        tableSize,
      }}
    >
      {children}
    </ManagementDataPage>
  )
}
