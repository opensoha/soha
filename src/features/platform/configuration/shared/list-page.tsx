import {
  kubernetesDeleteConfirmation,
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
import { TableCellLink } from '@/components/table-cell-content'
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
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined, FormOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useDeferredValue, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import '../styles.css'
import { configurationMutations } from './mutations'
import { configurationQueries } from './queries'
import { configurationTargetFromRecord } from './scope'
import type {
  ConfigurationKind,
  ConfigurationResourceRecord,
  ConfigurationScopeMode,
} from './types'

const CONFIGURATION_ENTITY_KINDS: Record<ConfigurationKind, string> = {
  configmaps: 'ConfigMap',
  secrets: 'Secret',
  resourcequotas: 'ResourceQuota',
  limitranges: 'LimitRange',
  hpas: 'HorizontalPodAutoscaler',
  poddisruptionbudgets: 'PodDisruptionBudget',
  priorityclasses: 'PriorityClass',
  runtimeclasses: 'RuntimeClass',
  leases: 'Lease',
  mutatingwebhookconfigurations: 'MutatingWebhookConfiguration',
  validatingwebhookconfigurations: 'ValidatingWebhookConfiguration',
}

function normalizeKeyword(value: string) {
  return value.trim().toLowerCase()
}

function matchesKeyword(record: ConfigurationResourceRecord, keyword: string) {
  if (!keyword) return true
  return [record.name, record.namespace ?? ''].some((value) =>
    value.toLowerCase().includes(keyword),
  )
}

export function ConfigurationNameLink({
  kind,
  name,
  namespace,
  includeCluster = false,
}: {
  includeCluster?: boolean
  kind: ConfigurationKind
  name: string
  namespace?: string
}) {
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const params = new URLSearchParams()
  if (namespace) params.set('namespace', namespace)
  if (includeCluster && clusterId) params.set('clusterId', clusterId)
  const query = params.size ? `?${params}` : ''
  return (
    <TableCellLink
      label={name}
      onClick={() => navigate(`/configuration/${kind}/${encodeURIComponent(name)}${query}`)}
    />
  )
}

export function ConfigurationResourceListPage<T extends ConfigurationResourceRecord>({
  columns,
  defaultTemplate,
  emptyDescription,
  kind,
  label,
  onEdit,
  scopeMode = 'namespace',
  singularLabel,
  permissionKey,
  autoRefresh = false,
}: {
  columns: TableColumnsType<T>
  defaultTemplate?: string
  emptyDescription: { en_US: string; zh_CN: string }
  kind: ConfigurationKind
  label: string
  onEdit?: (record: T) => void
  scopeMode?: ConfigurationScopeMode
  singularLabel?: string
  permissionKey?: string
  autoRefresh?: boolean
}) {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const scope = toScopeKey(clusterId, scopeMode === 'namespace' ? namespace : null)
  const refresh = useWorkloadRefresh()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = !permissionKey || hasPermission(permissions, `${permissionKey}.view`)
  const options = configurationQueries.list<T>(kind, scope)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: autoRefresh ? refresh.interval : false,
  })
  const visibleQueryData = readableQueryData(query)
  const removeMutation = usePlatformMutation(configurationMutations.remove(kind, queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = removeMutation.blocked
  const rawItems = canRead ? (visibleQueryData ?? []) : []
  const normalizedKeyword = normalizeKeyword(deferredSearchKeyword)
  const filteredItems = useMemo(
    () => rawItems.filter((item) => matchesKeyword(item, normalizedKeyword)),
    [normalizedKeyword, rawItems],
  )
  const effectiveEmptyDescription = !clusterId
    ? localeCode === 'zh_CN'
      ? '请选择集群'
      : 'Select a cluster'
    : !canRead
      ? localeCode === 'zh_CN'
        ? '没有权限查看配置资源'
        : 'Permission required to view resources'
      : normalizedKeyword && rawItems.length > 0
        ? localeCode === 'zh_CN'
          ? `没有匹配的 ${label}`
          : `No matching ${label}`
        : emptyDescription[localeCode]
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const canCreate = Boolean(defaultTemplate && singularLabel)
  const entityKind = singularLabel || CONFIGURATION_ENTITY_KINDS[kind]
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: label,
    entityKind,
    clusterId: clusterId || undefined,
    namespace: scopeMode === 'namespace' ? namespace || undefined : undefined,
    visibleFilters: { keyword: searchKeyword || undefined },
    pinnedData: { itemCount: rawItems.length },
  })
  const searchDimensions = scopeMode === 'namespace' ? '名称 / 命名空间' : '名称'
  const englishSearchDimensions = scopeMode === 'namespace' ? 'name / namespace' : 'name'

  const resourceActions = useKubernetesResourceActions<T>({
    resourceKey: `configuration:${kind}`,
    scopeKey: JSON.stringify([clusterId, namespace, kind]),
    resourceLabel: entityKind,
    records: rawItems,
    getName: (record) => record.name,
    getKey: (record) => `${record.namespace ?? ''}/${record.name}`,
    actions: (record) => {
      const target = configurationTargetFromRecord(clusterId, record)
      return [
        ...(onEdit &&
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
        ...((!permissionKey || hasPermission(permissions, `${permissionKey}.delete`)) &&
        hasAllowedAction(record.allowedActions, 'delete')
          ? [
              {
                key: 'delete',
                label: localeCode === 'zh_CN' ? '删除' : 'Delete',
                icon: <DeleteOutlined />,
                danger: true,
                disabled: platformWriteBlocked,
                loading:
                  removeMutation.isPending &&
                  removeMutation.variables?.scope.clusterId === clusterId &&
                  removeMutation.variables?.name === target.name &&
                  removeMutation.variables.scope.namespace === target.scope.namespace,
                confirm: kubernetesDeleteConfirmation(record, localeCode),
                prepare: () => removeMutation.reset(),
                feedback: removeMutation.feedback,
                run: () => removeMutation.mutateAsync(target),
              },
            ]
          : []),
      ]
    },
  })

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
              resetLabel={localeCode === 'zh_CN' ? '重置' : 'Reset'}
              submitLabel={localeCode === 'zh_CN' ? '查询' : 'Search'}
              onReset={() => setSearchKeyword('')}
            />
          ),
          children: (
            <ManagementKeywordField
              label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
              value={searchKeyword}
              onChange={setSearchKeyword}
              placeholder={
                localeCode === 'zh_CN'
                  ? `搜索 ${label} ${searchDimensions}`
                  : `Search ${label} ${englishSearchDimensions}`
              }
              inputProps={{
                className: 'soha-platform-compact-field soha-workload-search-input',
                size: 'small',
              }}
            />
          ),
        }}
        tableNode={
          <KubernetesResourceTable
            resourceKey={`configuration:${kind}`}
            resourceLabel={label}
            allRows={rawItems}
            filterKey={normalizedKeyword}
            scopeMode={scopeMode}
            error={query.isEnabled === false ? null : query.error}
            hasReadData={query.data !== undefined}
            onRetry={query.isEnabled === false ? undefined : () => query.refetch()}
            className={'soha-platform-table'}
            columnSettingIconOnly={true}
            columnSettingPlacement={'header'}
            headerExtra={
              <ManagementTableToolbar>
                {canCreate ? (
                  <CreateEntry
                    context={{
                      clusterId: clusterId || '',
                      defaultNamespace:
                        scopeMode === 'namespace' ? namespace || undefined : undefined,
                      expectedKind: singularLabel,
                      resourceGroup: 'configuration',
                      scopeMode,
                      source: 'list',
                    }}
                    defaultTemplate={defaultTemplate || ''}
                    label={singularLabel || label}
                  />
                ) : null}
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
                    resourceLabel={label}
                    onRefresh={() => query.refetch()}
                  />
                ) : (
                  <ManagementRefreshButton
                    aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                    disabled={!clusterId}
                    loading={query.isFetching}
                    tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                    onClick={() => {
                      if (clusterId) void query.refetch()
                    }}
                  />
                )}
              </ManagementTableToolbar>
            }
            columns={resourceActions.hasActions ? [...columns, resourceActions.column] : columns}
            dataSource={clusterId ? filteredItems : []}
            rowKey={(record) => `${record.namespace ?? ''}/${record.name}`}
            onRow={(record: T) => ({
              'data-ai-context': encodeAIContextForElement({
                sourceWorkbench: 'platform',
                entityKind,
                entityName: record.name,
                clusterId: clusterId || undefined,
                namespace: record.namespace || undefined,
              }),
            })}
            loading={query.isLoading}
            localSorting={true}
            empty={
              <ManagementState
                bordered={false}
                compact
                description={effectiveEmptyDescription}
                kind={!clusterId ? 'select-scope' : 'empty'}
              />
            }
            tableSize={tableSize}
            scroll={{ x: 'max-content' }}
          />
        }
      />
    </>
  )
}
