import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementIconButton,
  ManagementQueryField,
  ManagementSearchableListPane,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import { namespaceQueries } from '@/features/platform/cluster-resources/queries'
import { toClusterScope } from '@/features/platform/cluster-resources/scope'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import type { KubernetesResourceRef } from '@opensoha/contracts/gen/ts/sohaapi'
import {
  ResourceDetailHeader,
  ResourceDetailTabs,
} from '@/features/platform/shared/resource-detail-header'
import {
  WorkloadRefresh,
  useWorkloadRefresh,
} from '@/features/platform/workloads/shared/workload-refresh'
import { formatAgeSeconds, formatRelativeTime } from '@/utils/time'
import { ArrowLeftOutlined, DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType, TableProps } from 'antd'
import { App, Button, Card, Descriptions, Popconfirm, Select, Spin } from 'antd'
import {
  lazy,
  Suspense,
  useCallback,
  useDeferredValue,
  useEffect,
  useLayoutEffect,
  useState,
} from 'react'
import { useBeforeUnload, useLocation, useSearchParams } from 'react-router-dom'
import { CRDInstanceOverview } from './instance-overview'
import { ResourceQueryPanel } from '../shared/resource-query-panel'
import { includesSearch, normalizeSearchKeyword } from '../shared/search'
import './instance-workspace.css'
import { crdMutations } from './mutations'
import { buildCRDApiGroupDetailPath } from './paths'
import { crdQueries } from './queries'
import type { CRD, CRDResourceInstance } from './types'
import { getServedVersions, isNamespacedCRD } from './utils'
import type { CRDEditorState } from './use-navigation-guard'

const CRDResourceEditorModal = lazy(async () => ({
  default: (await import('./resource-editor-modal')).CRDResourceEditorModal,
}))
const CRDResourceEditor = lazy(async () => ({
  default: (await import('./resource-editor-modal')).CRDResourceEditor,
}))
const WorkloadRelationships = lazy(async () => ({
  default: (await import('@/features/platform/shared/resource-insights/workload-relationships'))
    .WorkloadRelationships,
}))
const definitionKind = 'CustomResourceDefinition'
const definitionAPI = {
  apiVersion: 'apiextensions.k8s.io/v1',
  resource: 'customresourcedefinitions',
}
const canViewInstance = (instance?: CRDResourceInstance) =>
  ['view', 'update'].some((action) => hasAllowedAction(instance?.allowedActions, action))
const resourceKey = (record: CRDResourceInstance) => `${record.namespace || ''}/${record.name}`
function resourceAge(record: CRDResourceInstance, now: number) {
  const created = record.createdAt ? Date.parse(record.createdAt) : NaN
  return Number.isFinite(created)
    ? Math.max(0, (now - created) / 1000)
    : (record.ageSeconds ?? Number.POSITIVE_INFINITY)
}

