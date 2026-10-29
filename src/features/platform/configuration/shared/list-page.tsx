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
import { TableCellLink } from '@/components/table-cell-content'
import { hasAllowedAction } from '@/features/auth'
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
import { Popconfirm, Space } from 'antd'
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
}: {
  kind: ConfigurationKind
  name: string
  namespace?: string
}) {
  const navigate = useNavigate()
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
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
}: {
  columns: TableColumnsType<T>
  defaultTemplate?: string
  emptyDescription: { en_US: string; zh_CN: string }
  kind: ConfigurationKind
  label: string
  onEdit?: (record: T) => void
  scopeMode?: ConfigurationScopeMode
  singularLabel?: string
}) {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const scope = toScopeKey(clusterId, scopeMode === 'namespace' ? namespace : null)
  const query = useQuery(configurationQueries.list<T>(kind, scope))
  const visibleQueryData = readableQueryData(query)
  const removeMutation = usePlatformMutation(configurationMutations.remove(kind, queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = removeMutation.blocked
  const rawItems = visibleQueryData ?? []
  const normalizedKeyword = normalizeKeyword(deferredSearchKeyword)
  const filteredItems = useMemo(
    () => rawItems.filter((item) => matchesKeyword(item, normalizedKeyword)),
    [normalizedKeyword, rawItems],
  )
  const effectiveEmptyDescription = !clusterId
    ? localeCode === 'zh_CN'
      ? '请选择集群'
      : 'Select a cluster'
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
  const canShowActions = rawItems.some(
    (item) =>
      hasAllowedAction(item.allowedActions, 'delete') ||
      Boolean(onEdit && hasAllowedAction(item.allowedActions, 'update')),
  )
  const searchDimensions = scopeMode === 'namespace' ? '名称 / 命名空间' : '名称'
  const englishSearchDimensions = scopeMode === 'namespace' ? 'name / namespace' : 'name'

  const actionColumn: TableColumnsType<T>[number] = {
    title: '',
    dataIndex: '__actions',
    fixed: 'right',
    align: 'center',
    width: onEdit ? 84 : 52,
    onHeaderCell: () => ({ className: TABLE_ACTIONS_COLUMN_CLASS_NAME }),
    onCell: () => ({ className: TABLE_ACTIONS_COLUMN_CLASS_NAME }),
    render: (_value, record) => {
      const canEdit = Boolean(onEdit && hasAllowedAction(record.allowedActions, 'update'))
      const canDelete = hasAllowedAction(record.allowedActions, 'delete')
      if (!canEdit && !canDelete) return null
      const target = configurationTargetFromRecord(clusterId, record)
      const deleting =
        removeMutation.isPending &&
        removeMutation.variables?.name === target.name &&
        removeMutation.variables.scope.namespace === target.scope.namespace
      const deleteLabel = localeCode === 'zh_CN' ? '删除' : 'Delete'
      return (
        <Space size={2} className="soha-row-action-icons">
          {canEdit ? (
            <ManagementIconButton
              aria-label={`${localeCode === 'zh_CN' ? '编辑' : 'Edit'} ${record.name}`}
              icon={<FormOutlined />}
              onClick={() => onEdit?.(record)}
              tooltip={localeCode === 'zh_CN' ? '编辑' : 'Edit'}
            />
          ) : null}
          {canDelete ? (
            <Popconfirm
              title={
                localeCode === 'zh_CN' ? `确认删除 ${record.name}？` : `Delete ${record.name}?`
              }
              description={
                localeCode === 'zh_CN'
                  ? '此操作不可恢复，删除后集群资源立即消失。'
                  : 'This deletes the resource immediately and cannot be undone.'
              }
              okText={deleteLabel}
              cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
              okButtonProps={{ disabled: platformWriteBlocked, danger: true, loading: deleting }}
              placement="topRight"
              onConfirm={() => removeMutation.mutate(target, {})}
            >
              <ManagementIconButton
                disabled={platformWriteBlocked}
                danger
                icon={<DeleteOutlined />}
                aria-label={deleteLabel}
                loading={deleting}
                tooltip={deleteLabel}
              />
            </Popconfirm>
          ) : null}
        </Space>
      )
    },
  }

  return (
    <>
      {removeMutation.feedback}
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
                <ManagementRefreshButton
                  aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                  disabled={!clusterId}
                  loading={query.isFetching}
                  tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                  onClick={() => {
                    if (clusterId) void query.refetch()
                  }}
                />
              </ManagementTableToolbar>
            }
            columns={canShowActions ? [...columns, actionColumn] : columns}
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
