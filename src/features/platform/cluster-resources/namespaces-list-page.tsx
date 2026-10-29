import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
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
import {
  ApartmentOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  SendOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Form, Input, Modal, Popconfirm, Space } from 'antd'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { namespaceMutations } from './mutations'
import { namespaceQueries } from './queries'
import { toClusterScope } from './scope'
import type { ClusterNamespace } from './types'

export function ClusterNamespacesPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, setNamespace } = usePlatformScopeStore()
  const scope = toClusterScope(clusterId)
  const [editingNamespace, setEditingNamespace] = useState<ClusterNamespace | null>(null)
  const [namespaceModalVisible, setNamespaceModalVisible] = useState(false)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const namespacesQuery = useQuery(namespaceQueries.list(scope))
  const visibleNamespacesQueryData = readableQueryData(namespacesQuery)
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const canCreate = hasPermission(permissionSnapshot, 'platform.namespaces.create')
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

  const columns: TableColumnsType<ClusterNamespace> = [
    { title: '名称', dataIndex: 'name' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (status: string) => <StatusTag value={status} />,
    },
    {
      title: '标签',
      dataIndex: 'labels',
      render: (labels: Record<string, string>) =>
        labels
          ? Object.entries(labels)
              .slice(0, 3)
              .map(([key, value]) => <MetadataTag key={key} label={`${key}=${value}`} />)
          : '-',
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'name',
      width: 150,
      render: (name: string, record) => {
        const deleting =
          deleteNamespaceMutation.isPending && deleteNamespaceMutation.variables?.name === name
        const canUpdate = hasAllowedAction(record.allowedActions, 'update')
        const canDelete = hasAllowedAction(record.allowedActions, 'delete')
        return (
          <Space size={2} className="soha-row-action-icons">
            <ManagementIconButton
              aria-label={`查看命名空间 ${name} 资源`}
              size="small"
              tooltip="查看资源"
              icon={<SendOutlined />}
              onClick={() => {
                setNamespace(name)
                navigate('/workloads/overview')
              }}
            />
            <ManagementIconButton
              aria-label={`查看命名空间 ${name} Helm`}
              size="small"
              tooltip="Helm"
              icon={<ApartmentOutlined />}
              onClick={() => {
                setNamespace(name)
                navigate('/helm/releases')
              }}
            />
            {canUpdate ? (
              <ManagementIconButton
                disabled={platformWriteBlocked}
                aria-label={`编辑命名空间 ${name}`}
                size="small"
                tooltip="编辑"
                icon={<EditOutlined />}
                onClick={() => {
                  setEditingNamespace(record)
                  setNamespaceModalVisible(true)
                }}
              />
            ) : null}
            {canDelete ? (
              <Popconfirm
                title={`确认删除命名空间 ${name}？`}
                description="删除后该命名空间下的资源会一并回收，请确认。"
                okText="删除"
                cancelText="取消"
                okButtonProps={{ disabled: platformWriteBlocked, danger: true, loading: deleting }}
                placement="topRight"
                onConfirm={() => deleteNamespaceMutation.mutate({ scope, name }, {})}
              >
                <ManagementIconButton
                  disabled={platformWriteBlocked}
                  aria-label={`删除命名空间 ${name}`}
                  danger
                  icon={<DeleteOutlined />}
                  loading={deleting}
                  size="small"
                  tooltip="删除"
                />
              </Popconfirm>
            ) : null}
          </Space>
        )
      },
    },
  ]

  return (
    <div className="soha-page">
      {createNamespaceMutation.feedback}
      {updateNamespaceMutation.feedback}
      {deleteNamespaceMutation.feedback}

      {!clusterId ? (
        <ManagementState
          compact
          kind="select-scope"
          title={t('common.pleaseSelectClusterShort', 'Select a cluster')}
        />
      ) : (
        <KubernetesResourceTable
          resourceKey={'namespaces'}
          resourceLabel={'Namespaces'}
          allRows={visibleNamespacesQueryData ?? []}
          filterKey={''}
          scopeMode={'cluster'}
          error={namespacesQuery.isEnabled === false ? null : namespacesQuery.error}
          hasReadData={namespacesQuery.data !== undefined}
          onRetry={
            namespacesQuery.isEnabled === false ? undefined : () => namespacesQuery.refetch()
          }
          columnSettingIconOnly
          columnSettingPlacement="header"
          shellClassName="soha-management-table-shell"
          columns={columns}
          dataSource={visibleNamespacesQueryData ?? []}
          rowKey="name"
          loading={namespacesQuery.isLoading || namespacesQuery.isFetching}
          localSorting
          tableSize={tableSize}
          scroll={{ x: 'max-content' }}
          headerExtra={
            <ManagementTableToolbar>
              {canCreate ? (
                <Button
                  autoInsertSpace={false}
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
              <ManagementDensityButton
                aria-label={t('common.tableDensity', '切换表格密度')}
                title={t('common.tableDensity', '切换表格密度')}
                tooltip={t('common.tableDensity', '切换表格密度')}
                onClick={() =>
                  setTableSize((current) => (current === 'small' ? 'middle' : 'small'))
                }
              />
              <ManagementRefreshButton
                aria-label={t('common.refresh', '刷新')}
                loading={namespacesQuery.isFetching}
                title={t('common.refresh', '刷新')}
                tooltip={t('common.refresh', '刷新')}
                onClick={() => void namespacesQuery.refetch()}
              />
            </ManagementTableToolbar>
          }
        />
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
