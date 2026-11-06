import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementQueryField,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { ReadFeedback } from '@/components/request-feedback'
import { TableCellLink } from '@/components/table-cell-content'
import { podQueries } from '../workloads/pods/queries'
import {
  WorkloadQueryPanel,
  WorkloadSearchInput,
  useWorkloadTableDensity,
} from '../workloads/shared/list-controls'
import { WorkloadRefresh, useWorkloadRefresh } from '../workloads/shared/workload-refresh'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import { parseStringMap, stringifyMap } from '@/features/platform/node-resource-utils'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import '@/features/platform/styles/base.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatAgeSeconds } from '@/utils/time'
import {
  ApartmentOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  SendOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Form, Input, Modal, Select, Tooltip, Typography } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { namespaceMutations } from './mutations'
import { namespaceQueries } from './queries'
import { toClusterScope } from './scope'
import type { ClusterNamespace } from './types'
import { filterNamespaces, namespaceRows, type NamespaceListRow } from './namespace-list-model'
import { NamespaceLabelsCell } from './namespace-labels-cell'
import './namespaces-list.css'

export function ClusterNamespacesPage() {
  const { t, localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const nameCollator = useMemo(
    () => new Intl.Collator(zh ? 'zh-CN' : 'en-US', { numeric: true, sensitivity: 'base' }),
    [zh],
  )
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, setNamespace } = usePlatformScopeStore()
  const scope = toClusterScope(clusterId)
  const [editingNamespace, setEditingNamespace] = useState<ClusterNamespace | null>(null)
  const [namespaceModalVisible, setNamespaceModalVisible] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('')
  const { tableSize, densityButton } = useWorkloadTableDensity(localeCode)
  const refresh = useWorkloadRefresh()
  const namespacesQuery = useQuery({
    ...namespaceQueries.list(scope),
    refetchInterval: refresh.interval,
  })
  const visibleNamespacesQueryData = readableQueryData(namespacesQuery)
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const canCreate = hasPermission(permissionSnapshot, 'platform.namespaces.create')
  const canReadPods =
    Boolean(clusterId) &&
    hasPermission(permissionSnapshot, 'platform.namespaces.view') &&
    hasPermission(permissionSnapshot, 'platform.pods.view')
  const podsQuery = useQuery({
    ...podQueries.list({ clusterId, namespace: null }),
    enabled: canReadPods,
    refetchInterval: refresh.interval,
  })
  const visiblePods = canReadPods && !podsQuery.isError ? readableQueryData(podsQuery) : undefined
  const rows = useMemo(
    () => namespaceRows(visibleNamespacesQueryData ?? [], visiblePods),
    [visibleNamespacesQueryData, visiblePods],
  )
  const filteredRows = useMemo(
    () => filterNamespaces(rows, keyword, status),
    [rows, keyword, status],
  )
  const openNamespace = (name: string, path = '/workloads/overview') => {
    if (!clusterId) return
    setNamespace(name)
    navigate({
      pathname: path,
      search: new URLSearchParams({ clusterId, namespace: name }).toString(),
    })
  }
  const statsHint = !canReadPods
    ? zh
      ? '需要命名空间与 Pod 查看权限'
      : 'Namespace and Pod view permissions are required'
    : podsQuery.isError
      ? zh
        ? 'Pod 统计未获取到，请重试刷新'
        : 'Pod counts unavailable; retry refresh'
      : zh
        ? '当前权限可见的 Pods，包含已完成的 Pods'
        : 'Pods visible to your permissions, including completed Pods'
  const createNamespaceMutation = usePlatformMutation(namespaceMutations.create(queryClient), {
    label: ['创建命名空间', 'Create namespace'],
    target: (v) => platformResourceTarget({ scope: v.scope, name: v.input.name }),
  })
  const updateNamespaceMutation = usePlatformMutation(namespaceMutations.update(queryClient), {
    label: ['更新命名空间', 'Update namespace'],
    target: platformResourceTarget,
  })
  const deleteNamespaceMutation = usePlatformMutation(namespaceMutations.remove(queryClient), {
    label: ['删除命名空间', 'Delete namespace'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked =
    createNamespaceMutation.blocked ||
    updateNamespaceMutation.blocked ||
    deleteNamespaceMutation.blocked

  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: '集群命名空间',
    entityKind: 'kubernetes.namespace-list',
    entityName: clusterId || 'namespaces',
    clusterId: clusterId ?? undefined,
    namespace: editingNamespace?.name,
    pinnedData: {
      namespaceCount: visibleNamespacesQueryData?.length ?? 0,
      editingNamespace: editingNamespace?.name,
    },
    visibleFilters: { keyword: keyword || undefined, status: status || undefined },
  })

  const closeModal = () => {
    setNamespaceModalVisible(false)
    setEditingNamespace(null)
  }

  const namespaceModalInitValues = useMemo(() => {
    if (!editingNamespace) return { labels: '{}', annotations: '{}' }
    return {
      name: editingNamespace.name,
      labels: stringifyMap(editingNamespace.labels),
      annotations: stringifyMap(editingNamespace.annotations),
    }
  }, [editingNamespace])

  useEffect(() => {
    setNamespaceModalVisible(false)
    setEditingNamespace(null)
  }, [clusterId])
  useEffect(() => {
    setKeyword('')
    setStatus('')
  }, [clusterId])
  const resourceActions = useKubernetesResourceActions<ClusterNamespace>({
    resourceKey: 'namespaces',
    resourceLabel: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
    records: visibleNamespacesQueryData ?? [],
    scopeKey: clusterId ?? '',
    getName: (record) => record.name,
    getKey: (record) => record.name,
    actions: (record) => [
      {
        key: 'resources',
        label: localeCode === 'zh_CN' ? '查看资源' : 'View resources',
        icon: <SendOutlined />,
        run: () => {
          openNamespace(record.name)
        },
      },
      {
        key: 'helm',
        label: 'Helm',
        icon: <ApartmentOutlined />,
        run: () => {
          openNamespace(record.name, '/helm/releases')
        },
      },
      ...(hasAllowedAction(record.allowedActions, 'update')
        ? [
            {
              key: 'edit',
              label: localeCode === 'zh_CN' ? '编辑' : 'Edit',
              icon: <EditOutlined />,
              disabled: platformWriteBlocked,
              run: () => {
                setEditingNamespace(record)
                setNamespaceModalVisible(true)
              },
            },
          ]
        : []),
      ...(hasAllowedAction(record.allowedActions, 'delete')
        ? [
            {
              key: 'delete',
              label: localeCode === 'zh_CN' ? '删除' : 'Delete',
              icon: <DeleteOutlined />,
              danger: true,
              disabled: platformWriteBlocked,
              loading:
                deleteNamespaceMutation.isPending &&
                deleteNamespaceMutation.variables?.scope.clusterId === clusterId &&
                deleteNamespaceMutation.variables?.name === record.name,
              confirm: kubernetesDeleteConfirmation(
                record,
                localeCode,
                localeCode === 'zh_CN'
                  ? '删除后该命名空间下的资源会一并回收，请确认。'
                  : 'Resources in this namespace will also be deleted.',
              ),
              prepare: () => deleteNamespaceMutation.reset(),
              feedback: deleteNamespaceMutation.feedback,
              run: () => deleteNamespaceMutation.mutateAsync({ scope, name: record.name }),
            },
          ]
        : []),
    ],
  })

  const columns: TableColumnsType<NamespaceListRow> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      defaultSortOrder: 'ascend',
      sorter: (a, b) => nameCollator.compare(a.name, b.name),
      render: (name: string) => <TableCellLink label={name} onClick={() => openNamespace(name)} />,
    },
    {
      ...tableColumnPresets.status,
      title: zh ? '状态' : 'Status',
      dataIndex: 'status',
      render: (status: string) => <StatusTag value={status} />,
    },
    {
      title: zh ? '可见 Pods' : 'Visible Pods',
      onHeaderCell: () => ({ title: statsHint }),
      dataIndex: 'podCount',
      width: 112,
      render: (count: number | undefined, record) =>
        count === undefined ? (
          <Tooltip title={statsHint}>
            <span>—</span>
          </Tooltip>
        ) : (
          <TableCellLink
            label={String(count)}
            onClick={() => openNamespace(record.name, '/workloads/pods')}
          />
        ),
    },
    {
      title: zh ? '待就绪 / 异常' : 'Not ready / abnormal',
      onHeaderCell: () => ({
        title: zh
          ? '未就绪或异常的可见 Pods；已完成的 Pods 不计入'
          : 'Visible Pods that are not ready or abnormal; completed Pods are excluded',
      }),
      dataIndex: 'attentionCount',
      width: 136,
      render: (count: number | undefined, record) => {
        const unknown = record.unknownReadiness ?? 0
        const label =
          count === undefined || (unknown > 0 && count === 0)
            ? '—'
            : `${unknown ? '≥ ' : ''}${count}`
        return (
          <Tooltip
            title={
              count === undefined
                ? statsHint
                : unknown
                  ? zh
                    ? `${unknown} 个 Pod 缺少就绪信息，仅统计已知异常`
                    : `${unknown} Pods have unknown readiness; only known issues are counted`
                  : zh
                    ? 'Pending、Failed、Unknown 或容器未就绪；已完成不计入'
                    : 'Pending, Failed, Unknown or containers not ready; completed Pods excluded'
            }
          >
            <Typography.Text type={count ? 'warning' : undefined}>{label}</Typography.Text>
          </Tooltip>
        )
      },
    },
    {
      title: zh ? '创建时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 112,
      render: (age: number | undefined) =>
        typeof age === 'number' && Number.isFinite(age) && age >= 0 ? formatAgeSeconds(age) : '—',
    },
    {
      title: zh ? '标签' : 'Labels',
      dataIndex: 'labels',
      width: 320,
      render: (labels: Record<string, string>) => <NamespaceLabelsCell labels={labels} zh={zh} />,
    },
  ]

  return (
    <div className="soha-page soha-namespaces-list">
      {createNamespaceMutation.feedback}
      {updateNamespaceMutation.feedback}
      {resourceActions.confirmation}
      {!resourceActions.isConfirming && deleteNamespaceMutation.feedback}
      <ReadFeedback
        query={{ ...podsQuery, data: undefined }}
        label={zh ? 'Pod 统计' : 'Pod counts'}
        canRead={canReadPods}
      />

      {!clusterId ? (
        <ManagementState
          compact
          kind="select-scope"
          title={t('common.pleaseSelectClusterShort', 'Select a cluster')}
        />
      ) : (
        <>
          <WorkloadQueryPanel
            localeCode={localeCode}
            hasActiveFilters={Boolean(keyword || status)}
            onReset={() => {
              setKeyword('')
              setStatus('')
            }}
          >
            <WorkloadSearchInput
              label={zh ? '关键词' : 'Keyword'}
              placeholder={zh ? '搜索命名空间 / 标签' : 'Search namespace / label'}
              value={keyword}
              onChange={setKeyword}
              width={280}
            />
            <ManagementQueryField label={zh ? '状态' : 'Status'} width={200}>
              <Select
                aria-label={zh ? '命名空间状态' : 'Namespace status'}
                size="small"
                value={status}
                onChange={setStatus}
                options={[
                  { value: '', label: zh ? '全部状态' : 'All statuses' },
                  { value: 'Active', label: 'Active' },
                  { value: 'Terminating', label: 'Terminating' },
                ]}
              />
            </ManagementQueryField>
          </WorkloadQueryPanel>
          <KubernetesResourceTable
            resourceKey={'namespaces'}
            resourceLabel={'Namespaces'}
            allRows={rows}
            filterKey={`${keyword}\u0000${status}`}
            scopeMode={'cluster'}
            error={namespacesQuery.isEnabled === false ? null : namespacesQuery.error}
            hasReadData={namespacesQuery.data !== undefined}
            onRetry={
              namespacesQuery.isEnabled === false ? undefined : () => namespacesQuery.refetch()
            }
            columnSettingIconOnly
            columnSettingPlacement="header"
            shellClassName="soha-management-table-shell"
            columns={[...columns, resourceActions.column]}
            dataSource={filteredRows}
            rowKey="name"
            loading={namespacesQuery.isLoading}
            localSorting
            tableSize={tableSize}
            scroll={{ x: 'max-content' }}
            headerExtra={
              <ManagementTableToolbar>
                {canCreate ? (
                  <Button
                    autoInsertSpace={false}
                    disabled={platformWriteBlocked}
                    icon={<PlusOutlined />}
                    size="small"
                    type="primary"
                    onClick={() => {
                      setEditingNamespace(null)
                      setNamespaceModalVisible(true)
                    }}
                  >
                    {t('common.create', 'Create')}
                  </Button>
                ) : null}
                {densityButton}
                <WorkloadRefresh
                  settings={refresh}
                  queries={[namespacesQuery, podsQuery]}
                  resourceLabel={zh ? '命名空间' : 'Namespaces'}
                  label={t('common.refresh', '刷新')}
                  compact
                  onRefresh={() =>
                    Promise.all([
                      namespacesQuery.refetch(),
                      ...(canReadPods ? [podsQuery.refetch()] : []),
                    ])
                  }
                />
              </ManagementTableToolbar>
            }
          />
        </>
      )}

      <Modal
        okButtonProps={{ disabled: platformWriteBlocked }}
        destroyOnHidden
        title={editingNamespace ? `编辑命名空间 ${editingNamespace.name}` : '新建命名空间'}
        open={namespaceModalVisible}
        footer={null}
        width={720}
        onCancel={() => {
          if (!platformWriteBlocked) closeModal?.()
        }}
      >
        {createNamespaceMutation.feedback}
        {updateNamespaceMutation.feedback}
        {deleteNamespaceMutation.feedback}

        <Form
          key={editingNamespace?.name ?? 'namespace-new'}
          layout="vertical"
          initialValues={namespaceModalInitValues}
          preserve={false}
          onFinish={(values) => {
            if (platformWriteBlocked || !clusterId) return
            if (
              editingNamespace
                ? !(visibleNamespacesQueryData ?? []).some(
                    (item) =>
                      item.name === editingNamespace.name &&
                      hasAllowedAction(item.allowedActions, 'update'),
                  )
                : !canCreate
            )
              return
            const input = {
              name: editingNamespace?.name ?? String(values.name ?? ''),
              labels: parseStringMap(values.labels, 'Labels'),
              annotations: parseStringMap(values.annotations, 'Annotations'),
            }
            const callbacks = {
              onSuccess: () => {
                closeModal()
              },
            }
            if (editingNamespace) {
              updateNamespaceMutation.mutate(
                { scope, name: editingNamespace.name, input },
                callbacks,
              )
            } else {
              createNamespaceMutation.mutate({ scope, input }, callbacks)
            }
          }}
        >
          {!editingNamespace ? (
            <Form.Item
              name="name"
              label="命名空间名称"
              rules={[{ required: true, message: '请输入命名空间名称' }]}
            >
              <Input />
            </Form.Item>
          ) : null}
          <Form.Item name="labels" label="Labels(JSON)">
            <Input.TextArea rows={8} />
          </Form.Item>
          <Form.Item name="annotations" label="Annotations(JSON)">
            <Input.TextArea rows={8} />
          </Form.Item>
          <div className="soha-form-actions">
            <Button onClick={closeModal}>取消</Button>
            <Button
              htmlType="submit"
              type="primary"
              loading={createNamespaceMutation.isPending || updateNamespaceMutation.isPending}
            >
              {editingNamespace ? '保存' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
