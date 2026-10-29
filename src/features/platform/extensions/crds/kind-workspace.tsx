import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementIconButton,
  ManagementQueryField,
  ManagementSearchableListPane,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
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
import { formatAgeSeconds, formatRelativeTime } from '@/utils/time'
import { ArrowLeftOutlined, DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType, TableProps } from 'antd'
import { App, Button, Card, Descriptions, Popconfirm, Select, Space, Spin } from 'antd'
import { lazy, Suspense, useDeferredValue, useEffect, useState } from 'react'
import { ResourceQueryPanel } from '../shared/resource-query-panel'
import { includesSearch, normalizeSearchKeyword } from '../shared/search'
import './instance-workspace.css'
import { crdMutations } from './mutations'
import { crdQueries } from './queries'
import type { CRD, CRDResourceInstance } from './types'
import { getServedVersions, isNamespacedCRD } from './utils'

const CRDResourceEditorModal = lazy(async () => ({
  default: (await import('./resource-editor-modal')).CRDResourceEditorModal,
}))
const CRDResourceEditor = lazy(async () => ({
  default: (await import('./resource-editor-modal')).CRDResourceEditor,
}))
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
}: {
  crd: CRD
  kinds: CRD[]
  onKindSelect: (name: string) => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { modal } = App.useApp()
  const [kindSearch, setKindSearch] = useState('')
  const [editorState, setEditorState] = useState({ dirty: false, busy: false })
  const keyword = normalizeSearchKeyword(useDeferredValue(kindSearch))
  const leaveResource = (action: () => void) => {
    if (editorState.busy) return
    const switchNow = () => {
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
          onStateChange={setEditorState}
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
  onStateChange: (state: { dirty: boolean; busy: boolean }) => void
}) {
  const { t, localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { clusterId, namespace, setNamespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const capability = useClusterCapability('custom.resources', localeCode)
  const [createOpen, setCreateOpen] = useState(false)
  const [selection, setSelection] = useState<CRDResourceInstance>()
  const [editing, setEditing] = useState(false)
  const [editorState, setEditorState] = useState({ dirty: false, busy: false })
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
  const resourcesQuery = useQuery(
    crdQueries.resources(clusterId, crd, namespace, !capability.isLoading && !disabled),
  )
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
  const selected = resourcesReadable
    ? (resources.find((record) => selection && resourceKey(record) === resourceKey(selection)) ??
      (editing || resourcesQuery.isFetching || resourcesQuery.isError ? selection : undefined))
    : undefined
  const canView = ['view', 'update'].some((action) =>
    hasAllowedAction(selected?.allowedActions, action),
  )
  const canUpdate =
    selected && !selected.deletingAt && hasAllowedAction(selected.allowedActions, 'update')
  const busy = editorState.busy || deleteMutation.isPending
  useEffect(() => {
    onStateChange({ dirty: editorState.dirty, busy })
  }, [onStateChange, editorState.dirty, busy])
  const switchResource = (action: () => void) => {
    if (busy) return
    onLeave(() => {
      setEditing(false)
      setEditorState({ dirty: false, busy: false })
      action()
    })
  }
  const refresh = () => {
    if (resourcesQuery.isEnabled) void resourcesQuery.refetch()
    if (accessQuery.isEnabled) void accessQuery.refetch()
  }
  const openResource = (record: CRDResourceInstance, edit = false) =>
    switchResource(() => {
      setSelection(record)
      setEditing(edit)
    })
  const backButton = (
    <Button
      autoInsertSpace={false}
      icon={<ArrowLeftOutlined />}
      disabled={busy}
      onClick={() => switchResource(() => setSelection(undefined))}
    >
      {zh ? '返回实例列表' : 'Back to instances'}
    </Button>
  )
  const deleteButton = (record: CRDResourceInstance) =>
    !record.deletingAt && hasAllowedAction(record.allowedActions, 'delete') ? (
      <Popconfirm
        title={t('common.deleteConfirm', `Delete ${record.name}?`)}
        description={`${record.name} (${record.namespace || crd.scope})`}
        okText={t('common.delete', 'Delete')}
        cancelText={t('common.cancel', 'Cancel')}
        okButtonProps={{
          disabled: platformWriteBlocked,
          danger: true,
          loading: deleteMutation.isPending,
        }}
        onConfirm={() => {
          if (!clusterId || disabled || busy || editing) return
          deleteMutation.mutate(
            {
              clusterId,
              crd,
              namespace: record.namespace ?? namespace,
              resourceName: record.name,
              expectedUid: record.uid,
            },
            {
              onSuccess: () => {
                if (selection && resourceKey(selection) === resourceKey(record))
                  setSelection(undefined)
              },
            },
          )
        }}
      >
        <ManagementIconButton
          danger
          icon={<DeleteOutlined />}
          aria-label={t('common.delete', 'Delete')}
          tooltip={t('common.delete', 'Delete')}
          disabled={platformWriteBlocked || disabled || busy || editing}
          loading={deleteMutation.isPending}
        />
      </Popconfirm>
    ) : null
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
    {
      key: 'actions',
      render: (_value, record) => (
        <Space size={0} className="soha-row-action-icons">
          <ManagementIconButton
            icon={<EditOutlined />}
            aria-label={t('common.edit', 'Edit')}
            tooltip={t('common.edit', 'Edit')}
            disabled={
              disabled ||
              busy ||
              Boolean(record.deletingAt) ||
              !hasAllowedAction(record.allowedActions, 'update')
            }
            onClick={() => openResource(record, true)}
          />
          {deleteButton(record)}
        </Space>
      ),
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
    <div className="soha-crd-resource-heading">
      <div className="soha-crd-resource-identity">
        <div className="soha-crd-resource-title">
          <h2>{selected.name}</h2>
          {selected.status || selected.deletingAt ? (
            <StatusTag value={selected.deletingAt ? 'deleting' : selected.status!} />
          ) : null}
        </div>
        <span className="soha-crd-resource-secondary">
          {selected.kind || crd.kind} · {selected.namespace || (zh ? '集群范围' : 'Cluster scope')}{' '}
          ·{' '}
          {selected.createdAt
            ? formatRelativeTime(selected.createdAt)
            : selected.ageSeconds == null
              ? '—'
              : formatAgeSeconds(selected.ageSeconds)}
        </span>
      </div>
      <ManagementTableToolbar>
        {backButton}
        {editing ? (
          <Button disabled={busy} onClick={() => switchResource(() => {})}>
            {zh ? '取消编辑' : 'Cancel editing'}
          </Button>
        ) : (
          <ManagementIconButton
            icon={<EditOutlined />}
            aria-label={t('common.edit', 'Edit')}
            tooltip={t('common.edit', 'Edit')}
            disabled={platformWriteBlocked || !canUpdate || disabled || busy}
            onClick={() => {
              setSelection(selected)
              setEditing(true)
            }}
          />
        )}
        {deleteButton(selected)}
      </ManagementTableToolbar>
      <Descriptions
        className="soha-crd-resource-facts"
        column={1}
        size="small"
        items={[
          {
            key: 'api',
            label: zh ? 'API 版本' : 'API version',
            children: selected.apiVersion || `${crd.group}/${crd.version}`,
          },
          { key: 'crd', label: zh ? '资源定义' : 'CRD definition', children: crd.name },
          ...Object.entries(selected.summary ?? {}).map(([key, value]) => ({
            key: `summary/${key}`,
            label: key,
            children: value == null ? '—' : String(value),
          })),
        ]}
      />
      <details className="soha-crd-resource-metadata">
        <summary>{zh ? '更多元数据' : 'More metadata'}</summary>
        <Descriptions
          className="soha-crd-resource-facts"
          column={1}
          size="small"
          items={[
            {
              key: 'versions',
              label: zh ? '支持版本' : 'Served versions',
              children: getServedVersions(crd).join(', '),
            },
            {
              key: 'finalizers',
              label: 'Finalizers',
              children: selected.finalizers?.length ? (
                <div className="soha-crd-resource-finalizers">
                  {selected.finalizers.map((value) => (
                    <span key={value}>{value}</span>
                  ))}
                </div>
              ) : (
                '—'
              ),
            },
          ]}
        />
      </details>
    </div>
  ) : null
  return (
    <>
      <ReadFeedback canRead={accessQuery.isEnabled} query={accessQuery} label="资源权限" />
      {selection ? (
        <ReadFeedback
          canRead={resourcesQuery.isEnabled}
          query={resourcesQuery}
          label="自定义资源"
        />
      ) : null}

      {deleteMutation.feedback}
      {isNamespacedCRD(crd) ? (
        <ReadFeedback
          canRead={namespacesQuery.isEnabled}
          query={namespacesQuery}
          label={zh ? '命名空间' : 'Namespaces'}
        />
      ) : null}

      <div className="soha-crd-instance-list" hidden={Boolean(selection)}>
        <ResourceQueryPanel
          searchKeyword={searchKeyword}
          setSearchKeyword={(value) => {
            setSearchKeyword(value)
          }}
          placeholder={zh ? '搜索实例 / 命名空间 / 摘要' : 'Search instance / namespace / summary'}
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
          columns={columns}
          dataSource={filtered}
          rowKey={resourceKey}
          scroll={{ x: 'max-content' }}
          localSorting
          enableDensity
          columnSettingIconOnly
          columnSettingPlacement="header"
          onRefresh={refresh}
          refreshing={resourcesQuery.isFetching}
          loading={resourcesQuery.isLoading || capability.isLoading}
          error={resourcesQuery.isEnabled ? resourcesQuery.error : null}
          onRetry={resourcesQuery.isEnabled ? refresh : undefined}
          hasReadData={visibleResourcesQueryData !== undefined}
          shellClassName="soha-management-table-shell"
          onChange={onTableChange}
          headerExtra={
            <Button
              autoInsertSpace={false}
              type="primary"
              icon={<PlusOutlined />}
              aria-label={zh ? '新建实例' : 'Create instance'}
              disabled={!canCreate || busy}
              onClick={() => switchResource(() => setCreateOpen(true))}
            >
              {zh ? '新建实例' : 'Create instance'}
            </Button>
          }
          empty={
            <ManagementState
              bordered={false}
              compact
              kind={disabled ? 'unsupported' : 'empty'}
              title={disabled ? undefined : zh ? '暂无自定义资源实例' : 'No custom resources'}
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
      {selection ? (
        <>
          {selected && canView ? (
            <Suspense fallback={<Card loading />}>
              <CRDResourceEditor
                key={`${resourceKey(selected)}/${editing}`}
                crd={crd}
                mode="edit"
                resource={selected}
                editable={editing}
                header={header}
                onStateChange={setEditorState}
                customResourceMutationsDisabled={disabled}
                customResourceCapabilityReason={canUpdate ? undefined : capability.reason}
                onClose={editing ? () => setEditing(false) : undefined}
              />
            </Suspense>
          ) : (
            <Card className="soha-detail-card">
              {header || backButton}
              <ManagementState
                bordered={false}
                kind={selected ? 'no-permission' : 'not-found'}
                description={
                  selected
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
        </>
      ) : null}
      {createOpen ? (
        <Suspense fallback={<Spin />}>
          <CRDResourceEditorModal
            crd={crd}
            mode="create"
            onClose={() => setCreateOpen(false)}
            customResourceMutationsDisabled={disabled}
            customResourceCapabilityReason={canCreate ? undefined : capability.reason}
          />
        </Suspense>
      ) : null}
    </>
  )
}
