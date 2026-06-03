import { useDeferredValue, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { App, Button, Input, InputNumber, Modal, Popconfirm, Popover, Progress, Select, Space, Tag, Tooltip, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined, SearchOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDensityButton,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementState,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { useResourceActions } from '@/components/resource-actions'
import { BooleanTag } from '@/components/status-tag'
import { hasAllowedAction } from '@/features/auth/permission-snapshot'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import {
  CONFIGMAP_DEFAULT_TEMPLATE,
  CreateResourceModal,
  SECRET_DEFAULT_TEMPLATE,
} from '@/features/platform/configuration-detail-pages'
import {
  CLUSTER_ROLE_BINDING_DEFAULT_TEMPLATE,
  CLUSTER_ROLE_DEFAULT_TEMPLATE,
  ROLE_BINDING_DEFAULT_TEMPLATE,
  ROLE_DEFAULT_TEMPLATE,
  SERVICE_ACCOUNT_DEFAULT_TEMPLATE,
  buildClusterSelectionDescription,
  buildDefaultResourceSearchValues,
  buildNamespaceQuery,
  buildRBACDetailPath,
  buildRBACErrorMessage,
  buildRBACRefreshLabel,
  buildRBACSearchEmptyDescription,
  buildRBACSearchPlaceholder,
  buildRequestErrorDescription,
  buildResourceSearchEmptyDescription,
  buildResourceSearchPlaceholder,
  buildWorkloadReplicaErrorDescription,
  buildWorkloadReplicaSearchEmptyDescription,
  includesSearch,
  localize,
  normalizeSearchKeyword,
  parseQuotaNumeric,
  parseRBACSubject,
} from '@/features/platform/platform-management-model'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, Cluster } from '@/types'
import type {
  ClusterRoleBindingResource,
  ClusterRoleResource,
  ConfigMapResource,
  EndpointSliceResource,
  HorizontalPodAutoscalerResource,
  IngressClassResource,
  LeaseResource,
  LimitRangeResource,
  LocalizedCopy,
  MutatingWebhookConfigurationResource,
  NetworkPolicyResource,
  PodDisruptionBudgetResource,
  PortForwardSession,
  PriorityClassResource,
  RBACActionConfig,
  RBACCreateConfig,
  ReplicaSetResource,
  ReplicationControllerResource,
  ResourceQuotaResource,
  RoleBindingResource,
  RoleResource,
  RuntimeClassResource,
  SecretResource,
  ServiceAccountResource,
  ValidatingWebhookConfigurationResource,
  WorkloadReplicaActionConfig,
} from '@/features/platform/platform-management-model'
import type { TableColumnsType } from 'antd'
import './platform-pages.css'

const { Text } = Typography

function PlatformTableState({ description, kind }: { description: ReactNode; kind?: 'empty' | 'error' | 'select-scope' }) {
  return <ManagementState bordered={false} compact description={description} kind={kind ?? 'empty'} />
}

function buildTableStateKind(clusterId: string | null | undefined, description: string): 'empty' | 'select-scope' {
  return !clusterId || description.includes('请选择') || description.includes('Select a cluster') ? 'select-scope' : 'empty'
}




function roleRefTag(value?: string) {
  if (!value) {
    return <Text type="secondary">-</Text>
  }
  return <Text code className="soha-rbac-role-ref">{value}</Text>
}

function renderRBACSubjectChips(subjects: string[] | undefined, emptyLabel: string) {
  if (!subjects || subjects.length === 0) {
    return <Text type="secondary">{emptyLabel}</Text>
  }

  const preview = subjects.slice(0, 2).map(parseRBACSubject)
  const overflow = subjects.slice(2).map(parseRBACSubject)

  return (
    <div className="soha-rbac-subject-list">
      {preview.map((subject) => (
        <Tag key={subject.label} className="soha-rbac-subject-chip">
          {subject.label}
        </Tag>
      ))}
      {overflow.length > 0 ? (
        <Popover
          placement="topLeft"
          content={(
            <div className="soha-rbac-subject-popover">
              {overflow.map((subject) => (
                <Tag key={subject.label} className="soha-rbac-subject-chip">
                  {subject.label}
                </Tag>
              ))}
            </div>
          )}
        >
          <Tag className="soha-rbac-subject-chip">{`+${overflow.length}`}</Tag>
        </Popover>
      ) : null}
    </div>
  )
}

function useRBACActionColumn<T extends Record<string, any>>(resourcePath: string, actionConfig?: RBACActionConfig<T>) {
  return useResourceActions<T>({
    resourcePath,
    resourceKind: actionConfig?.resourceKind ?? 'Resource',
    getName: actionConfig?.getName ?? (() => ''),
    getNamespace: actionConfig?.getNamespace,
    canDelete: actionConfig?.canDelete,
  })
}

function useScopedResourceQuery<T>(resourcePath: string) {
  const { clusterId, namespace } = usePlatformScopeStore()
  return useQuery({
    queryKey: ['platform-resource', resourcePath, clusterId, namespace],
    queryFn: () => api.get<ApiResponse<T[]>>(buildClusterScopedPath(clusterId!, resourcePath, namespace)),
    enabled: !!clusterId,
  })
}

function ResourceTableCard<T extends Record<string, any>>({
  columns,
  headerExtra,
  emptyDescription,
  resourcePath,
  rowKey,
  searchPlaceholder,
  searchValues = buildDefaultResourceSearchValues,
  title,
  actionConfig,
}: {
  columns: TableColumnsType<T>
  headerExtra?: ReactNode
  emptyDescription: LocalizedCopy
  resourcePath: string
  rowKey: string | ((record: T) => string)
  searchPlaceholder?: LocalizedCopy
  searchValues?: (record: T) => Array<string | undefined | null>
  title: LocalizedCopy
  actionConfig?: {
    resourceKind: string
    resourceLabel?: string
    getName: (record: T) => string
    getNamespace?: (record: T) => string | undefined
  }
}) {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const query = useScopedResourceQuery<T>(resourcePath)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const rawItems = query.data?.data ?? []
  const filteredItems = useMemo(
    () => rawItems.filter((item) => includesSearch(searchValues(item), normalizedKeyword)),
    [normalizedKeyword, rawItems, searchValues],
  )

  const { column: actionColumn, modalNode } = useResourceActions<T>({
    resourcePath,
    resourceKind: actionConfig?.resourceKind ?? 'Resource',
    resourceLabel: actionConfig?.resourceLabel,
    getName: actionConfig?.getName ?? (() => ''),
    getNamespace: actionConfig?.getNamespace,
  })

  const effectiveColumns = actionConfig ? [...columns, actionColumn] : columns
  const scroll = actionConfig ? { x: 'max-content' as const } : undefined
  const effectiveEmptyDescription = !clusterId
    ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')
    : normalizedKeyword && rawItems.length > 0
      ? localize(localeCode, buildResourceSearchEmptyDescription(title))
    : localize(localeCode, emptyDescription)
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  return (
    <>
      {actionConfig ? modalNode : null}
      <ManagementQueryPanel
        onFinish={() => undefined}
        actions={(
          <>
            <Button autoInsertSpace={false} disabled={!searchKeyword.trim()} htmlType="button" onClick={() => setSearchKeyword('')}>
              {localeCode === 'zh_CN' ? '重置' : 'Reset'}
            </Button>
            <Button autoInsertSpace={false} htmlType="submit" type="primary">
              {localeCode === 'zh_CN' ? '查询' : 'Search'}
            </Button>
          </>
        )}
      >
        <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
          <Input
            allowClear
            className="soha-platform-compact-field soha-workload-search-input"
            prefix={<SearchOutlined />}
            size="small"
            value={searchKeyword}
            variant="filled"
            onChange={(event) => setSearchKeyword(event.target.value)}
            placeholder={localize(localeCode, searchPlaceholder ?? buildResourceSearchPlaceholder(title))}
          />
        </ManagementQueryField>
      </ManagementQueryPanel>
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={(
          <ManagementTableToolbar>
            {headerExtra}
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              disabled={!clusterId}
              loading={query.isFetching}
              tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onClick={() => {
                if (clusterId) {
                  void query.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        columns={effectiveColumns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={rowKey}
        loading={query.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        empty={<PlatformTableState description={effectiveEmptyDescription} kind={buildTableStateKind(clusterId, effectiveEmptyDescription)} />}
        pageSize={10}
        tableSize={tableSize}
        scroll={scroll ?? { x: 'max-content' }}
      />
    </>
  )
}

function ResourceListPage<T extends Record<string, any>>({
  columns,
  headerExtra,
  emptyDescription,
  resourcePath,
  rowKey,
  title,
  actionConfig,
}: {
  columns: TableColumnsType<T>
  headerExtra?: ReactNode
  emptyDescription: LocalizedCopy
  resourcePath: string
  rowKey: string | ((record: T) => string)
  title: LocalizedCopy
  actionConfig?: {
    resourceKind: string
    resourceLabel?: string
    getName: (record: T) => string
    getNamespace?: (record: T) => string | undefined
  }
}) {
  return (
    <div className="soha-page">
      <ResourceTableCard<T>
        columns={columns}
        headerExtra={headerExtra}
        resourcePath={resourcePath}
        rowKey={rowKey}
        title={title}
        emptyDescription={emptyDescription}
        actionConfig={actionConfig}
      />
    </div>
  )
}

function WorkloadReplicaTableEmpty({
  clusterId,
  emptyDescription,
  filteredCount,
  localeCode,
  title,
  totalCount,
}: {
  clusterId?: string | null
  emptyDescription: LocalizedCopy
  filteredCount: number
  localeCode: 'zh_CN' | 'en_US'
  title: LocalizedCopy
  totalCount: number
}) {
  const hasFilterMiss = totalCount > 0 && filteredCount === 0
  const resolvedTitle = !clusterId
    ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')
    : hasFilterMiss
      ? localize(localeCode, buildWorkloadReplicaSearchEmptyDescription(title))
      : localize(localeCode, emptyDescription)
  const description = !clusterId
    ? (localeCode === 'zh_CN' ? '在顶部作用域选择集群后查看工作负载资源。' : 'Select a cluster in the header scope controls to inspect workload resources.')
    : hasFilterMiss
      ? (localeCode === 'zh_CN' ? '调整搜索条件后重试。' : 'Adjust the search term and try again.')
      : (localeCode === 'zh_CN' ? '当前集群和命名空间范围内没有可展示的记录。' : 'No records are available for the selected cluster and namespace scope.')

  return (
    <ManagementState
      bordered={false}
      compact
      description={description}
      kind={!clusterId ? 'select-scope' : 'empty'}
      title={resolvedTitle}
    />
  )
}

function renderReplicaReadyCell(ready: number | undefined, desired: number | undefined) {
  const readyCount = Math.max(0, Number.isFinite(ready) ? Number(ready) : 0)
  const desiredCount = Math.max(0, Number.isFinite(desired) ? Number(desired) : 0)
  const percent = desiredCount > 0 ? Math.min(100, Math.round((readyCount / desiredCount) * 100)) : 0
  const isComplete = desiredCount === 0 || readyCount >= desiredCount

  return (
    <div className="soha-replica-progress-cell">
      <Progress percent={percent} showInfo={false} size="small" status={isComplete ? 'success' : 'active'} />
      <Text type="secondary">{`${readyCount}/${desiredCount}`}</Text>
    </div>
  )
}

function renderWorkloadNameText(value: string) {
  return (
    <Tooltip title={value} placement="topLeft">
      <Text strong className="soha-workload-name-text">
        {value}
      </Text>
    </Tooltip>
  )
}

function WorkloadReplicaListPage<T extends { allowedActions?: string[] }>({
  actionConfig,
  columns,
  emptyDescription,
  resourcePath,
  rowKey,
  searchPlaceholder,
  searchValues,
  title,
}: {
  actionConfig?: WorkloadReplicaActionConfig<T>
  columns: TableColumnsType<T>
  emptyDescription: LocalizedCopy
  resourcePath: string
  rowKey: string | ((record: T) => string)
  searchPlaceholder: LocalizedCopy
  searchValues: (record: T) => Array<string | undefined | null>
  title: LocalizedCopy
}) {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const query = useScopedResourceQuery<T>(resourcePath)
  const rawItems = query.data?.data ?? []
  const filteredItems = useMemo(
    () => rawItems.filter((item) => includesSearch(searchValues(item), normalizedKeyword)),
    [normalizedKeyword, rawItems, searchValues],
  )
  const canDelete = (record: T) => hasAllowedAction(record.allowedActions, 'delete')
  const shouldShowActions = Boolean(actionConfig && rawItems.some((item) => canDelete(item)))
  const { column: actionColumn, modalNode } = useResourceActions<T>({
    resourcePath,
    resourceKind: actionConfig?.resourceKind ?? 'Resource',
    getName: actionConfig?.getName ?? (() => ''),
    getNamespace: actionConfig?.getNamespace,
    canDelete,
    listInvalidationKey: ['platform-resource', resourcePath, clusterId, namespace],
  })
  const effectiveColumns = shouldShowActions ? [...columns, actionColumn] : columns
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const replicaQueryPanel = (
    <ManagementQueryPanel
      onFinish={() => undefined}
      actions={(
        <>
          <Button autoInsertSpace={false} disabled={!searchKeyword.trim()} htmlType="button" onClick={() => setSearchKeyword('')}>
            {localeCode === 'zh_CN' ? '重置' : 'Reset'}
          </Button>
          <Button autoInsertSpace={false} htmlType="submit" type="primary">
            {localeCode === 'zh_CN' ? '查询' : 'Search'}
          </Button>
        </>
      )}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
        <Input
          allowClear
          className="soha-platform-compact-field soha-workload-search-input"
          prefix={<SearchOutlined />}
          size="small"
          value={searchKeyword}
          variant="filled"
          onChange={(event) => setSearchKeyword(event.target.value)}
          placeholder={localize(localeCode, searchPlaceholder)}
        />
      </ManagementQueryField>
    </ManagementQueryPanel>
  )

  return (
    <div className="soha-page">
      {shouldShowActions ? modalNode : null}
      {query.isError ? (
        <ManagementState
          className="mb-3"
          description={buildWorkloadReplicaErrorDescription(localeCode, query.error)}
          kind="error"
          title={localeCode === 'zh_CN' ? '工作负载资源暂时不可用' : 'Workload resources unavailable'}
        />
      ) : null}
      {replicaQueryPanel}
      <AdminTable
        className="soha-workload-replica-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={effectiveColumns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={rowKey}
        loading={query.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        pageSize={10}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={(
          <ManagementTableToolbar>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              disabled={!clusterId}
              loading={query.isFetching}
              tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onClick={() => {
                if (clusterId) {
                  void query.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={(
          <WorkloadReplicaTableEmpty
            clusterId={clusterId}
            emptyDescription={emptyDescription}
            filteredCount={filteredItems.length}
            localeCode={localeCode}
            title={title}
            totalCount={rawItems.length}
          />
        )}
      />
    </div>
  )
}

function ResourceNameLink({ to, name }: { to: string; name: string }) {
  const navigate = useNavigate()
  return (
    <Button type="text" onClick={() => navigate(to)}>{name}</Button>
  )
}

function RBACListPage<T extends { allowedActions?: string[] }>({
  actionConfig,
  createConfig,
  columns,
  emptyDescription,
  resourcePath,
  rowKey,
  searchPlaceholder,
  searchValues,
  title,
}: {
  actionConfig?: RBACActionConfig<T>
  createConfig?: RBACCreateConfig
  columns: TableColumnsType<T>
  emptyDescription: LocalizedCopy
  resourcePath: string
  rowKey: string | ((record: T) => string)
  searchPlaceholder?: LocalizedCopy
  searchValues: (record: T) => Array<string | undefined | null>
  title: LocalizedCopy
}) {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const query = useScopedResourceQuery<T>(resourcePath)
  const clustersQuery = useQuery({
    queryKey: ['clusters'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
    enabled: !!clusterId,
  })
  const [searchKeyword, setSearchKeyword] = useState('')
  const [createVisible, setCreateVisible] = useState(false)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const rawItems = query.data?.data ?? []
  const currentCluster = (clustersQuery.data?.data ?? []).find((item) => item.id === clusterId)
  const isAgentCluster = currentCluster?.connectionMode === 'agent'
  const filteredItems = useMemo(
    () => rawItems.filter((item) => includesSearch(searchValues(item), normalizedKeyword)),
    [normalizedKeyword, rawItems, searchValues],
  )
  const effectiveActionConfig = actionConfig
    ? {
        ...actionConfig,
        canDelete: (record: T) => {
          if (isAgentCluster) return false
          return actionConfig.canDelete ? actionConfig.canDelete(record) : true
        },
      }
    : undefined
  const shouldShowActions = effectiveActionConfig
    ? rawItems.some((item) => effectiveActionConfig.canDelete ? effectiveActionConfig.canDelete(item) : true)
    : false
  const { column: actionColumn, modalNode } = useRBACActionColumn<T>(resourcePath, shouldShowActions ? effectiveActionConfig : undefined)

  const effectiveColumns = shouldShowActions ? [...columns, actionColumn] : columns
  const effectiveEmptyDescription = !clusterId
    ? buildClusterSelectionDescription(localeCode)
    : normalizedKeyword && rawItems.length > 0
      ? localize(localeCode, buildRBACSearchEmptyDescription(title))
      : localize(localeCode, emptyDescription)
  const createDisabled = !clusterId || isAgentCluster
  const createDisabledReason = !clusterId
    ? (localeCode === 'zh_CN' ? '请先选择集群。' : 'Select a cluster first.')
    : isAgentCluster
      ? (localeCode === 'zh_CN' ? 'agent 集群暂不支持 YAML 新增。' : 'YAML create is not supported for agent-connected clusters yet.')
      : ''
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  return (
    <div className="soha-page">
      {query.isError ? (
        <ManagementState
          className="mb-3"
          description={buildRequestErrorDescription(localeCode, query.error)}
          kind="error"
          title={buildRBACErrorMessage(localeCode)}
        />
      ) : null}
      {modalNode}
      {createConfig ? (
        <CreateResourceModal
          visible={createVisible}
          onClose={() => setCreateVisible(false)}
          kind={createConfig.kind}
          resourcePath={resourcePath}
          defaultTemplate={createConfig.defaultTemplate}
          invalidationKeys={[['platform-resource', resourcePath]]}
          namespaceScope={createConfig.namespaceScope}
        />
      ) : null}
      <ManagementQueryPanel
        onFinish={() => undefined}
        actions={(
          <>
            <Button autoInsertSpace={false} disabled={!searchKeyword.trim()} htmlType="button" onClick={() => setSearchKeyword('')}>
              {localeCode === 'zh_CN' ? '重置' : 'Reset'}
            </Button>
            <Button autoInsertSpace={false} htmlType="submit" type="primary">
              {localeCode === 'zh_CN' ? '查询' : 'Search'}
            </Button>
          </>
        )}
      >
        <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
          <Input
            allowClear
            className="soha-platform-compact-field soha-workload-search-input"
            prefix={<SearchOutlined />}
            size="small"
            value={searchKeyword}
            variant="filled"
            onChange={(event) => setSearchKeyword(event.target.value)}
            placeholder={localize(localeCode, searchPlaceholder ?? buildRBACSearchPlaceholder(title))}
          />
        </ManagementQueryField>
      </ManagementQueryPanel>
      <AdminTable
        className="soha-rbac-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={effectiveColumns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={rowKey}
        loading={query.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        pageSize={10}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={(
          <ManagementTableToolbar>
            {createConfig ? (
              <Tooltip title={createDisabled ? createDisabledReason : ''}>
                <span>
                  <Button autoInsertSpace={false} size="small" type="primary" disabled={createDisabled} onClick={() => setCreateVisible(true)}>
                    {localeCode === 'zh_CN' ? '新增' : 'Create'}
                  </Button>
                </span>
              </Tooltip>
            ) : null}
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={buildRBACRefreshLabel(localeCode)}
              disabled={!clusterId}
              loading={query.isFetching}
              tooltip={buildRBACRefreshLabel(localeCode)}
              onClick={() => {
                if (clusterId) {
                  void query.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={<PlatformTableState description={effectiveEmptyDescription} kind={buildTableStateKind(clusterId, effectiveEmptyDescription)} />}
      />
    </div>
  )
}

const configMapColumns: TableColumnsType<ConfigMapResource> = [
  {
    title: 'Name',
    dataIndex: 'name',
    render: (value: string, record: ConfigMapResource) => (
      <ResourceNameLink name={value} to={`/configuration/configmaps/${encodeURIComponent(value)}${buildNamespaceQuery(record.namespace)}`} />
    ),
  },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Data', dataIndex: 'dataEntries' },
  { title: 'Binary', dataIndex: 'binaryEntries' },
  { title: 'Immutable', dataIndex: 'immutable', render: (value: boolean) => <BooleanTag value={value} /> },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const secretColumns: TableColumnsType<SecretResource> = [
  {
    title: 'Name',
    dataIndex: 'name',
    render: (value: string, record: SecretResource) => (
      <ResourceNameLink name={value} to={`/configuration/secrets/${encodeURIComponent(value)}${buildNamespaceQuery(record.namespace)}`} />
    ),
  },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Type', dataIndex: 'type', render: (value: string) => value || '-' },
  { title: 'Data', dataIndex: 'dataEntries' },
  { title: 'Immutable', dataIndex: 'immutable', render: (value: boolean) => <BooleanTag value={value} /> },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

function buildReplicaSetColumns(localeCode: 'zh_CN' | 'en_US'): TableColumnsType<ReplicaSetResource> {
  return [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      ellipsis: { showTitle: false },
      render: renderWorkloadNameText,
      width: 240,
    },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 160 },
    {
      title: 'Ready',
      dataIndex: 'readyReplicas',
      width: 190,
      render: (_: number, record: ReplicaSetResource) => renderReplicaReadyCell(record.readyReplicas, record.desiredReplicas),
    },
    { title: 'Desired', dataIndex: 'desiredReplicas', width: 96 },
    { title: 'Available', dataIndex: 'availableReplicas', width: 110 },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 120, render: (value: number) => formatAgeSeconds(value) },
  ]
}

const hpaColumns: TableColumnsType<HorizontalPodAutoscalerResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Target', dataIndex: 'targetRef' },
  { title: 'Min', dataIndex: 'minReplicas' },
  { title: 'Max', dataIndex: 'maxReplicas' },
  { title: 'Current', dataIndex: 'currentReplicas' },
  { title: 'Desired', dataIndex: 'desiredReplicas' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const pdbColumns: TableColumnsType<PodDisruptionBudgetResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Min Available', dataIndex: 'minAvailable', render: (value: string) => value || '-' },
  { title: 'Max Unavailable', dataIndex: 'maxUnavailable', render: (value: string) => value || '-' },
  { title: 'Healthy', dataIndex: 'currentHealthy' },
  { title: 'Desired', dataIndex: 'desiredHealthy' },
  { title: 'Allowed', dataIndex: 'disruptionsAllowed' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const endpointSliceColumns: TableColumnsType<EndpointSliceResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Address Type', dataIndex: 'addressType' },
  { title: 'Endpoints', dataIndex: 'endpoints' },
  { title: 'Ports', dataIndex: 'ports', render: (value: string[] | undefined) => value?.join(', ') || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const networkPolicyColumns: TableColumnsType<NetworkPolicyResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Policy Types', dataIndex: 'policyTypes', render: (value: string[] | undefined) => value?.map((item) => <Tag key={item}>{item}</Tag>) ?? '-' },
  { title: 'Ingress Rules', dataIndex: 'ingressRules' },
  { title: 'Egress Rules', dataIndex: 'egressRules' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

function buildServiceAccountColumns(localeCode: 'zh_CN' | 'en_US'): TableColumnsType<ServiceAccountResource> {
  return [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (value: string, record: ServiceAccountResource) => (
        <ResourceNameLink name={value} to={buildRBACDetailPath('serviceaccounts', value, record.namespace)} />
      ),
    },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
    { title: localeCode === 'zh_CN' ? 'Secrets' : 'Secrets', dataIndex: 'secrets', width: 88 },
    { title: localeCode === 'zh_CN' ? '镜像拉取密钥' : 'Image Pull Secrets', dataIndex: 'imagePullSecrets', width: 138 },
    {
      title: localeCode === 'zh_CN' ? '自动挂载 Token' : 'Automount Token',
      dataIndex: 'automountServiceAccountToken',
      width: 132,
      render: (value: boolean) => (
        <BooleanTag
          value={value}
          trueLabel={localeCode === 'zh_CN' ? '是' : 'Yes'}
          falseLabel={localeCode === 'zh_CN' ? '否' : 'No'}
        />
      ),
    },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
  ]
}

function buildRoleColumns(localeCode: 'zh_CN' | 'en_US'): TableColumnsType<RoleResource> {
  return [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (value: string, record: RoleResource) => (
        <ResourceNameLink name={value} to={buildRBACDetailPath('roles', value, record.namespace)} />
      ),
    },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
    { title: localeCode === 'zh_CN' ? '规则数' : 'Rules', dataIndex: 'rules', width: 88 },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
  ]
}

function buildRoleBindingColumns(localeCode: 'zh_CN' | 'en_US'): TableColumnsType<RoleBindingResource> {
  return [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (value: string, record: RoleBindingResource) => (
        <ResourceNameLink name={value} to={buildRBACDetailPath('rolebindings', value, record.namespace)} />
      ),
    },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
    {
      title: 'RoleRef',
      dataIndex: 'roleRef',
      width: 170,
      render: (value: string | undefined) => roleRefTag(value),
    },
    {
      title: localeCode === 'zh_CN' ? '主体预览' : 'Subjects',
      dataIndex: 'subjects',
      render: (value: string[] | undefined) => renderRBACSubjectChips(value, localeCode === 'zh_CN' ? '无主体' : 'No subjects'),
    },
    {
      title: localeCode === 'zh_CN' ? '主体数' : 'Subject Count',
      dataIndex: 'subjects',
      width: 108,
      render: (value: string[] | undefined) => value?.length ?? 0,
    },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
  ]
}

const ingressClassColumns: TableColumnsType<IngressClassResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Controller', dataIndex: 'controller' },
  { title: 'Default', dataIndex: 'isDefault', render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" /> },
  { title: 'Parameters', dataIndex: 'parameters', render: (value: string | undefined) => value || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const priorityClassColumns: TableColumnsType<PriorityClassResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Value', dataIndex: 'value' },
  { title: 'Global Default', dataIndex: 'globalDefault', render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" /> },
  { title: 'Preemption', dataIndex: 'preemptionPolicy', render: (value: string | undefined) => value || '-' },
  { title: 'Description', dataIndex: 'description', render: (value: string | undefined) => value || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const runtimeClassColumns: TableColumnsType<RuntimeClassResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Handler', dataIndex: 'handler' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

function buildClusterRoleColumns(localeCode: 'zh_CN' | 'en_US'): TableColumnsType<ClusterRoleResource> {
  return [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (value: string) => (
        <ResourceNameLink name={value} to={buildRBACDetailPath('clusterroles', value)} />
      ),
    },
    { title: localeCode === 'zh_CN' ? '规则数' : 'Rules', dataIndex: 'rules', width: 88 },
    { title: localeCode === 'zh_CN' ? '聚合规则' : 'Aggregation', dataIndex: 'aggregationRules', width: 108 },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
  ]
}

function buildClusterRoleBindingColumns(localeCode: 'zh_CN' | 'en_US'): TableColumnsType<ClusterRoleBindingResource> {
  return [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (value: string) => (
        <ResourceNameLink name={value} to={buildRBACDetailPath('clusterrolebindings', value)} />
      ),
    },
    {
      title: 'RoleRef',
      dataIndex: 'roleRef',
      width: 170,
      render: (value: string | undefined) => roleRefTag(value),
    },
    {
      title: localeCode === 'zh_CN' ? '主体预览' : 'Subjects',
      dataIndex: 'subjects',
      render: (value: string[] | undefined) => renderRBACSubjectChips(value, localeCode === 'zh_CN' ? '无主体' : 'No subjects'),
    },
    {
      title: localeCode === 'zh_CN' ? '主体数' : 'Subject Count',
      dataIndex: 'subjects',
      width: 108,
      render: (value: string[] | undefined) => value?.length ?? 0,
    },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 104, render: (value: number) => formatAgeSeconds(value) },
  ]
}

const mutatingWebhookColumns: TableColumnsType<MutatingWebhookConfigurationResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Webhooks', dataIndex: 'webhooks' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const validatingWebhookColumns: TableColumnsType<ValidatingWebhookConfigurationResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Webhooks', dataIndex: 'webhooks' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

function renderQuotaProgress(record: ResourceQuotaResource) {
  const hard = record.hard ?? {}
  const used = record.used ?? {}
  const keys = Object.keys(hard)
  if (keys.length === 0) return <Text type="secondary">-</Text>
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 220 }}>
      {keys.map((key) => {
        const hardRaw = hard[key]
        const usedRaw = used[key] ?? '0'
        const hardNum = parseQuotaNumeric(hardRaw)
        const usedNum = parseQuotaNumeric(usedRaw)
        const percent = hardNum && hardNum > 0 && usedNum != null
          ? Math.min(100, Math.round((usedNum / hardNum) * 100))
          : 0
        return (
          <Tooltip key={key} title={`${key}: ${usedRaw} / ${hardRaw}`} placement="top">
            <div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'rgba(0, 0, 0, 0.45)' }}>
                <span style={{ maxWidth: 140, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{key}</span>
                <span>{usedRaw} / {hardRaw}</span>
              </div>
              <Progress percent={percent} aria-label={`${key} quota usage`} showInfo={false} size="small" />
            </div>
          </Tooltip>
        )
      })}
    </div>
  )
}

const resourceQuotaColumns: TableColumnsType<ResourceQuotaResource> = [
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Name', dataIndex: 'name' },
  { title: 'Scopes', dataIndex: 'scopes', render: (value: string[] | undefined) => value?.join(', ') || '-' },
  { title: 'Usage', dataIndex: 'hard', render: (_: unknown, record: ResourceQuotaResource) => renderQuotaProgress(record) },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const limitRangeColumns: TableColumnsType<LimitRangeResource> = [
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Name', dataIndex: 'name' },
  { title: 'Limits', dataIndex: 'limits' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const leaseColumns: TableColumnsType<LeaseResource> = [
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Name', dataIndex: 'name' },
  { title: 'Holder', dataIndex: 'holderIdentity', render: (value: string | undefined) => value || '-' },
  { title: 'Duration (s)', dataIndex: 'leaseDurationSeconds', render: (value: number | undefined) => (value == null ? '-' : String(value)) },
  { title: 'Acquired', dataIndex: 'acquireTime', render: (value: string | undefined) => (value ? formatDateTime(value) : '-') },
  { title: 'Renewed', dataIndex: 'renewTime', render: (value: string | undefined) => (value ? formatDateTime(value) : '-') },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

function buildReplicationControllerColumns(localeCode: 'zh_CN' | 'en_US'): TableColumnsType<ReplicationControllerResource> {
  return [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      ellipsis: { showTitle: false },
      render: renderWorkloadNameText,
      width: 260,
    },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 160 },
    {
      title: 'Ready',
      dataIndex: 'readyReplicas',
      width: 190,
      render: (_: number, record: ReplicationControllerResource) => renderReplicaReadyCell(record.readyReplicas, record.desiredReplicas),
    },
    { title: 'Desired', dataIndex: 'desiredReplicas', width: 96 },
    { title: 'Current', dataIndex: 'currentReplicas', width: 96 },
    { title: 'Available', dataIndex: 'availableReplicas', width: 110 },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', width: 120, render: (value: number) => formatAgeSeconds(value) },
  ]
}

export function WorkloadsReplicaSetsPage() {
  const { localeCode } = useI18n()

  return (
    <WorkloadReplicaListPage<ReplicaSetResource>
      title={{ zh_CN: 'ReplicaSets', en_US: 'ReplicaSets' }}
      resourcePath="workloads/replicasets"
      columns={buildReplicaSetColumns(localeCode)}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 ReplicaSets', en_US: 'No replica sets in the current scope' }}
      searchPlaceholder={{ zh_CN: '搜索 ReplicaSet / Namespace', en_US: 'Search replica set / namespace' }}
      searchValues={(record) => [record.name, record.namespace]}
    />
  )
}

export function WorkloadsReplicationControllersPage() {
  const { localeCode } = useI18n()

  return (
    <WorkloadReplicaListPage<ReplicationControllerResource>
      title={{ zh_CN: 'ReplicationControllers', en_US: 'ReplicationControllers' }}
      resourcePath="workloads/replicationcontrollers"
      columns={buildReplicationControllerColumns(localeCode)}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 ReplicationController', en_US: 'No replication controllers in the current scope' }}
      searchPlaceholder={{ zh_CN: '搜索 ReplicationController / Namespace', en_US: 'Search replication controller / namespace' }}
      searchValues={(record) => [record.name, record.namespace]}
      actionConfig={{
        resourceKind: 'ReplicationController',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
      }}
    />
  )
}

export function ConfigurationConfigMapsPage() {
  const { localeCode } = useI18n()
  const [createVisible, setCreateVisible] = useState(false)
  return (
    <div className="soha-page">
      <ResourceTableCard<ConfigMapResource>
        columns={configMapColumns}
        headerExtra={(
          <Button autoInsertSpace={false} size="small" type="primary" icon={<PlusOutlined />} onClick={() => setCreateVisible(true)}>
            {localeCode === 'zh_CN' ? '新增' : 'Create'}
          </Button>
        )}
        resourcePath="configuration/configmaps"
        rowKey={(record) => `${record.namespace}/${record.name}`}
        title={{ zh_CN: 'ConfigMaps', en_US: 'ConfigMaps' }}
        emptyDescription={{ zh_CN: '当前范围没有 ConfigMaps', en_US: 'No configmaps in the current scope' }}
        actionConfig={{
          resourceKind: 'ConfigMap',
          getName: (record) => record.name,
          getNamespace: (record) => record.namespace,
        }}
      />
      <CreateResourceModal
        visible={createVisible}
        onClose={() => setCreateVisible(false)}
        kind="ConfigMap"
        resourcePath="configuration/configmaps"
        defaultTemplate={CONFIGMAP_DEFAULT_TEMPLATE}
        invalidationKeys={[['platform-resource', 'configuration/configmaps']]}
      />
    </div>
  )
}

export function ConfigurationSecretsPage() {
  const { localeCode } = useI18n()
  const [createVisible, setCreateVisible] = useState(false)
  return (
    <div className="soha-page">
      <ResourceTableCard<SecretResource>
        columns={secretColumns}
        headerExtra={(
          <Button autoInsertSpace={false} size="small" type="primary" icon={<PlusOutlined />} onClick={() => setCreateVisible(true)}>
            {localeCode === 'zh_CN' ? '新增' : 'Create'}
          </Button>
        )}
        resourcePath="configuration/secrets"
        rowKey={(record) => `${record.namespace}/${record.name}`}
        title={{ zh_CN: 'Secrets', en_US: 'Secrets' }}
        emptyDescription={{ zh_CN: '当前范围没有 Secrets', en_US: 'No secrets in the current scope' }}
        actionConfig={{
          resourceKind: 'Secret',
          getName: (record) => record.name,
          getNamespace: (record) => record.namespace,
        }}
      />
      <CreateResourceModal
        visible={createVisible}
        onClose={() => setCreateVisible(false)}
        kind="Secret"
        resourcePath="configuration/secrets"
        defaultTemplate={SECRET_DEFAULT_TEMPLATE}
        invalidationKeys={[['platform-resource', 'configuration/secrets']]}
      />
    </div>
  )
}

export function ConfigurationResourceQuotasPage() {
  return (
    <ResourceListPage<ResourceQuotaResource>
      title={{ zh_CN: 'ResourceQuotas', en_US: 'ResourceQuotas' }}
      resourcePath="configuration/resourcequotas"
      columns={resourceQuotaColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 ResourceQuota', en_US: 'No resource quotas in the current scope' }}
      actionConfig={{
        resourceKind: 'ResourceQuota',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
      }}
    />
  )
}

export function ConfigurationLimitRangesPage() {
  return (
    <ResourceListPage<LimitRangeResource>
      title={{ zh_CN: 'LimitRanges', en_US: 'LimitRanges' }}
      resourcePath="configuration/limitranges"
      columns={limitRangeColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 LimitRange', en_US: 'No limit ranges in the current scope' }}
      actionConfig={{
        resourceKind: 'LimitRange',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
      }}
    />
  )
}

export function ConfigurationHPAPage() {
  return (
    <ResourceListPage<HorizontalPodAutoscalerResource>
      title={{ zh_CN: 'HorizontalPodAutoscalers', en_US: 'HorizontalPodAutoscalers' }}
      resourcePath="configuration/hpas"
      columns={hpaColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 HPA', en_US: 'No HPA resources in the current scope' }}
    />
  )
}

export function ConfigurationPodDisruptionBudgetsPage() {
  return (
    <ResourceListPage<PodDisruptionBudgetResource>
      title={{ zh_CN: 'PodDisruptionBudgets', en_US: 'PodDisruptionBudgets' }}
      resourcePath="configuration/poddisruptionbudgets"
      columns={pdbColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 PodDisruptionBudgets', en_US: 'No pod disruption budgets in the current scope' }}
    />
  )
}

export function ConfigurationPriorityClassesPage() {
  return (
    <ResourceListPage<PriorityClassResource>
      title={{ zh_CN: 'PriorityClasses', en_US: 'PriorityClasses' }}
      resourcePath="configuration/priorityclasses"
      columns={priorityClassColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 PriorityClass', en_US: 'No priority classes in this cluster' }}
      actionConfig={{ resourceKind: 'PriorityClass', getName: (record) => record.name }}
    />
  )
}

export function ConfigurationRuntimeClassesPage() {
  return (
    <ResourceListPage<RuntimeClassResource>
      title={{ zh_CN: 'RuntimeClasses', en_US: 'RuntimeClasses' }}
      resourcePath="configuration/runtimeclasses"
      columns={runtimeClassColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 RuntimeClass', en_US: 'No runtime classes in this cluster' }}
      actionConfig={{ resourceKind: 'RuntimeClass', getName: (record) => record.name }}
    />
  )
}

export function ConfigurationLeasesPage() {
  return (
    <ResourceListPage<LeaseResource>
      title={{ zh_CN: 'Leases', en_US: 'Leases' }}
      resourcePath="configuration/leases"
      columns={leaseColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 Lease', en_US: 'No leases in the current scope' }}
      actionConfig={{
        resourceKind: 'Lease',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
      }}
    />
  )
}

export function ConfigurationMutatingWebhooksPage() {
  return (
    <ResourceListPage<MutatingWebhookConfigurationResource>
      title={{ zh_CN: 'MutatingWebhookConfigurations', en_US: 'MutatingWebhookConfigurations' }}
      resourcePath="configuration/mutatingwebhookconfigurations"
      columns={mutatingWebhookColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 MutatingWebhookConfiguration', en_US: 'No mutating webhook configurations in this cluster' }}
      actionConfig={{ resourceKind: 'MutatingWebhookConfiguration', getName: (record) => record.name }}
    />
  )
}

export function ConfigurationValidatingWebhooksPage() {
  return (
    <ResourceListPage<ValidatingWebhookConfigurationResource>
      title={{ zh_CN: 'ValidatingWebhookConfigurations', en_US: 'ValidatingWebhookConfigurations' }}
      resourcePath="configuration/validatingwebhookconfigurations"
      columns={validatingWebhookColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 ValidatingWebhookConfiguration', en_US: 'No validating webhook configurations in this cluster' }}
      actionConfig={{ resourceKind: 'ValidatingWebhookConfiguration', getName: (record) => record.name }}
    />
  )
}

export function NetworkEndpointSlicesPage() {
  return (
    <ResourceListPage<EndpointSliceResource>
      title={{ zh_CN: 'EndpointSlices', en_US: 'EndpointSlices' }}
      resourcePath="network/endpointslices"
      columns={endpointSliceColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 EndpointSlices', en_US: 'No endpoint slices in the current scope' }}
    />
  )
}

export function NetworkIngressClassesPage() {
  return (
    <ResourceListPage<IngressClassResource>
      title={{ zh_CN: 'IngressClasses', en_US: 'IngressClasses' }}
      resourcePath="network/ingressclasses"
      columns={ingressClassColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 IngressClass', en_US: 'No ingress classes in this cluster' }}
      actionConfig={{ resourceKind: 'IngressClass', getName: (record) => record.name }}
    />
  )
}

export function NetworkPoliciesPage() {
  return (
    <ResourceListPage<NetworkPolicyResource>
      title={{ zh_CN: 'NetworkPolicies', en_US: 'NetworkPolicies' }}
      resourcePath="network/networkpolicies"
      columns={networkPolicyColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 NetworkPolicies', en_US: 'No network policies in the current scope' }}
    />
  )
}

export function NetworkPortForwardPage() {
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [modalVisible, setModalVisible] = useState(false)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [form, setForm] = useState<{ targetKind: string; targetName: string; namespace: string; localPort: number; remotePort: number }>({
    targetKind: 'Pod',
    targetName: '',
    namespace: namespace || 'default',
    localPort: 8080,
    remotePort: 80,
  })

  const listKey = ['port-forwards', clusterId]
  const query = useQuery({
    queryKey: listKey,
    queryFn: () => api.get<ApiResponse<PortForwardSession[]>>(`/clusters/${clusterId}/network/port-forwards`),
    enabled: !!clusterId,
  })

  const registerMutation = useMutation({
    mutationFn: (payload: typeof form) =>
      api.post<ApiResponse<PortForwardSession>>(`/clusters/${clusterId}/network/port-forwards`, payload),
    onSuccess: () => {
      setModalVisible(false)
      void message.success(localeCode === 'zh_CN' ? '已登记 Port Forward' : 'Port forward registered')
      queryClient.invalidateQueries({ queryKey: listKey })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const stopMutation = useMutation({
    mutationFn: (sessionId: string) =>
      api.delete(`/clusters/${clusterId}/network/port-forwards/${sessionId}`),
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? '已停止' : 'Stopped')
      queryClient.invalidateQueries({ queryKey: listKey })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const rawItems = query.data?.data ?? []
  const filteredItems = useMemo(
    () => rawItems.filter((item) => includesSearch([
      item.sessionId,
      item.namespace,
      item.targetKind,
      item.targetName,
      item.status,
      String(item.localPort),
      String(item.remotePort),
    ], normalizedKeyword)),
    [normalizedKeyword, rawItems],
  )
  const effectiveEmpty = !clusterId
    ? (localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster')
    : normalizedKeyword && rawItems.length > 0
      ? (localeCode === 'zh_CN' ? '没有匹配的 Port Forward' : 'No matching port forward sessions')
      : (localeCode === 'zh_CN' ? '当前集群没有登记的 Port Forward' : 'No port forward sessions registered')
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  const columns: TableColumnsType<PortForwardSession> = [
    { title: localeCode === 'zh_CN' ? '会话' : 'Session', dataIndex: 'sessionId', render: (value: string) => <Text code>{value.slice(0, 8)}</Text> },
    { title: 'Namespace', dataIndex: 'namespace' },
    { title: localeCode === 'zh_CN' ? '目标' : 'Target', dataIndex: 'targetName', render: (_: unknown, record: PortForwardSession) => `${record.targetKind}/${record.targetName}` },
    { title: localeCode === 'zh_CN' ? '本地端口' : 'Local', dataIndex: 'localPort' },
    { title: localeCode === 'zh_CN' ? '远端端口' : 'Remote', dataIndex: 'remotePort' },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status', render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{value}</Tag> },
    { title: localeCode === 'zh_CN' ? '创建时间' : 'Created', dataIndex: 'createdAt', render: (value: string) => formatDateTime(value) },
    {
      title: localeCode === 'zh_CN' ? '操作' : 'Actions',
      dataIndex: 'sessionId',
      fixed: 'right',
      align: 'center',
      width: 64,
      render: (value: string) => (
        <Popconfirm
          title={localeCode === 'zh_CN' ? '确认停止该 Port Forward？' : 'Stop this port forward?'}
          description={localeCode === 'zh_CN' ? '这只会停止 Soha 中登记的转发会话记录。' : 'This stops the registered forward session record in Soha.'}
          okText={localeCode === 'zh_CN' ? '停止' : 'Stop'}
          cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
          okButtonProps={{ danger: true, loading: stopMutation.isPending && stopMutation.variables === value }}
          placement="topRight"
          onConfirm={() => stopMutation.mutate(value)}
        >
          <Tooltip title={localeCode === 'zh_CN' ? '停止' : 'Stop'}>
            <Button
              aria-label={localeCode === 'zh_CN' ? '停止 Port Forward' : 'Stop port forward'}
              size="small"
              type="text"
              danger
              icon={<DeleteOutlined />}
              loading={stopMutation.isPending && stopMutation.variables === value}
            />
          </Tooltip>
        </Popconfirm>
      ),
    },
  ]

  return (
    <div className="soha-page">
      <ManagementQueryPanel
        onFinish={() => undefined}
        actions={(
          <>
            <Button autoInsertSpace={false} disabled={!searchKeyword.trim()} htmlType="button" onClick={() => setSearchKeyword('')}>
              {localeCode === 'zh_CN' ? '重置' : 'Reset'}
            </Button>
            <Button autoInsertSpace={false} htmlType="submit" type="primary">
              {localeCode === 'zh_CN' ? '查询' : 'Search'}
            </Button>
          </>
        )}
      >
        <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
          <Input
            allowClear
            className="soha-platform-compact-field soha-workload-search-input"
            prefix={<SearchOutlined />}
            size="small"
            value={searchKeyword}
            variant="filled"
            onChange={(event) => setSearchKeyword(event.target.value)}
            placeholder={localeCode === 'zh_CN' ? '搜索会话 / Namespace / 目标 / 状态 / 端口' : 'Search session / namespace / target / status / port'}
          />
        </ManagementQueryField>
      </ManagementQueryPanel>
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={(
          <ManagementTableToolbar>
            <Tooltip title={!clusterId ? (localeCode === 'zh_CN' ? '请先选择集群' : 'Select a cluster first') : ''}>
              <span>
                <Button
                  autoInsertSpace={false}
                  size="small"
                  type="primary"
                  icon={<PlusOutlined />}
                  disabled={!clusterId}
                  onClick={() => setModalVisible(true)}
                >
                  {localeCode === 'zh_CN' ? '新建 Port Forward' : 'New Port Forward'}
                </Button>
              </span>
            </Tooltip>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              disabled={!clusterId}
              loading={query.isFetching}
              tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onClick={() => {
                if (clusterId) {
                  void query.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey="sessionId"
        loading={query.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        pageSize={10}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        empty={<PlatformTableState description={effectiveEmpty} kind={!clusterId ? 'select-scope' : 'empty'} />}
      />
      <Modal
        title={localeCode === 'zh_CN' ? '新建 Port Forward' : 'New Port Forward'}
        open={modalVisible}
        onOk={() => registerMutation.mutate(form)}
        onCancel={() => setModalVisible(false)}
        confirmLoading={registerMutation.isPending}
      >
        <Space orientation="vertical" align="start" style={{ width: '100%' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '目标类型' : 'Target kind'}</Text>
            <Select
              value={form.targetKind}
              onChange={(value) => setForm((prev) => ({ ...prev, targetKind: String(value) }))}
              style={{ flex: 1 }}
              options={[
                { value: 'Pod', label: 'Pod' },
                { value: 'Service', label: 'Service' },
              ]}
            />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>Namespace</Text>
            <Input value={form.namespace} onChange={(event) => setForm((prev) => ({ ...prev, namespace: event.target.value }))} style={{ flex: 1 }} />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '目标名称' : 'Target name'}</Text>
            <Input value={form.targetName} onChange={(event) => setForm((prev) => ({ ...prev, targetName: event.target.value }))} style={{ flex: 1 }} />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '本地端口' : 'Local port'}</Text>
            <InputNumber value={form.localPort} min={1} max={65535} onChange={(v) => setForm((prev) => ({ ...prev, localPort: Number(v) || 0 }))} style={{ flex: 1 }} />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '远端端口' : 'Remote port'}</Text>
            <InputNumber value={form.remotePort} min={1} max={65535} onChange={(v) => setForm((prev) => ({ ...prev, remotePort: Number(v) || 0 }))} style={{ flex: 1 }} />
          </div>
        </Space>
      </Modal>
    </div>
  )
}

export function PlatformAccessControlServiceAccountsPage() {
  const { localeCode } = useI18n()
  return (
    <RBACListPage<ServiceAccountResource>
      title={{ zh_CN: 'ServiceAccounts', en_US: 'ServiceAccounts' }}
      resourcePath="access-control/serviceaccounts"
      columns={buildServiceAccountColumns(localeCode)}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 ServiceAccounts', en_US: 'No service accounts in the current scope' }}
      searchValues={(record) => [record.name, record.namespace]}
      createConfig={{
        kind: 'ServiceAccount',
        defaultTemplate: SERVICE_ACCOUNT_DEFAULT_TEMPLATE,
      }}
      actionConfig={{
        resourceKind: 'ServiceAccount',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
        canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete'),
      }}
    />
  )
}

export function PlatformAccessControlClusterRolesPage() {
  const { localeCode } = useI18n()
  return (
    <RBACListPage<ClusterRoleResource>
      title={{ zh_CN: 'ClusterRoles', en_US: 'ClusterRoles' }}
      resourcePath="access-control/clusterroles"
      columns={buildClusterRoleColumns(localeCode)}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 ClusterRole', en_US: 'No cluster roles in this cluster' }}
      searchValues={(record) => [record.name]}
      createConfig={{
        kind: 'ClusterRole',
        defaultTemplate: CLUSTER_ROLE_DEFAULT_TEMPLATE,
        namespaceScope: 'cluster',
      }}
      actionConfig={{
        resourceKind: 'ClusterRole',
        getName: (record) => record.name,
        canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete'),
      }}
    />
  )
}

export function PlatformAccessControlRolesPage() {
  const { localeCode } = useI18n()
  return (
    <RBACListPage<RoleResource>
      title={{ zh_CN: 'Roles', en_US: 'Roles' }}
      resourcePath="access-control/roles"
      columns={buildRoleColumns(localeCode)}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 Roles', en_US: 'No roles in the current scope' }}
      searchValues={(record) => [record.name, record.namespace]}
      createConfig={{
        kind: 'Role',
        defaultTemplate: ROLE_DEFAULT_TEMPLATE,
      }}
      actionConfig={{
        resourceKind: 'Role',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
        canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete'),
      }}
    />
  )
}

export function PlatformAccessControlClusterRoleBindingsPage() {
  const { localeCode } = useI18n()
  return (
    <RBACListPage<ClusterRoleBindingResource>
      title={{ zh_CN: 'ClusterRoleBindings', en_US: 'ClusterRoleBindings' }}
      resourcePath="access-control/clusterrolebindings"
      columns={buildClusterRoleBindingColumns(localeCode)}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 ClusterRoleBinding', en_US: 'No cluster role bindings in this cluster' }}
      searchValues={(record) => [record.name, record.roleRef, ...(record.subjects ?? [])]}
      createConfig={{
        kind: 'ClusterRoleBinding',
        defaultTemplate: CLUSTER_ROLE_BINDING_DEFAULT_TEMPLATE,
        namespaceScope: 'cluster',
      }}
      actionConfig={{
        resourceKind: 'ClusterRoleBinding',
        getName: (record) => record.name,
        canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete'),
      }}
    />
  )
}

export function PlatformAccessControlRoleBindingsPage() {
  const { localeCode } = useI18n()
  return (
    <RBACListPage<RoleBindingResource>
      title={{ zh_CN: 'RoleBindings', en_US: 'RoleBindings' }}
      resourcePath="access-control/rolebindings"
      columns={buildRoleBindingColumns(localeCode)}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 RoleBindings', en_US: 'No role bindings in the current scope' }}
      searchValues={(record) => [record.name, record.namespace, record.roleRef, ...(record.subjects ?? [])]}
      createConfig={{
        kind: 'RoleBinding',
        defaultTemplate: ROLE_BINDING_DEFAULT_TEMPLATE,
      }}
      actionConfig={{
        resourceKind: 'RoleBinding',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
        canDelete: (record) => hasAllowedAction(record.allowedActions, 'delete'),
      }}
    />
  )
}