export function CRDKindWorkspace({
  crd,
  kinds,
  onKindSelect,
  onLeave,
  onStateChange,
}: {
  crd: CRD
  kinds: CRD[]
  onKindSelect: (name: string) => void
  onLeave?: (action: () => void) => void
  onStateChange?: (state: CRDEditorState) => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { modal } = App.useApp()
  const [kindSearch, setKindSearch] = useState('')
  const [editorState, setEditorState] = useState<CRDEditorState>({ dirty: false, busy: false })
  const reportEditorState = useCallback(
    (next: CRDEditorState) => {
      setEditorState(next)
      onStateChange?.(next)
    },
    [onStateChange],
  )
  useLayoutEffect(() => () => onStateChange?.({ dirty: false, busy: false }), [onStateChange])
  useBeforeUnload((event) => {
    if (editorState.dirty || editorState.busy) {
      event.preventDefault()
      event.returnValue = ''
    }
  })
  const keyword = normalizeSearchKeyword(useDeferredValue(kindSearch))
  const leaveResource = (action: () => void) => {
    if (onLeave) return onLeave(action)
    if (editorState.busy) return
    const switchNow = () => {
      editorState.discard?.()
      setEditorState({ dirty: false, busy: false })
      action()
    }
    if (!editorState.dirty) switchNow()
    else
      modal.confirm({
        title: zh ? '离开当前资源？' : 'Leave this resource?',
        content: zh
          ? '尚未应用的更改会被放弃。已保存的本地草稿仍会保留。'
          : 'Unapplied changes will be discarded. Saved local drafts are retained.',
        okText: zh ? '放弃更改并切换' : 'Discard and switch',
        cancelText: zh ? '取消' : 'Cancel',
        onOk: switchNow,
      })
  }
  return (
    <div className="soha-crd-instance-workspace">
      <ManagementSearchableListPane
        className="soha-crd-kind-directory"
        activeKey={crd.name}
        getItemKey={(kind: CRD) => kind.name}
        items={kinds.filter((kind) =>
          includesSearch([kind.kind, kind.name, kind.plural, ...getServedVersions(kind)], keyword),
        )}
        searchValue={kindSearch}
        onSearchChange={setKindSearch}
        searchPlaceholder={zh ? '搜索 CRD 类型' : 'Search CRD types'}
        emptyTitle={zh ? '没有匹配的 CRD 类型' : 'No matching CRD types'}
        onItemSelect={(kind) => {
          if (kind.name !== crd.name) leaveResource(() => onKindSelect(kind.name))
        }}
        renderItem={(kind) => (
          <>
            <span className="soha-crd-kind-heading">
              <strong title={kind.name}>{kind.kind}</strong>
              <span className="soha-crd-resource-secondary">
                {getServedVersions(kind).join(', ')}
              </span>
            </span>
            <span className="soha-crd-resource-secondary" title={kind.name}>
              {kind.plural} ·{' '}
              {kind.scope === 'Namespaced'
                ? zh
                  ? '命名空间'
                  : 'Namespaced'
                : zh
                  ? '集群'
                  : 'Cluster'}
            </span>
          </>
        )}
      />
      <div className="soha-crd-instance-content">
        <CRDInstancesWorkspace
          key={crd.name}
          crd={crd}
          onLeave={leaveResource}
          onStateChange={reportEditorState}
        />
      </div>
    </div>
  )
}

function CRDInstancesWorkspace({
  crd,
  onLeave,
  onStateChange,
}: {
  crd: CRD
  onLeave: (action: () => void) => void
  onStateChange: (state: CRDEditorState) => void
}) {
  const { t, localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { clusterId, namespace: storedNamespace, setNamespace } = usePlatformScopeStore()
  const [params, setParams] = useSearchParams()
  const { key: locationKey } = useLocation()
  // List scope and the selected object's namespace are deliberately separate.
  const namespace = params.has('namespace') ? params.get('namespace') || null : storedNamespace
  const resourceName = params.get('resource')
  const resourceNamespace = params.get('resourceNamespace') || ''
  const resourceView = Boolean(resourceName)
  const tab = params.get('tab') || (resourceView ? 'overview' : 'instances')
  const refreshSettings = useWorkloadRefresh()
  const scopedParams = (previous: URLSearchParams, changes: Record<string, string | null>) => {
    const next = new URLSearchParams(previous)
    next.set('kind', crd.name)
    if (clusterId) next.set('clusterId', clusterId)
    next.set('namespace', namespace || '')
    Object.entries(changes).forEach(([key, value]) =>
      value === null ? next.delete(key) : next.set(key, value),
    )
    return next
  }
  const updateView = (changes: Record<string, string | null>, replace = false) =>
    setParams((previous) => scopedParams(previous, changes), { replace })
  const queryClient = useQueryClient()
  const capability = useClusterCapability('custom.resources', localeCode)
  const [createOpen, setCreateOpen] = useState(false)
  const [createNamespace, setCreateNamespace] = useState<string | null>(null)
  const [selection, setSelection] = useState<CRDResourceInstance>()
  const [returnAfterDelete, setReturnAfterDelete] = useState<{
    locationKey: string
    uid: string
  } | null>(null)
  const [editingTarget, setEditingTarget] = useState<string | null>(null)
  const editing = tab === 'yaml' && editingTarget === `${resourceNamespace}/${resourceName}`
  const [editorState, setEditorState] = useState({ dirty: false, busy: false })
  const [createEditorState, setCreateEditorState] = useState({ dirty: false, busy: false })
  const [searchKeyword, setSearchKeyword] = useState('')
  const [sort, setSort] = useState<{ key?: string; order?: 'ascend' | 'descend' | null }>({})
  const keyword = normalizeSearchKeyword(useDeferredValue(searchKeyword))
  const disabled = capability.disabled
  const namespacesQuery = useQuery({
    ...namespaceQueries.list(toClusterScope(clusterId)),
    enabled: Boolean(clusterId) && isNamespacedCRD(crd) && !capability.disabled,
  })
  const namespaces = readableQueryData(namespacesQuery) ?? []
  const accessQuery = useQuery(
    crdQueries.access(clusterId, crd, namespace, !capability.isLoading && !disabled),
  )
  const visibleAccessQueryData = readableQueryData(accessQuery)
  const resourcesQuery = useQuery({
    ...crdQueries.resources(clusterId, crd, namespace, !capability.isLoading && !disabled),
    refetchInterval: (query) =>
      !editing &&
      !editorState.dirty &&
      !editorState.busy &&
      !createOpen &&
      refreshSettings.interval(query)
        ? query.state.data?.some((item) => item.deletingAt)
          ? 3000
          : 15000
        : false,
  })
  const catalogQuery = useQuery({
    ...crdQueries.catalog(clusterId),
    refetchInterval: (query) =>
      !editing &&
      !editorState.dirty &&
      !editorState.busy &&
      !createOpen &&
      refreshSettings.interval(query)
        ? query.state.data?.some((item) => item.deletingAt)
          ? 3000
          : 15000
        : false,
  })
  const visibleResourcesQueryData = readableQueryData(resourcesQuery)
  const permissions = usePermissionSnapshot()
  const canCreate =
    !disabled &&
    visibleAccessQueryData?.allowedActions?.includes('create') === true &&
    hasPermission(permissions.data?.data, 'platform.extensions.custom-resources.create')
  const deleteMutation = usePlatformMutation(crdMutations.remove(queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = deleteMutation.blocked
  const resources = disabled ? [] : (visibleResourcesQueryData ?? [])
  const filtered = resources.filter((record) =>
    includesSearch(
      [
        record.name,
        record.namespace,
        record.kind || crd.kind,
        record.apiVersion || `${crd.group}/${crd.version}`,
        record.status,
        ...Object.entries(record.summary ?? {}).flatMap(([key, value]) => [key, value]),
      ],
      keyword,
    ),
  )
  const resourcesReadable =
    resourcesQuery.isEnabled && (!resourcesQuery.isError || visibleResourcesQueryData !== undefined)
  const selected =
    resourceView && resourcesReadable
      ? (resources.find(
          (record) =>
            record.name === resourceName &&
            (!isNamespacedCRD(crd) || (record.namespace || '') === resourceNamespace),
        ) ??
        ((editing || resourcesQuery.isFetching || resourcesQuery.isError) &&
        selection?.name === resourceName &&
        (selection.namespace || '') === resourceNamespace
          ? selection
          : undefined))
      : undefined
  const canView = canViewInstance(selected)
  const canUpdate =
    selected && !selected.deletingAt && hasAllowedAction(selected.allowedActions, 'update')
  const namespaced = isNamespacedCRD(crd)
  const servedVersion = getServedVersions(crd).includes(crd.version)
  const instanceAPI = { apiVersion: `${crd.group}/${crd.version}`, resource: crd.plural }
  const resolveCustomDetailPath = (resource: KubernetesResourceRef) => {
    if (!clusterId || resource.clusterId !== clusterId) return null
    const groupPath = buildCRDApiGroupDetailPath(crd.group)
    if (
      resource.kind === definitionKind &&
      resource.apiVersion === definitionAPI.apiVersion &&
      resource.name === crd.name &&
      resource.scopeMode === 'cluster' &&
      !resource.namespace &&
      (!resource.uid || !crd.uid || resource.uid === crd.uid)
    ) {
      return `${groupPath}?${scopedParams(params, {
        resource: null,
        resourceNamespace: null,
        tab: 'definition',
      })}`
    }
    if (
      resource.kind !== crd.kind ||
      !servedVersion ||
      resource.apiVersion !== instanceAPI.apiVersion ||
      resource.scopeMode !== (namespaced ? 'namespace' : 'cluster') ||
      Boolean(resource.namespace) !== namespaced ||
      (namespaced && namespace && resource.namespace !== namespace)
    )
      return null
    const instance = resources.find(
      (record) =>
        record.name === resource.name &&
        (record.namespace || '') === (resource.namespace || '') &&
        (!record.kind || record.kind === resource.kind) &&
        (!record.apiVersion || record.apiVersion === resource.apiVersion) &&
        (!resource.uid || resource.uid === record.uid),
    )
    if (!instance || !canViewInstance(instance)) return null
    return `${groupPath}?${scopedParams(params, {
      resource: instance.name,
      resourceNamespace: instance.namespace || '',
      tab: 'overview',
    })}`
  }
  const relationshipRoot = resourceView
    ? selected && canView && servedVersion && (!namespaced || selected.namespace)
      ? {
          kind: crd.kind,
          name: selected.name,
          namespace: namespaced ? (selected.namespace ?? null) : null,
          api: instanceAPI,
        }
      : null
    : { kind: definitionKind, name: crd.name, namespace: null, api: definitionAPI }
  const relationshipTab = {
    key: 'relationships',
    label: zh ? '关系' : 'Relationships',
    disabled: !relationshipRoot,
    children:
      tab === 'relationships' && clusterId && relationshipRoot ? (
        <Suspense fallback={<ManagementState kind="loading" />}>
          <WorkloadRelationships
            kind={relationshipRoot.kind}
            name={relationshipRoot.name}
            scope={{ clusterId, namespace: relationshipRoot.namespace }}
            rootAPI={relationshipRoot.api}
            resolveCustomDetailPath={resolveCustomDetailPath}
          />
        </Suspense>
      ) : null,
  }
  const busy = editorState.busy || createEditorState.busy || deleteMutation.blocked
  const discard = useCallback(() => {
    setEditingTarget(null)
    setEditorState({ dirty: false, busy: false })
    setCreateEditorState({ dirty: false, busy: false })
    setCreateOpen(false)
  }, [])
  useLayoutEffect(() => {
    onStateChange({ dirty: editorState.dirty || createEditorState.dirty, busy, discard })
  }, [onStateChange, editorState.dirty, createEditorState.dirty, busy, discard])
  useEffect(() => {
    if (busy || !returnAfterDelete) return
    setReturnAfterDelete(null)
    if (
      returnAfterDelete.locationKey !== locationKey ||
      (selected && selected.uid !== returnAfterDelete.uid) ||
      editorState.dirty ||
      createEditorState.dirty
    )
      return
    setSelection(undefined)
    updateView({ resource: null, resourceNamespace: null, tab: 'instances' }, true)
  })
  const reportEditState = useCallback(
    (next: CRDEditorState) => {
      setEditorState(next)
      // Monaco changes precede the React commit. Block a history event in the same interval.
      if (next.dirty || next.busy) onStateChange({ ...next, discard })
    },
    [discard, onStateChange],
  )
  const switchResource = (action: () => void) => {
    if (busy) return
    onLeave(() => {
      setEditingTarget(null)
      setEditorState({ dirty: false, busy: false })
      action()
    })
  }
  const refresh = () => {
    if (busy || editing || editorState.dirty || createOpen) return
    if (catalogQuery.isEnabled) void catalogQuery.refetch()
    if (resourcesQuery.isEnabled) void resourcesQuery.refetch()
    if (accessQuery.isEnabled) void accessQuery.refetch()
    if (selected && canView)
      void queryClient.invalidateQueries({
        queryKey: crdQueries.yaml(
          {
            clusterId: clusterId!,
            crd,
            namespace: selected.namespace,
            resourceName: selected.name,
          },
          true,
        ).queryKey,
      })
  }
  const refreshControl = (
    <WorkloadRefresh
      settings={refreshSettings}
      resourceLabel={crd.kind}
      queries={[catalogQuery, resourcesQuery, accessQuery]}
      onRefresh={refresh}
      disabled={!clusterId || disabled || busy || editing || editorState.dirty || createOpen}
    />
  )
  const returnToInstances = () =>
    switchResource(() => {
      setSelection(undefined)
      updateView({ resource: null, resourceNamespace: null, tab: 'instances' })
    })
  const openResource = (record: CRDResourceInstance, edit = false) =>
    switchResource(() => {
      setSelection(record)
      setEditingTarget(edit ? resourceKey(record) : null)
      updateView({
        resource: record.name,
        resourceNamespace: record.namespace || '',
        tab: edit ? 'yaml' : 'overview',
      })
    })
  const backButton = (
    <Button
      autoInsertSpace={false}
      icon={<ArrowLeftOutlined />}
      disabled={busy}
      onClick={returnToInstances}
    >
      {zh ? '返回实例列表' : 'Back to instances'}
    </Button>
  )
  const deleteResource = async (record: CRDResourceInstance) => {
    if (!clusterId || !record.uid) return
    const uid = record.uid
    await deleteMutation.mutateAsync({
      clusterId,
      crd,
      namespace: record.namespace ?? namespace,
      resourceName: record.name,
      expectedUid: uid,
    })
    if (resourceName === record.name && resourceNamespace === (record.namespace || ''))
      setReturnAfterDelete({ locationKey, uid })
  }
  const deleteButton = (record: CRDResourceInstance) =>
    !record.deletingAt && hasAllowedAction(record.allowedActions, 'delete') ? (
      <Popconfirm
        title={t('common.deleteConfirm', `Delete ${record.name}?`)}
        description={`${record.name} (${record.namespace || crd.scope})`}
        okText={t('common.delete', 'Delete')}
        cancelText={t('common.cancel', 'Cancel')}
        okButtonProps={{
          disabled: platformWriteBlocked || !record.uid,
          danger: true,
          loading:
            deleteMutation.isPending &&
            deleteMutation.variables?.clusterId === clusterId &&
            deleteMutation.variables.resourceName === record.name &&
            deleteMutation.variables.expectedUid === record.uid,
        }}
        onConfirm={() => {
          if (!clusterId || !record.uid || disabled || busy || editing) return
          void deleteResource(record).catch(() => {
            // Operation feedback owns request errors.
          })
        }}
      >
        <ManagementIconButton
          danger
          icon={<DeleteOutlined />}
          aria-label={t('common.delete', 'Delete')}
          tooltip={t('common.delete', 'Delete')}
          disabled={platformWriteBlocked || disabled || busy || editing || !record.uid}
          loading={deleteMutation.isPending}
        />
      </Popconfirm>
    ) : null
  const resourceActions = useKubernetesResourceActions<CRDResourceInstance>({
    resourceKey: `crd-instances:${crd.name}`,
    resourceLabel: crd.kind,
    records: resources,
    scopeKey: JSON.stringify([clusterId, namespace, crd.group, crd.plural, crd.version]),
    getName: (record) => record.name,
    getKey: (record) => `${resourceKey(record)}/${record.uid ?? ''}`,
    actions: (record) => [
      ...(hasAllowedAction(record.allowedActions, 'update')
        ? [
            {
              key: 'edit',
              label: t('common.edit', 'Edit'),
              icon: <EditOutlined />,
              disabled: platformWriteBlocked || disabled || busy || Boolean(record.deletingAt),
              reason: disabled ? capability.reason : undefined,
              run: () => openResource(record, true),
            },
          ]
        : []),
      ...(!record.deletingAt && hasAllowedAction(record.allowedActions, 'delete')
        ? [
            {
              key: 'delete',
              label: t('common.delete', 'Delete'),
              icon: <DeleteOutlined />,
              danger: true,
              disabled: platformWriteBlocked || disabled || busy || editing || !record.uid,
              reason: !record.uid
                ? zh
                  ? '缺少资源 UID，请刷新后重试。'
                  : 'Missing resource UID. Refresh and retry.'
                : disabled
                  ? capability.reason
                  : undefined,
              loading:
                deleteMutation.isPending &&
                deleteMutation.variables?.clusterId === clusterId &&
                deleteMutation.variables.resourceName === record.name &&
                deleteMutation.variables.expectedUid === record.uid,
              confirm: kubernetesDeleteConfirmation(record, localeCode),
              prepare: () => deleteMutation.reset(),
              feedback: deleteMutation.feedback,
              run: () => deleteResource(record),
            },
          ]
        : []),
    ],
  })

  const columns: TableColumnsType<CRDResourceInstance> = [
    {
      key: 'name',
      dataIndex: 'name',
      title: zh ? '名称' : 'Name',
      width: 240,
      sortOrder: sort.key === 'name' ? sort.order : null,
      render: (_value, record) => (
        <Button
          type="link"
          className="soha-crd-instance-name"
          title={record.name}
          disabled={busy}
          onClick={() => openResource(record)}
        >
          {record.name}
        </Button>
      ),
    },
    ...(crd.scope === 'Namespaced'
      ? [
          {
            key: 'namespace',
            dataIndex: 'namespace',
            title: zh ? '命名空间' : 'Namespace',
            width: 150,
            sortOrder: sort.key === 'namespace' ? sort.order : null,
            render: (value: string) => (
              <button
                className="soha-crd-namespace-filter"
                title={value}
                onClick={() => {
                  setNamespace(value)
                  updateView({ namespace: value })
                }}
              >
                <MetadataTag tone="blue" title={value} label={<span>{value}</span>} />
              </button>
            ),
          },
        ]
      : []),
    {
      key: 'status',
      dataIndex: 'status',
      title: zh ? '状态' : 'Status',
      width: 100,
      sortOrder: sort.key === 'status' ? sort.order : null,
      render: (_value, record) =>
        record.status || record.deletingAt ? (
          <StatusTag value={record.deletingAt ? 'deleting' : record.status!} />
        ) : (
          '—'
        ),
    },
    {
      key: 'age',
      title: zh ? '创建时间' : 'Created',
      width: 110,
      sortOrder: sort.key === 'age' ? sort.order : null,
      sorter: (left, right) => {
        const now = Date.now()
        const a = resourceAge(left, now),
          b = resourceAge(right, now)
        if (a === b) return 0
        if (!Number.isFinite(a)) return 1
        if (!Number.isFinite(b)) return -1
        return b - a
      },
      render: (_value, record) =>
        record.createdAt
          ? formatRelativeTime(record.createdAt)
          : record.ageSeconds == null
            ? '—'
            : formatAgeSeconds(record.ageSeconds),
    },
  ]
  const onTableChange: NonNullable<TableProps<CRDResourceInstance>['onChange']> = (
    _pagination,
    _filters,
    sorter,
  ) => {
    const column = Array.isArray(sorter) ? sorter[0] : sorter
    setSort({
      key: column.columnKey == null ? undefined : String(column.columnKey),
      order: column.order,
    })
  }
  const header = selected ? (
    <ResourceDetailHeader
      hideBack
      kind={selected.kind || crd.kind}
      name={selected.name}
      namespace={selected.namespace}
      status={selected.deletingAt ? 'deleting' : selected.status}
      createdAt={selected.createdAt}
      ageSeconds={selected.ageSeconds}
      facts={<span>{selected.apiVersion || `${crd.group}/${crd.version}`}</span>}
      actions={
        <>
          {backButton}
          <WorkloadRefresh
            settings={refreshSettings}
            resourceLabel={crd.kind}
            queries={[catalogQuery, resourcesQuery, accessQuery]}
            onRefresh={refresh}
            disabled={disabled || busy || editing || editorState.dirty}
            beforeRefresh={
              editing ? (
                <Button disabled={busy} onClick={() => switchResource(() => {})}>
                  {zh ? '取消编辑' : 'Cancel editing'}
                </Button>
              ) : canUpdate ? (
                <Button
                  disabled={platformWriteBlocked || disabled || busy}
                  onClick={() => openResource(selected, true)}
                >
                  {zh ? '编辑 YAML' : 'Edit YAML'}
                </Button>
              ) : undefined
            }
          />
          {deleteButton(selected)}
        </>
      }
    />
  ) : null
  const definition = (
    <Card title={zh ? '定义信息' : 'Definition information'} size="small">
      <Descriptions
        column={1}
        size="small"
        items={[
          { key: 'name', label: 'CRD', children: crd.name },
          { key: 'kind', label: 'Kind', children: crd.kind },
          { key: 'group', label: 'API Group', children: crd.group },
          { key: 'plural', label: zh ? '资源路径' : 'Resource path', children: crd.plural },
          { key: 'scope', label: zh ? '作用域' : 'Scope', children: crd.scope },
          { key: 'version', label: zh ? '当前查询版本' : 'Query version', children: crd.version },
          {
            key: 'versions',
            label: zh ? '支持版本' : 'Served versions',
            children: (
              <div className="soha-crd-inline-tags">
                {getServedVersions(crd).map((version) => (
                  <MetadataTag key={version} label={version} />
                ))}
              </div>
            ),
          },
        ]}
      />
    </Card>
  )
  return (
    <>
      {resourceActions.confirmation}
      <ReadFeedback canRead={accessQuery.isEnabled} query={accessQuery} label="资源权限" />
      {resourceView ? (
        <ReadFeedback
          canRead={resourcesQuery.isEnabled}
          query={resourcesQuery}
          label="自定义资源"
        />
      ) : null}

      {!resourceActions.isConfirming && deleteMutation.feedback}
      {isNamespacedCRD(crd) ? (
        <ReadFeedback
          canRead={namespacesQuery.isEnabled}
          query={namespacesQuery}
          label={zh ? '命名空间' : 'Namespaces'}
        />
      ) : null}

      <div
        className="soha-page soha-workload-detail-page soha-crd-kind-panel"
        hidden={resourceView}
      >
        <ResourceDetailHeader
          hideBack
          kind="CRD"
          name={crd.kind}
          createdAt={crd.createdAt}
          ageSeconds={crd.ageSeconds}
          facts={
            <>
              <MetadataTag label={crd.scope} />
              <span>
                {crd.group}/{crd.version}
              </span>
            </>
          }
          actions={refreshControl}
        />
        <ResourceDetailTabs
          activeKey={
            !resourceView && ['definition', 'relationships'].includes(tab) ? tab : 'instances'
          }
          onChange={(key) => switchResource(() => updateView({ tab: key }, true))}
          items={[
            {
              key: 'instances',
              label: zh ? '实例' : 'Instances',
              children: (
                <div className="soha-crd-instance-list">
                  <ResourceQueryPanel
                    searchKeyword={searchKeyword}
                    setSearchKeyword={(value) => {
                      setSearchKeyword(value)
                    }}
                    placeholder={
                      zh ? '搜索实例 / 命名空间 / 摘要' : 'Search instance / namespace / summary'
                    }
                  >
                    {isNamespacedCRD(crd) ? (
                      <ManagementQueryField label={zh ? '命名空间' : 'Namespace'} width={240}>
                        <Select
                          aria-label={zh ? '选择资源命名空间' : 'Select resource namespace'}
                          disabled={!clusterId || busy}
                          loading={namespacesQuery.isLoading}
                          showSearch={{ optionFilterProp: 'label' }}
                          value={namespace ?? ''}
                          options={[
                            { value: '', label: zh ? '全部命名空间' : 'All namespaces' },
                            ...namespaces.map((item) => ({ value: item.name, label: item.name })),
                          ]}
                          onChange={(value) =>
                            onLeave(() => {
                              setSelection(undefined)
                              setNamespace(value || null)
                              updateView({
                                namespace: value,
                                resource: null,
                                resourceNamespace: null,
                                tab: 'instances',
                              })
                            })
                          }
                        />
                      </ManagementQueryField>
                    ) : null}
                  </ResourceQueryPanel>
                  <KubernetesResourceTable
                    resourceKey={`crd-instances:${crd.group}/${crd.kind}/${crd.version}`}
                    resourceLabel={crd.kind}
                    allRows={resources}
                    filterKey={keyword}
                    scopeMode={isNamespacedCRD(crd) ? 'namespace' : 'cluster'}
                    columns={
                      resourceActions.hasActions ? [...columns, resourceActions.column] : columns
                    }
                    dataSource={filtered}
                    rowKey={resourceKey}
                    scroll={{ x: 'max-content' }}
                    localSorting
                    enableDensity
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    loading={resourcesQuery.isLoading || capability.isLoading}
                    error={resourcesQuery.isEnabled ? resourcesQuery.error : null}
                    onRetry={resourcesQuery.isEnabled ? refresh : undefined}
                    hasReadData={visibleResourcesQueryData !== undefined}
                    shellClassName="soha-management-table-shell"
                    onChange={onTableChange}
                    headerExtra={
                      <ManagementTableToolbar>
                        <Button
                          autoInsertSpace={false}
                          type="primary"
                          icon={<PlusOutlined />}
                          aria-label={zh ? '新建实例' : 'Create instance'}
                          disabled={!canCreate || busy}
                          onClick={() =>
                            switchResource(() => {
                              setCreateNamespace(namespace)
                              setCreateOpen(true)
                            })
                          }
                        >
                          {zh ? '新建实例' : 'Create instance'}
                        </Button>
                      </ManagementTableToolbar>
                    }
                    empty={
                      <ManagementState
                        bordered={false}
                        compact
                        kind={disabled ? 'unsupported' : 'empty'}
                        title={
                          disabled ? undefined : zh ? '暂无自定义资源实例' : 'No custom resources'
                        }
                        description={
                          disabled
                            ? capability.reason
                            : searchKeyword
                              ? zh
                                ? '没有匹配的实例，请调整搜索条件。'
                                : 'No matching instances. Adjust your search.'
                              : undefined
                        }
                      />
                    }
                  />
                </div>
              ),
            },
            { key: 'definition', label: zh ? '定义信息' : 'Definition', children: definition },
            { ...relationshipTab, children: resourceView ? null : relationshipTab.children },
          ]}
        />
      </div>
      {resourceView ? (
        <div className="soha-page soha-workload-detail-page soha-crd-resource-detail">
          {resourcesQuery.isLoading || capability.isLoading ? (
            <Card loading />
          ) : selected && canView ? (
            <>
              {header}
              <ResourceDetailTabs
                activeKey={['yaml', 'relationships'].includes(tab) ? tab : 'overview'}
                onChange={(key) => switchResource(() => updateView({ tab: key }, true))}
                items={[
                  {
                    key: 'overview',
                    label: zh ? '概览' : 'Overview',
                    children: <CRDInstanceOverview crd={crd} resource={selected} />,
                  },
                  relationshipTab,
                  {
                    key: 'yaml',
                    label: 'YAML',
                    children:
                      tab === 'yaml' || editing ? (
                        <Suspense fallback={<Card loading />}>
                          <CRDResourceEditor
                            key={`${resourceKey(selected)}/${editing}`}
                            crd={crd}
                            mode="edit"
                            resource={selected}
                            editable={editing}
                            onStateChange={reportEditState}
                            customResourceMutationsDisabled={disabled}
                            customResourceCapabilityReason={
                              canUpdate ? undefined : capability.reason
                            }
                            onClose={editing ? () => switchResource(() => {}) : undefined}
                          />
                        </Suspense>
                      ) : null,
                  },
                ]}
              />
            </>
          ) : (
            <Card className="soha-detail-card">
              {header || backButton}
              <ManagementState
                bordered={false}
                kind={
                  disabled
                    ? 'unsupported'
                    : resourcesQuery.isError && !visibleResourcesQueryData
                      ? 'error'
                      : selected
                        ? 'no-permission'
                        : 'not-found'
                }
                description={
                  disabled
                    ? capability.reason
                    : resourcesQuery.isError && !visibleResourcesQueryData
                      ? zh
                        ? '实例读取失败，请返回列表重试。'
                        : 'Failed to read instances. Return to the list and retry.'
                      : selected
                        ? zh
                          ? '当前授权不允许查看此实例内容'
                          : 'You cannot view this instance'
                        : zh
                          ? '此实例已不存在，请返回列表刷新。'
                          : 'This instance is no longer available. Return to the list and refresh.'
                }
              />
            </Card>
          )}
        </div>
      ) : null}
      {createOpen ? (
        <Suspense fallback={<Spin />}>
          <CRDResourceEditorModal
            crd={crd}
            namespaceOverride={createNamespace}
            mode="create"
            onClose={() => setCreateOpen(false)}
            onLeave={onLeave}
            onStateChange={setCreateEditorState}
            customResourceMutationsDisabled={disabled}
            customResourceCapabilityReason={canCreate ? undefined : capability.reason}
          />
        </Suspense>
      ) : null}
    </>
  )
}
