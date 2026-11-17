import { ContextFeedback } from '@/components/context-feedback'
import {
  getResourceName,
  getNamespacedResourceKey,
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
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { hasPermission, usePermissionSnapshot, hasAllowedAction } from '@/features/auth'
import { encodeAIContextForElement, useAIPageContext } from '@/features/copilot'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import { CreateEntry } from '@/features/platform/resource-creation/components/create-entry'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'

import { useCallback, useDeferredValue, useMemo, useState } from 'react'
import '../styles.css'
import { accessControlMutations } from './mutations'
import { accessControlQueries } from './queries'
import { accessControlScopeFromSelection, accessControlTargetFromRecord } from './scope'
import type { AccessControlKind, AccessControlResourceRecord } from './types'

const EMPTY_ITEMS: never[] = []
const TABLE_SCROLL = { x: 'max-content' } as const

const ACCESS_CONTROL_ENTITY_KINDS: Record<AccessControlKind, string> = {
  serviceaccounts: 'ServiceAccount',
  roles: 'Role',
  rolebindings: 'RoleBinding',
  clusterroles: 'ClusterRole',
  clusterrolebindings: 'ClusterRoleBinding',
}

function normalizeKeyword(value: string) {
  return value.trim().toLowerCase()
}

function matchesSearchValues(values: Array<string | null | undefined>, keyword: string) {
  if (!keyword) return true
  return values.some((value) => (value ?? '').toLowerCase().includes(keyword))
}

export function AccessControlResourceListPage<T extends AccessControlResourceRecord>({
  columns,
  permissionKey,
  onEdit,
  autoRefresh = false,
  defaultTemplate,
  emptyDescription,
  kind,
  label,
  rowKey,
  searchValues,
}: {
  permissionKey?: string
  onEdit?: (record: T) => void
  autoRefresh?: boolean
  columns: TableColumnsType<T>
  defaultTemplate: string
  emptyDescription: { en_US: string; zh_CN: string }
  kind: AccessControlKind
  label: string
  rowKey: string | ((record: T) => string)
  searchValues: (record: T) => Array<string | null | undefined>
}) {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const yamlCapability = useClusterCapability('resource.yaml.apply', localeCode)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const scope = accessControlScopeFromSelection(kind, clusterId, namespace)
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = !permissionKey || hasPermission(permissions, `${permissionKey}.view`)
  const refresh = useWorkloadRefresh()
  const options = accessControlQueries.list<T>(kind, scope)
  const query = useQuery({
    ...options,
    enabled: canRead && options.enabled,
    ...(autoRefresh ? { refetchInterval: refresh.interval } : {}),
  })
  const visibleQueryData = readableQueryData(query)
  const removeMutation = usePlatformMutation(accessControlMutations.remove(kind, queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = removeMutation.blocked
  const rawItems = canRead ? (visibleQueryData ?? EMPTY_ITEMS) : EMPTY_ITEMS
  const normalizedKeyword = normalizeKeyword(deferredSearchKeyword)
  const filteredItems = useMemo(
    () => rawItems.filter((item) => matchesSearchValues(searchValues(item), normalizedKeyword)),
    [normalizedKeyword, rawItems, searchValues],
  )
  const entityKind = ACCESS_CONTROL_ENTITY_KINDS[kind]
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: label,
    entityKind,
    clusterId: clusterId || undefined,
    namespace: scope.namespace || undefined,
    visibleFilters: { keyword: searchKeyword || undefined },
    pinnedData: { itemCount: rawItems.length },
  })
  const effectiveEmptyDescription = !canRead
    ? localeCode === 'zh_CN'
      ? '没有查看权限'
      : 'Permission required'
    : !clusterId
      ? localeCode === 'zh_CN'
        ? '请选择集群查看 RBAC 资源。'
        : 'Select a cluster to inspect RBAC resources.'
      : normalizedKeyword && rawItems.length > 0
        ? localeCode === 'zh_CN'
          ? `没有匹配的 ${label}`
          : `No matching ${label}`
        : emptyDescription[localeCode]
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const {
    isPending: removePending,
    variables: removeVariables,
    reset: resetRemove,
    mutateAsync: removeAsync,
    feedback: removeFeedback,
  } = removeMutation
  const actions = useCallback<(record: T) => KubernetesResourceAction[]>(
    (record) => {
      const target = accessControlTargetFromRecord(kind, clusterId, record)
      const deleteActions =
        canRead &&
        (!permissionKey || hasPermission(permissions, `${permissionKey}.delete`)) &&
        hasAllowedAction(record.allowedActions, 'delete')
          ? [
              {
                key: 'delete',
                label: localeCode === 'zh_CN' ? '删除' : 'Delete',
                icon: <DeleteOutlined />,
                danger: true,
                disabled:
                  platformWriteBlocked || yamlCapability.isLoading || yamlCapability.disabled,
                reason: yamlCapability.reason,
                loading:
                  removePending &&
                  removeVariables?.scope.clusterId === clusterId &&
                  removeVariables?.name === target.name &&
                  removeVariables.scope.namespace === target.scope.namespace,
                confirm: kubernetesDeleteConfirmation(record, localeCode),
                prepare: () => resetRemove(),
                feedback: removeFeedback,
                run: () => removeAsync(target),
              },
            ]
          : []
      return [
        ...(onEdit &&
        canRead &&
        (!permissionKey || hasPermission(permissions, `${permissionKey}.update`)) &&
        hasAllowedAction(record.allowedActions, 'update')
          ? [
              {
                key: 'edit',
                label: localeCode === 'zh_CN' ? '编辑 YAML' : 'Edit YAML',
                disabled: platformWriteBlocked,
                run: () => onEdit(record),
              },
            ]
          : []),
        ...deleteActions,
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
      kind,
      yamlCapability.isLoading,
      yamlCapability.disabled,
      yamlCapability.reason,
    ],
  )
  const resourceActions = useKubernetesResourceActions<T>({
    resourceKey: `rbac:${kind}`,
    resourceLabel: entityKind,
    records: rawItems,
    getName: getResourceName,
    getKey: getNamespacedResourceKey,
    actions,
  })

  const effectiveColumns = useMemo(
    () => (resourceActions.hasActions ? [...columns, resourceActions.column] : columns),
    [columns, resourceActions.hasActions, resourceActions.column],
  )
  const rowProps = useCallback(
    (record: T) => ({
      'data-ai-context': encodeAIContextForElement({
        sourceWorkbench: 'platform',
        entityKind,
        entityName: record.name,
        clusterId: clusterId || undefined,
        namespace: record.namespace || undefined,
      }),
    }),
    [clusterId, entityKind],
  )
  const empty = useMemo(
    () => (
      <ManagementState
        bordered={false}
        compact
        description={effectiveEmptyDescription}
        kind={!clusterId ? 'select-scope' : 'empty'}
      />
    ),
    [clusterId, effectiveEmptyDescription],
  )

  return (
    <>
      {resourceActions.confirmation}
      {!resourceActions.isConfirming && removeMutation.feedback}
      <ManagementDataPage
        beforeQuery={
          <>
            {yamlCapability.reason ? (
              <ContextFeedback
                description={yamlCapability.reason}
                showIcon
                style={{ marginBottom: 12 }}
                title={localeCode === 'zh_CN' ? 'YAML 操作能力' : 'YAML operation capability'}
                type={yamlCapability.disabled ? 'warning' : 'info'}
              />
            ) : null}
          </>
        }
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
              inputProps={{
                className: 'soha-platform-compact-field soha-workload-search-input',
                size: 'small',
              }}
              label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
              onChange={setSearchKeyword}
              placeholder={localeCode === 'zh_CN' ? `搜索 ${label}` : `Search ${label}`}
              value={searchKeyword}
            />
          ),
        }}
        tableNode={
          <KubernetesResourceTable
            resourceKey={`rbac:${kind}`}
            resourceLabel={label}
            allRows={rawItems}
            filterKey={normalizedKeyword}
            scopeMode={kind.startsWith('cluster') ? 'cluster' : 'namespace'}
            error={query.isEnabled === false ? null : query.error}
            hasReadData={query.data !== undefined}
            onRetry={query.isEnabled === false ? undefined : () => query.refetch()}
            className={'soha-rbac-table soha-platform-table'}
            columnSettingIconOnly={true}
            columnSettingPlacement={'header'}
            columns={effectiveColumns}
            dataSource={clusterId ? filteredItems : EMPTY_ITEMS}
            empty={empty}
            headerExtra={
              <ManagementTableToolbar>
                <CreateEntry
                  context={{
                    clusterId: clusterId || '',
                    defaultNamespace:
                      kind === 'roles' || kind === 'rolebindings' || kind === 'serviceaccounts'
                        ? namespace || undefined
                        : undefined,
                    expectedApiVersion:
                      kind === 'serviceaccounts' ? 'v1' : 'rbac.authorization.k8s.io/v1',
                    expectedKind: {
                      serviceaccounts: 'ServiceAccount',
                      roles: 'Role',
                      rolebindings: 'RoleBinding',
                      clusterroles: 'ClusterRole',
                      clusterrolebindings: 'ClusterRoleBinding',
                    }[kind],
                    resourceGroup: 'access-control',
                    scopeMode:
                      kind === 'clusterroles' || kind === 'clusterrolebindings'
                        ? 'cluster'
                        : 'namespace',
                    source: 'list',
                  }}
                  defaultTemplate={defaultTemplate}
                  label={label.replace(/s$/, '')}
                />
                <ManagementDensityButton
                  aria-label={densityLabel}
                  onClick={() =>
                    setTableSize((current) => (current === 'middle' ? 'small' : 'middle'))
                  }
                  title={densityLabel}
                  tooltip={densityLabel}
                />
                {autoRefresh ? (
                  <WorkloadRefresh
                    compact
                    settings={refresh}
                    queries={[query]}
                    disabled={!clusterId || !canRead}
                    resourceLabel={label}
                    onRefresh={() => query.refetch()}
                  />
                ) : (
                  <ManagementRefreshButton
                    aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                    disabled={!clusterId}
                    loading={query.isFetching}
                    onClick={() => {
                      if (clusterId) void query.refetch()
                    }}
                    tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                  />
                )}
              </ManagementTableToolbar>
            }
            loading={query.isLoading}
            localSorting={true}
            rowKey={rowKey}
            onRow={rowProps}
            scroll={TABLE_SCROLL}
            tableSize={tableSize}
          />
        }
      />
    </>
  )
}
