import {
  getResourceName,
  kubernetesDeleteConfirmation,
  type KubernetesResourceAction,
  useKubernetesResourceActions,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { encodeAIContextForElement, useAIPageContext } from '@/features/copilot'
import { CreateEntry } from '@/features/platform/resource-creation/components/create-entry'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined, FormOutlined } from '@ant-design/icons'
import {
  useQuery,
  useQueryClient,
  type MutationOptions,
  type UseQueryOptions,
} from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useCallback, useDeferredValue, useMemo, useState } from 'react'
import '../styles.css'
import { toClusterStorageScope, toStorageScope } from './scope'
import type { StorageTarget } from './types'

const EMPTY_ITEMS: never[] = []
const TABLE_SCROLL = { x: 'max-content' } as const

function includesSearch(values: Array<string | undefined | null>, keyword: string) {
  if (!keyword) return true
  return values.some((value) => (value ?? '').toLowerCase().includes(keyword))
}

export function StorageListPage<T extends { allowedActions?: string[]; name: string }>({
  clusterScoped,
  columns,
  createDefaultTemplate,
  emptyLabel,
  getRecordNamespace,
  kind,
  listQuery,
  removeOptions,
  resourceLabel,
  rowKey,
  searchPlaceholder,
  searchValues,
  permissionKey,
  onEdit,
  autoRefresh = false,
}: {
  permissionKey?: string
  onEdit?: (record: T) => void
  autoRefresh?: boolean
  clusterScoped: boolean
  columns: TableColumnsType<T>
  createDefaultTemplate: string
  emptyLabel: { zh_CN: string; en_US: string }
  getRecordNamespace?: (record: T) => string
  kind: string
  listQuery: (scope: ScopeKey) => UseQueryOptions<T[], Error, T[], readonly unknown[]>
  removeOptions: (
    queryClient: ReturnType<typeof useQueryClient>,
  ) => MutationOptions<void, Error, StorageTarget>
  resourceLabel: string
  rowKey: string | ((record: T) => string)
  searchPlaceholder: { zh_CN: string; en_US: string }
  searchValues: (record: T) => Array<string | undefined | null>
}) {
  const { localeCode } = useI18n()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const listScope = clusterScoped
    ? toClusterStorageScope(clusterId)
    : toStorageScope(clusterId, namespace)
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = !permissionKey || hasPermission(permissions, `${permissionKey}.view`)
  const refresh = useWorkloadRefresh()
  const options = listQuery(listScope)
  const query = useQuery({
    ...options,
    enabled: canRead && options.enabled,
    ...(autoRefresh ? { refetchInterval: refresh.interval } : {}),
  })
  const visibleQueryData = readableQueryData(query)
  const removeMutation = usePlatformMutation(removeOptions(queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = removeMutation.blocked
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const normalizedKeyword = useDeferredValue(searchKeyword).trim().toLowerCase()
  const rawItems = canRead ? (visibleQueryData ?? EMPTY_ITEMS) : EMPTY_ITEMS
  const filteredItems = useMemo(
    () => rawItems.filter((item) => includesSearch(searchValues(item), normalizedKeyword)),
    [normalizedKeyword, rawItems, searchValues],
  )
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: resourceLabel,
    entityKind: kind,
    clusterId: clusterId || undefined,
    namespace: clusterScoped ? undefined : namespace || undefined,
    visibleFilters: { keyword: searchKeyword || undefined },
    pinnedData: { itemCount: rawItems.length },
  })
  const {
    isPending: removePending,
    variables: removeVariables,
    reset: resetRemove,
    mutateAsync: removeAsync,
    feedback: removeFeedback,
  } = removeMutation
  const actions = useCallback<(record: T) => KubernetesResourceAction[]>(
    (record) => {
      const targetScope = clusterScoped
        ? toClusterStorageScope(clusterId)
        : toStorageScope(clusterId, getRecordNamespace?.(record))
      return [
        ...(onEdit &&
        canRead &&
        (!permissionKey || hasPermission(permissions, `${permissionKey}.update`)) &&
        hasAllowedAction(record.allowedActions, 'update')
          ? [
              {
                key: 'edit',
                label: localeCode === 'zh_CN' ? '编辑' : 'Edit',
                icon: <FormOutlined />,
                disabled: platformWriteBlocked,
                run: () => onEdit(record),
              },
            ]
          : []),
        ...(canRead &&
        (!permissionKey || hasPermission(permissions, `${permissionKey}.delete`)) &&
        hasAllowedAction(record.allowedActions, 'delete')
          ? [
              {
                key: 'delete',
                label: localeCode === 'zh_CN' ? '删除' : 'Delete',
                icon: <DeleteOutlined />,
                danger: true,
                disabled: platformWriteBlocked,
                loading:
                  removePending &&
                  removeVariables?.scope.clusterId === targetScope.clusterId &&
                  removeVariables?.name === record.name &&
                  removeVariables.scope.namespace === targetScope.namespace,
                confirm: kubernetesDeleteConfirmation(
                  { name: record.name, namespace: getRecordNamespace?.(record) },
                  localeCode,
                ),
                prepare: () => resetRemove(),
                feedback: removeFeedback,
                run: () => removeAsync({ scope: targetScope, name: record.name }),
              },
            ]
          : []),
      ]
    },
    [
      clusterId,
      localeCode,
      onEdit,
      platformWriteBlocked,
      removePending,
      removeVariables,
      resetRemove,
      removeAsync,
      removeFeedback,
      permissionKey,
      permissions,
      canRead,
      clusterScoped,
      getRecordNamespace,
    ],
  )
  const getKey = useCallback(
    (record: T) => `${getRecordNamespace?.(record) ?? ''}/${record.name}`,
    [getRecordNamespace],
  )
  const resourceActions = useKubernetesResourceActions<T>({
    resourceKey: `storage:${kind}`,
    resourceLabel,
    records: rawItems,
    getName: getResourceName,
    getKey: getKey,
    actions,
  })
  const effectiveColumns = useMemo(
    () => (resourceActions.hasActions ? [...columns, resourceActions.column] : columns),
    [columns, resourceActions.hasActions, resourceActions.column],
  )
  const effectiveEmpty = !clusterId
    ? localeCode === 'zh_CN'
      ? '请选择集群'
      : 'Select a cluster'
    : !canRead
      ? localeCode === 'zh_CN'
        ? '没有权限查看存储资源'
        : 'Permission required to view storage resources'
      : normalizedKeyword && rawItems.length > 0
        ? localeCode === 'zh_CN'
          ? `没有匹配的 ${resourceLabel}`
          : `No matching ${resourceLabel}`
        : emptyLabel[localeCode]
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  const rowProps = useCallback(
    (record: T) => ({
      'data-ai-context': encodeAIContextForElement({
        sourceWorkbench: 'platform',
        entityKind: kind,
        entityName: record.name,
        clusterId: clusterId || undefined,
        namespace: clusterScoped ? undefined : getRecordNamespace?.(record) || undefined,
      }),
    }),
    [clusterId, clusterScoped, getRecordNamespace, kind],
  )

  const empty = useMemo(
    () => (
      <ManagementState
        bordered={false}
        compact
        description={effectiveEmpty}
        kind={clusterId ? 'empty' : 'select-scope'}
      />
    ),
    [clusterId, effectiveEmpty],
  )

  return (
    <>
      {resourceActions.confirmation}
      {!resourceActions.isConfirming && removeMutation.feedback}
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
              value={searchKeyword}
              onChange={setSearchKeyword}
              placeholder={searchPlaceholder[localeCode]}
              inputProps={{
                className: 'soha-platform-compact-field soha-workload-search-input',
                size: 'small',
              }}
            />
          ),
        }}
        tableNode={
          <KubernetesResourceTable
            resourceKey={`storage:${kind}`}
            resourceLabel={resourceLabel}
            allRows={rawItems}
            filterKey={normalizedKeyword}
            scopeMode={clusterScoped ? 'cluster' : 'namespace'}
            error={query.isEnabled === false ? null : query.error}
            hasReadData={canRead && query.data !== undefined}
            onRetry={query.isEnabled === false ? undefined : () => query.refetch()}
            className={'soha-platform-table soha-storage-table'}
            columnSettingIconOnly={true}
            columnSettingPlacement={'header'}
            columns={effectiveColumns}
            dataSource={clusterId ? filteredItems : EMPTY_ITEMS}
            rowKey={rowKey}
            onRow={rowProps}
            loading={query.isLoading}
            localSorting={true}
            tableSize={tableSize}
            scroll={TABLE_SCROLL}
            headerExtra={
              <ManagementTableToolbar>
                <CreateEntry
                  context={{
                    clusterId: clusterId || '',
                    defaultNamespace: clusterScoped ? undefined : namespace || undefined,
                    expectedKind: kind,
                    resourceGroup: 'storage',
                    scopeMode: clusterScoped ? 'cluster' : 'namespace',
                    source: 'list',
                  }}
                  defaultTemplate={createDefaultTemplate}
                  label={resourceLabel}
                />
                <ManagementDensityButton
                  aria-label={densityLabel}
                  title={densityLabel}
                  tooltip={densityLabel}
                  onClick={() =>
                    setTableSize((current) => (current === 'middle' ? 'small' : 'middle'))
                  }
                />
                {autoRefresh ? (
                  <WorkloadRefresh
                    compact
                    disabled={!clusterId || !canRead}
                    settings={refresh}
                    queries={[query]}
                    resourceLabel={resourceLabel}
                    onRefresh={() => query.refetch()}
                  />
                ) : (
                  <ManagementRefreshButton
                    aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                    disabled={!clusterId || !canRead}
                    loading={query.isFetching}
                    tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                    onClick={() => clusterId && canRead && void query.refetch()}
                  />
                )}
              </ManagementTableToolbar>
            }
            empty={empty}
          />
        }
      />
    </>
  )
}
