import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { TABLE_ACTIONS_COLUMN_CLASS_NAME } from '@/components/resource-actions'
import { hasAllowedAction } from '@/features/auth'
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
import { Alert, Popconfirm } from 'antd'
import { useDeferredValue, useMemo, useState } from 'react'
import '../styles.css'
import { accessControlMutations } from './mutations'
import { accessControlQueries } from './queries'
import { accessControlScopeFromSelection, accessControlTargetFromRecord } from './scope'
import type { AccessControlKind, AccessControlResourceRecord } from './types'

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
  defaultTemplate,
  emptyDescription,
  kind,
  label,
  rowKey,
  searchValues,
}: {
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
  const query = useQuery(accessControlQueries.list<T>(kind, scope))
  const visibleQueryData = readableQueryData(query)
  const removeMutation = usePlatformMutation(accessControlMutations.remove(kind, queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = removeMutation.blocked
  const rawItems = visibleQueryData ?? []
  const normalizedKeyword = normalizeKeyword(deferredSearchKeyword)
  const filteredItems = useMemo(
    () => rawItems.filter((item) => matchesSearchValues(searchValues(item), normalizedKeyword)),
    [normalizedKeyword, rawItems, searchValues],
  )
  const actionsAvailable = !yamlCapability.isLoading && !yamlCapability.disabled
  const shouldShowActions =
    actionsAvailable && rawItems.some((item) => hasAllowedAction(item.allowedActions, 'delete'))
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
  const effectiveEmptyDescription = !clusterId
    ? localeCode === 'zh_CN'
      ? '请选择集群查看 RBAC 资源。'
      : 'Select a cluster to inspect RBAC resources.'
    : normalizedKeyword && rawItems.length > 0
      ? localeCode === 'zh_CN'
        ? `没有匹配的 ${label}`
        : `No matching ${label}`
      : emptyDescription[localeCode]
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const deleteLabel = localeCode === 'zh_CN' ? '删除' : 'Delete'
  const actionColumn: TableColumnsType<T>[number] = {
    title: '',
    dataIndex: '__actions',
    fixed: 'right',
    align: 'center',
    width: 52,
    onHeaderCell: () => ({ className: TABLE_ACTIONS_COLUMN_CLASS_NAME }),
    onCell: () => ({ className: TABLE_ACTIONS_COLUMN_CLASS_NAME }),
    render: (_value, record) => {
      if (!hasAllowedAction(record.allowedActions, 'delete')) return null
      const target = accessControlTargetFromRecord(kind, clusterId, record)
      const deleting =
        removeMutation.isPending &&
        removeMutation.variables?.name === target.name &&
        removeMutation.variables.scope.namespace === target.scope.namespace
      return (
        <Popconfirm
          title={localeCode === 'zh_CN' ? `确认删除 ${record.name}？` : `Delete ${record.name}?`}
          cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
          description={
            localeCode === 'zh_CN'
              ? '此操作不可恢复，删除后集群资源立即消失。'
              : 'This deletes the resource immediately and cannot be undone.'
          }
          okButtonProps={{ disabled: platformWriteBlocked, danger: true, loading: deleting }}
          okText={deleteLabel}
          onConfirm={() => removeMutation.mutate(target, {})}
          placement="topRight"
        >
          <ManagementIconButton
            disabled={platformWriteBlocked}
            danger
            aria-label={deleteLabel}
            icon={<DeleteOutlined />}
            loading={deleting}
            tooltip={deleteLabel}
          />
        </Popconfirm>
      )
    },
  }

  return (
    <>
      {removeMutation.feedback}
      <ManagementDataPage
        beforeQuery={
          <>
            {yamlCapability.reason ? (
              <Alert
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
            columns={shouldShowActions ? [...columns, actionColumn] : columns}
            dataSource={clusterId ? filteredItems : []}
            empty={
              <ManagementState
                bordered={false}
                compact
                description={effectiveEmptyDescription}
                kind={!clusterId ? 'select-scope' : 'empty'}
              />
            }
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
                <ManagementRefreshButton
                  aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                  disabled={!clusterId}
                  loading={query.isFetching}
                  onClick={() => {
                    if (clusterId) void query.refetch()
                  }}
                  tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                />
              </ManagementTableToolbar>
            }
            loading={query.isLoading}
            localSorting={true}
            rowKey={rowKey}
            onRow={(record: T) => ({
              'data-ai-context': encodeAIContextForElement({
                sourceWorkbench: 'platform',
                entityKind,
                entityName: record.name,
                clusterId: clusterId || undefined,
                namespace: record.namespace || undefined,
              }),
            })}
            scroll={{ x: 'max-content' }}
            tableSize={tableSize}
          />
        }
      />
    </>
  )
}
