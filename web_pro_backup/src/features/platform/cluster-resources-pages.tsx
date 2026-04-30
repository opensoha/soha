import { useMemo, useState } from 'react'
import { App, Button, Card, Empty, Form, Input, Modal, Space, Spin, Tag, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined, SendOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PlatformClusterScopeHint } from '@/components/platform-cluster-scope-hint'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import {
  NodeResourcePanel,
  parseStringMap,
  parseTaints,
  stringifyMap,
  stringifyTaints,
} from '@/features/platform/node-resource-utils'
import type { ApiResponse, Namespace, Node, NodeDetail } from '@/types'
import type { TableColumnsType } from 'antd'

const { Text } = Typography

export function ClusterNodesPage() {
  const { t } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId } = usePlatformScopeStore()
  const [editingNodeName, setEditingNodeName] = useState<string | null>(null)

  const nodesQuery = useQuery({
    queryKey: ['cluster-nodes', clusterId],
    queryFn: () => api.get<ApiResponse<Node[]>>(`/clusters/${clusterId}/infrastructure/nodes`),
    enabled: !!clusterId,
  })

  const nodeDetailQuery = useQuery({
    queryKey: ['cluster-node-detail', clusterId, editingNodeName],
    queryFn: () => api.get<ApiResponse<NodeDetail>>(`/clusters/${clusterId}/infrastructure/nodes/${editingNodeName}/detail`),
    enabled: !!clusterId && !!editingNodeName,
  })

  const updateNodeMutation = useMutation({
    mutationFn: async (values: Record<string, unknown>) => {
      if (!clusterId || !editingNodeName) return
      return api.put<ApiResponse<NodeDetail>>(`/clusters/${clusterId}/infrastructure/nodes/${editingNodeName}`, {
        labels: parseStringMap(values.labels, 'Labels'),
        taints: parseTaints(values.taints),
      })
    },
    onSuccess: () => {
      void message.success('节点配置已更新')
      queryClient.invalidateQueries({ queryKey: ['cluster-nodes', clusterId] })
      queryClient.invalidateQueries({ queryKey: ['cluster-node-detail', clusterId, editingNodeName] })
      setEditingNodeName(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const deleteNodeMutation = useMutation({
    mutationFn: async (nodeName: string) => {
      if (!clusterId) return
      return api.delete(`/clusters/${clusterId}/infrastructure/nodes/${nodeName}`)
    },
    onSuccess: () => {
      void message.success('节点对象已删除')
      queryClient.invalidateQueries({ queryKey: ['cluster-nodes', clusterId] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const nodeDetail = nodeDetailQuery.data?.data
  const nodeModalInitValues = useMemo(() => {
    if (!nodeDetail) return undefined
    return {
      labels: stringifyMap(nodeDetail.labels),
      taints: stringifyTaints(nodeDetail.taints),
    }
  }, [nodeDetail])

  const nodeColumns: TableColumnsType<Node> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string) => (
        <Button type="text" onClick={() => navigate(`/cluster-resources/nodes/${encodeURIComponent(name)}?clusterId=${encodeURIComponent(clusterId || '')}`)}>
          {name}
        </Button>
      ),
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    {
      title: '角色',
      dataIndex: 'roles',
      render: (roles: string[]) => roles?.map((r) => <Tag key={r} className="mr-1">{r}</Tag>) ?? '-',
    },
    { title: 'IP', dataIndex: 'internalIp', render: (value: string) => value || '-' },
    { title: 'Version', dataIndex: 'version', render: (value: string) => value || '-' },
    { title: 'Pods', dataIndex: 'podCount' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'name',
      render: (name: string) => (
        <Space>
          <Button
            size="small"
            type="text"
            onClick={() => navigate(`/cluster-resources/nodes/${encodeURIComponent(name)}?clusterId=${encodeURIComponent(clusterId || '')}`)}
          >
            详情
          </Button>
          <Button size="small" type="text" icon={<EditOutlined />} onClick={() => setEditingNodeName(name)}>
            编辑
          </Button>
          <Button
            size="small"
            type="text"
            danger
            icon={<DeleteOutlined />}
            onClick={() => {
              Modal.confirm({
                title: `确认删除节点 ${name}？`,
                content: '这会删除 Kubernetes 中的 Node 对象，不会自动回收底层机器。',
                onOk: () => deleteNodeMutation.mutate(name),
              })
            }}
          >
            删除
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PlatformScopeToolbar />
      <PlatformClusterScopeHint resourceLabel="节点" />
      {!clusterId ? (
        <Empty description={t('common.pleaseSelectCluster', 'Please select a cluster')} />
      ) : (
        <>
          <Card>
            <Text type="secondary" className="text-xs">
              新增节点需要通过云平台、虚拟机编排或集群扩容工具完成；这里支持编辑 labels/taints 和删除 Node 对象。
            </Text>
          </Card>
          <AdminTable
            columns={nodeColumns}
            dataSource={nodesQuery.data?.data ?? []}
            rowKey="name"
            loading={nodesQuery.isLoading}
            pageSize={10}
            expandedRowRender={(record: Node) => <NodeResourcePanel node={record} />}
            hideExpandedColumn={false}
          />
        </>
      )}

      <Modal
        title={editingNodeName ? `编辑节点 ${editingNodeName}` : '编辑节点'}
        open={!!editingNodeName}
        footer={null}
        width={720}
        onCancel={() => setEditingNodeName(null)}
      >
        {nodeDetailQuery.isLoading && !nodeDetail ? (
          <div className="flex items-center justify-center h-48">
            <Spin size="large" />
          </div>
        ) : (
          <Form key={editingNodeName ?? 'node'} layout="vertical" initialValues={nodeModalInitValues} onFinish={(values) => updateNodeMutation.mutate(values)}>
            <Form.Item name="labels" label="Labels(JSON)">
              <Input.TextArea rows={8} />
            </Form.Item>
            <Form.Item name="taints" label="Taints(JSON Array)">
              <Input.TextArea rows={8} />
            </Form.Item>
            <div className="kc-form-actions">
              <Button onClick={() => setEditingNodeName(null)}>取消</Button>
              <Button htmlType="submit" type="primary" loading={updateNodeMutation.isPending}>
                保存
              </Button>
            </div>
          </Form>
        )}
      </Modal>
    </div>
  )
}

export function ClusterNamespacesPage() {
  const { t } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, setNamespace } = usePlatformScopeStore()
  const [editingNamespace, setEditingNamespace] = useState<Namespace | null>(null)
  const [namespaceModalVisible, setNamespaceModalVisible] = useState(false)

  const namespacesQuery = useQuery({
    queryKey: ['cluster-namespaces', clusterId],
    queryFn: () => api.get<ApiResponse<Namespace[]>>(`/clusters/${clusterId}/namespaces`),
    enabled: !!clusterId,
  })

  const createNamespaceMutation = useMutation({
    mutationFn: async (values: Record<string, unknown>) => {
      if (!clusterId) return
      return api.post<ApiResponse<Namespace>>(`/clusters/${clusterId}/namespaces`, {
        name: values.name,
        labels: parseStringMap(values.labels, 'Labels'),
        annotations: parseStringMap(values.annotations, 'Annotations'),
      })
    },
    onSuccess: () => {
      void message.success('命名空间已创建')
      queryClient.invalidateQueries({ queryKey: ['cluster-namespaces', clusterId] })
      setNamespaceModalVisible(false)
      setEditingNamespace(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const updateNamespaceMutation = useMutation({
    mutationFn: async (values: Record<string, unknown>) => {
      if (!clusterId || !editingNamespace) return
      return api.put<ApiResponse<Namespace>>(`/clusters/${clusterId}/namespaces/${editingNamespace.name}`, {
        name: editingNamespace.name,
        labels: parseStringMap(values.labels, 'Labels'),
        annotations: parseStringMap(values.annotations, 'Annotations'),
      })
    },
    onSuccess: () => {
      void message.success('命名空间已更新')
      queryClient.invalidateQueries({ queryKey: ['cluster-namespaces', clusterId] })
      setNamespaceModalVisible(false)
      setEditingNamespace(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const deleteNamespaceMutation = useMutation({
    mutationFn: async (namespaceName: string) => {
      if (!clusterId) return
      return api.delete(`/clusters/${clusterId}/namespaces/${namespaceName}`)
    },
    onSuccess: () => {
      void message.success('命名空间已删除')
      queryClient.invalidateQueries({ queryKey: ['cluster-namespaces', clusterId] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const namespaceModalInitValues = useMemo(() => {
    if (!editingNamespace) {
      return { labels: '{}', annotations: '{}' }
    }
    return {
      name: editingNamespace.name,
      labels: stringifyMap(editingNamespace.labels),
      annotations: stringifyMap(editingNamespace.annotations),
    }
  }, [editingNamespace])

  const nsColumns: TableColumnsType<Namespace> = [
    { title: '名称', dataIndex: 'name' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    {
      title: '标签',
      dataIndex: 'labels',
      render: (labels: Record<string, string>) =>
        labels
          ? Object.entries(labels).slice(0, 3).map(([k, v]) => (
              <Tag key={k} className="mr-1">{`${k}=${v}`}</Tag>
            ))
          : '-',
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'name',
      render: (name: string, record: Namespace) => (
        <Space>
          <Button
            size="small"
            type="text"
            icon={<SendOutlined />}
            onClick={() => {
              setNamespace(name)
              navigate('/workloads/overview')
            }}
          >
            查看资源
          </Button>
          <Button
            size="small"
            type="text"
            onClick={() => {
              setNamespace(name)
              navigate('/helm/releases')
            }}
          >
            Helm
          </Button>
          <Button
            size="small"
            type="text"
            icon={<EditOutlined />}
            onClick={() => {
              setEditingNamespace(record)
              setNamespaceModalVisible(true)
            }}
          >
            编辑
          </Button>
          <Button
            size="small"
            type="text"
            danger
            icon={<DeleteOutlined />}
            onClick={() => {
              Modal.confirm({
                title: `确认删除命名空间 ${name}？`,
                content: '删除后该命名空间下的资源会一并回收，请确认。',
                onOk: () => deleteNamespaceMutation.mutate(name),
              })
            }}
          >
            删除
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PlatformScopeToolbar />
      {!clusterId ? (
        <Empty description={t('common.pleaseSelectClusterShort', 'Select a cluster')} />
      ) : (
        <AdminTable
          columns={nsColumns}
          dataSource={namespacesQuery.data?.data ?? []}
          rowKey="name"
          loading={namespacesQuery.isLoading}
          pageSize={10}
          title={t('page.namespaces.title', 'Namespaces')}
          headerExtra={(
            <Button
              icon={<PlusOutlined />}
              type="primary"
              onClick={() => {
                setEditingNamespace(null)
                setNamespaceModalVisible(true)
              }}
            >
              {t('common.create', 'Create')}
            </Button>
          )}
        />
      )}

      <Modal
        title={editingNamespace ? `编辑命名空间 ${editingNamespace.name}` : '新建命名空间'}
        open={namespaceModalVisible}
        footer={null}
        width={720}
        onCancel={() => {
          setNamespaceModalVisible(false)
          setEditingNamespace(null)
        }}
      >
        <Form
          key={editingNamespace?.name ?? 'namespace-new'}
          layout="vertical"
          initialValues={namespaceModalInitValues}
          onFinish={(values) => {
            if (editingNamespace) {
              updateNamespaceMutation.mutate(values)
            } else {
              createNamespaceMutation.mutate(values)
            }
          }}
        >
          {!editingNamespace ? (
            <Form.Item name="name" label="命名空间名称" rules={[{ required: true, message: '请输入命名空间名称' }]}>
              <Input />
            </Form.Item>
          ) : null}
          <Form.Item name="labels" label="Labels(JSON)">
            <Input.TextArea rows={8} />
          </Form.Item>
          <Form.Item name="annotations" label="Annotations(JSON)">
            <Input.TextArea rows={8} />
          </Form.Item>
          <div className="kc-form-actions">
            <Button
              onClick={() => {
                setNamespaceModalVisible(false)
                setEditingNamespace(null)
              }}
            >
              取消
            </Button>
            <Button htmlType="submit" type="primary" loading={createNamespaceMutation.isPending || updateNamespaceMutation.isPending}>
              {editingNamespace ? '保存' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
