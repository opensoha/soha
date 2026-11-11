import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryPanel,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { useI18n } from '@/i18n'
import { ReloadOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { Button } from 'antd'
import { useMemo, useState, type ReactNode } from 'react'
interface TablePaneProps<
  T extends {
    id: string
  },
> {
  columns: TableColumnsType<T>
  createAction?: ReactNode
  getSearchValues: (item: T) => unknown[]
  items?: T[]
  loading: boolean
  error: boolean | Error | null
  errorDescription?: string
  onRefresh: () => void
  refreshing: boolean
  searchPlaceholder: string
}
export function TablePane<
  T extends {
    id: string
  },
>({
  columns,
  createAction,
  error,
  errorDescription,
  getSearchValues,
  items: sourceItems,
  loading,
  onRefresh,
  refreshing,
  searchPlaceholder,
}: TablePaneProps<T>) {
  const { t } = useI18n()
  const items = sourceItems ?? []
  const [search, setSearch] = useState('')
  const [draftSearch, setDraftSearch] = useState('')
  const filtered = useMemo(() => {
    const keyword = search.trim().toLocaleLowerCase()
    if (!keyword) return items
    return items.filter((item) =>
      getSearchValues(item).some((value) =>
        String(value ?? '')
          .toLocaleLowerCase()
          .includes(keyword),
      ),
    )
  }, [getSearchValues, items, search])
  const tableActions = (
    <ManagementTableToolbar>
      {createAction}
      <ManagementIconButton
        aria-label={t('networkAccess.refresh', '刷新')}
        icon={<ReloadOutlined />}
        loading={refreshing}
        tooltip={t('networkAccess.refresh', '刷新')}
        onClick={onRefresh}
      />
    </ManagementTableToolbar>
  )
  return (
    <div className="soha-page">
      <ManagementQueryPanel
        actions={
          <ManagementQueryActions
            disabledReset={!draftSearch && !search}
            onReset={() => {
              setDraftSearch('')
              setSearch('')
            }}
          />
        }
        onFinish={() => setSearch(draftSearch.trim())}
      >
        <ManagementKeywordField
          onChange={setDraftSearch}
          placeholder={searchPlaceholder}
          value={draftSearch}
        />
      </ManagementQueryPanel>
      <AdminTable
        error={error instanceof Error ? error : undefined}
        hasReadData={sourceItems !== undefined}
        onRetry={onRefresh}
        columns={columns}
        dataSource={filtered}
        empty={
          error === true ? (
            <ManagementState
              actions={
                errorDescription ? (
                  <Button autoInsertSpace={false} onClick={onRefresh}>
                    {t('networkAccess.refresh', '刷新')}
                  </Button>
                ) : undefined
              }
              compact
              description={errorDescription}
              kind="error"
            />
          ) : undefined
        }
        loading={loading}
        localSorting
        rowKey="id"
        shellClassName="soha-management-table-shell"
        headerExtra={tableActions}
        columnSettingPlacement="header"
        columnSettingIconOnly
        viewportScroll
      />
    </div>
  )
}
