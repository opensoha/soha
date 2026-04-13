import { useMemo, useState } from 'react'
import { Button, Card, Empty, Form, Modal, Progress, Space, Spin, Tag, Toast, Typography } from '@douyinfe/semi-ui'
import { IconDelete, IconEdit, IconPlus, IconSend } from '@douyinfe/semi-icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PageHeader } from '@/components/page-header'
import { PlatformClusterScopeHint } from '@/components/platform-cluster-scope-hint'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, Namespace } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

const { Text } = Typography

interface Node {
  name: string
  status: string
  roles: string[]
  version?: string
  internalIp?: string
  podCount: number
  ageSeconds: number
  resources?: {
    allocatable?: {
      cpu?: string
      memory?: string
      ephemeralStorage?: string
      pods?: string
    }
    requests?: {
      cpu?: string
      memory?: string
      ephemeralStorage?: string
    }
    requestPercentages?: {
      cpu?: number
      memory?: number
      ephemeralStorage?: number
      pods?: number
    }
    usagePercentages?: {
      pods?: number
    }
  }
}

interface NodeDetail extends Node {
  labels?: Record<string, string>
  annotations?: Record<string, string>
  taints?: Array<{
    key: string
    value?: string
    effect: string
  }>
}

function stringifyMap(value?: Record<string, string>) {
  return JSON.stringify(value ?? {}, null, 2)
}

function parseStringMap(raw: unknown, field: string) {
  const value = typeof raw === 'string' ? raw.trim() : ''
  if (!value) return {}
  try {
    const parsed = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('invalid')
    }
    return Object.fromEntries(
      Object.entries(parsed as Record<string, unknown>).map(([key, item]) => [key, String(item ?? '')]),
    )
  } catch {
    throw new Error(`${field} 需要是合法 JSON 对象`)
  }
}

function stringifyTaints(value?: Array<{ key: string; value?: string; effect: string }>) {
  return JSON.stringify(value ?? [], null, 2)
}

function parseTaints(raw: unknown) {
  const value = typeof raw === 'string' ? raw.trim() : ''
  if (!value) return []
  try {
    const parsed = JSON.parse(value)
    if (!Array.isArray(parsed)) {
      throw new Error('invalid')
    }
    return parsed
      .filter((item) => item && typeof item === 'object')
      .map((item) => ({
        key: String((item as Record<string, unknown>).key ?? '').trim(),
        value: String((item as Record<string, unknown>).value ?? '').trim(),
        effect: String((item as Record<string, unknown>).effect ?? '').trim(),
      }))
      .filter((item) => item.key && item.effect)
  } catch {
    throw new Error('污点需要是合法 JSON 数组')
  }
}

function clampPercent(value?: number) {
  if (value == null || Number.isNaN(value)) return 0
  return Math.max(0, Math.min(100, Number(value.toFixed(1))))
}

function parseCpuCores(value?: string) {
  if (!value) return null
  const normalized = value.trim()
  if (!normalized) return null
  if (normalized.endsWith('m')) {
    const parsed = Number.parseFloat(normalized.slice(0, -1))
    return Number.isNaN(parsed) ? null : parsed / 1000
  }
  const parsed = Number.parseFloat(normalized)
  return Number.isNaN(parsed) ? null : parsed
}

function formatCpu(value?: string) {
  const cores = parseCpuCores(value)
  if (cores == null) return '-'
  if (cores >= 10) return `${cores.toFixed(0)} Core`
  if (cores >= 1) return `${cores.toFixed(1)} Core`
  return `${cores.toFixed(2)} Core`
}

function parseBytes(value?: string) {
  if (!value) return null
  const normalized = value.trim()
  if (!normalized) return null
  const match = normalized.match(/^([\d.]+)(Ki|Mi|Gi|Ti|Pi|Ei)?$/)
  if (!match) return null
  const amount = Number.parseFloat(match[1])
  if (Number.isNaN(amount)) return null
  const unit = match[2] ?? ''
  const factors: Record<string, number> = {
    '': 1,
    Ki: 1024,
    Mi: 1024 ** 2,
    Gi: 1024 ** 3,
    Ti: 1024 ** 4,
    Pi: 1024 ** 5,
    Ei: 1024 ** 6,
  }
  return amount * factors[unit]
}

function formatBytesAsG(value?: string) {
  const bytes = parseBytes(value)
  if (bytes == null) return '-'
  const gib = bytes / 1024 ** 3
  if (gib >= 10) return `${gib.toFixed(0)}G`
  if (gib >= 1) return `${gib.toFixed(1)}G`
  return `${gib.toFixed(2)}G`
}

function resolveProgressStroke(percent: number) {
  if (percent >= 85) return 'var(--semi-color-danger)'
  if (percent >= 60) return 'var(--semi-color-warning)'
  return 'var(--semi-color-success)'
}

function ResourceProgressCell({
  primary,
  secondary,
  percent,
  ariaLabel,
}: {
  primary: string
  secondary: string
  percent: number
  ariaLabel: string
}) {
  const value = clampPercent(percent)
  return (
    <div className="kc-resource-cell">
      <div className="kc-resource-cell-copy">
        <Text strong>{primary}</Text>
        <Text type="tertiary" size="small">{secondary}</Text>
      </div>
      <Progress
        percent={value}
        showInfo
        size="large"
        stroke={resolveProgressStroke(value)}
        format={(current) => `${current}%`}
        aria-label={ariaLabel}
      />
    </div>
  )
}

function NodeResourcePanel({ node }: { node: Node }) {
  return (
    <div className="kc-node-expand-grid">
      <ResourceProgressCell
        primary={`${formatCpu(node.resources?.requests?.cpu)} / ${formatCpu(node.resources?.allocatable?.cpu)}`}
        secondary="CPU 已分配 / 总量"
        percent={node.resources?.requestPercentages?.cpu ?? 0}
        ariaLabel={`cpu allocation for ${node.name}`}
      />
      <ResourceProgressCell
        primary={`${formatBytesAsG(node.resources?.requests?.memory)} / ${formatBytesAsG(node.resources?.allocatable?.memory)}`}
        secondary="内存已分配 / 总量"
        percent={node.resources?.requestPercentages?.memory ?? 0}
        ariaLabel={`memory allocation for ${node.name}`}
      />
      <ResourceProgressCell
        primary={`${formatBytesAsG(node.resources?.requests?.ephemeralStorage)} / ${formatBytesAsG(node.resources?.allocatable?.ephemeralStorage)}`}
        secondary="磁盘已分配 / 总量"
        percent={node.resources?.requestPercentages?.ephemeralStorage ?? 0}
        ariaLabel={`disk allocation for ${node.name}`}
      />
      <ResourceProgressCell
        primary={`${node.podCount} / ${node.resources?.allocatable?.pods || '-'}`}
        secondary="Pods 已用 / 总量"
        percent={node.resources?.usagePercentages?.pods ?? 0}
        ariaLabel={`pod allocation for ${node.name}`}
      />
    </div>
  )
}

export function ClusterNodesPage() {
  const { t } = useI18n()
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
      Toast.success('节点配置已更新')
      queryClient.invalidateQueries({ queryKey: ['cluster-nodes', clusterId] })
      queryClient.invalidateQueries({ queryKey: ['cluster-node-detail', clusterId, editingNodeName] })
      setEditingNodeName(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteNodeMutation = useMutation({
    mutationFn: async (nodeName: string) => {
      if (!clusterId) return
      return api.delete(`/clusters/${clusterId}/infrastructure/nodes/${nodeName}`)
    },
    onSuccess: () => {
      Toast.success('节点对象已删除')
      queryClient.invalidateQueries({ queryKey: ['cluster-nodes', clusterId] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const nodeDetail = nodeDetailQuery.data?.data
  const nodeModalInitValues = useMemo(() => {
    if (!nodeDetail) return undefined
    return {
      labels: stringifyMap(nodeDetail.labels),
      taints: stringifyTaints(nodeDetail.taints),
    }
  }, [nodeDetail])

  const nodeColumns: ColumnProps<Node>[] = [
    { title: '名称', dataIndex: 'name' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    {
      title: '角色',
      dataIndex: 'roles',
      render: (roles: string[]) => roles?.map((r) => <Tag key={r} size="small" className="mr-1">{r}</Tag>) ?? '-',
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
          <Button size="small" theme="borderless" icon={<IconEdit />} onClick={() => setEditingNodeName(name)}>
            编辑
          </Button>
          <Button
            size="small"
            theme="borderless"
            type="danger"
            icon={<IconDelete />}
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
      <PageHeader title={t('page.nodes.title', 'Node Resources')} description={t('page.nodes.desc', 'Inspect node resources in the current cluster scope and edit labels / taints.')} />
      <PlatformScopeToolbar />
      <PlatformClusterScopeHint resourceLabel="节点" />
      {!clusterId ? (
        <Empty description={t('common.pleaseSelectCluster', 'Please select a cluster')} />
      ) : (
        <>
          <Card>
            <Text type="tertiary" size="small">
              新增节点需要通过云平台、虚拟机编排或集群扩容工具完成；这里支持编辑 labels/taints 和删除 Node 对象。
            </Text>
          </Card>
          <AdminTable
            columns={nodeColumns}
            dataSource={nodesQuery.data?.data ?? []}
            rowKey="name"
            loading={nodesQuery.isLoading}
            pageSize={20}
            expandedRowRender={(record: Node) => <NodeResourcePanel node={record} />}
            hideExpandedColumn={false}
          />
        </>
      )}

      <Modal
        title={editingNodeName ? `编辑节点 ${editingNodeName}` : '编辑节点'}
        visible={!!editingNodeName}
        footer={null}
        width={720}
        onCancel={() => setEditingNodeName(null)}
      >
        {nodeDetailQuery.isLoading && !nodeDetail ? (
          <div className="flex items-center justify-center h-48">
            <Spin size="large" />
          </div>
        ) : (
          <Form key={editingNodeName ?? 'node'} initValues={nodeModalInitValues} onSubmit={(values) => updateNodeMutation.mutate(values)}>
            <Form.TextArea field="labels" label="Labels(JSON)" rows={8} />
            <Form.TextArea field="taints" label="Taints(JSON Array)" rows={8} />
            <div className="kc-form-actions">
              <Button onClick={() => setEditingNodeName(null)}>取消</Button>
              <Button htmlType="submit" theme="solid" loading={updateNodeMutation.isPending}>
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
      Toast.success('命名空间已创建')
      queryClient.invalidateQueries({ queryKey: ['cluster-namespaces', clusterId] })
      setNamespaceModalVisible(false)
      setEditingNamespace(null)
    },
    onError: (err: Error) => Toast.error(err.message),
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
      Toast.success('命名空间已更新')
      queryClient.invalidateQueries({ queryKey: ['cluster-namespaces', clusterId] })
      setNamespaceModalVisible(false)
      setEditingNamespace(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteNamespaceMutation = useMutation({
    mutationFn: async (namespaceName: string) => {
      if (!clusterId) return
      return api.delete(`/clusters/${clusterId}/namespaces/${namespaceName}`)
    },
    onSuccess: () => {
      Toast.success('命名空间已删除')
      queryClient.invalidateQueries({ queryKey: ['cluster-namespaces', clusterId] })
    },
    onError: (err: Error) => Toast.error(err.message),
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

  const nsColumns: ColumnProps<Namespace>[] = [
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
              <Tag key={k} size="small" className="mr-1">{`${k}=${v}`}</Tag>
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
            theme="borderless"
            icon={<IconSend />}
            onClick={() => {
              setNamespace(name)
              navigate('/workloads/overview')
            }}
          >
            查看资源
          </Button>
          <Button
            size="small"
            theme="borderless"
            icon={<IconEdit />}
            onClick={() => {
              setEditingNamespace(record)
              setNamespaceModalVisible(true)
            }}
          >
            编辑
          </Button>
          <Button
            size="small"
            theme="borderless"
            type="danger"
            icon={<IconDelete />}
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
      <PageHeader
        title={t('page.namespaces.title', 'Namespaces')}
        description={t('page.namespaces.desc', 'Manage namespaces in the current cluster scope and jump into related workload views.')}
        actions={
          <Button
            icon={<IconPlus />}
            theme="solid"
            onClick={() => {
              setEditingNamespace(null)
              setNamespaceModalVisible(true)
            }}
          >
            {t('common.create', 'Create')}
          </Button>
        }
      />
      <PlatformScopeToolbar />
      {!clusterId ? (
        <Empty description={t('common.pleaseSelectClusterShort', 'Select a cluster')} />
      ) : (
        <AdminTable
          columns={nsColumns}
          dataSource={namespacesQuery.data?.data ?? []}
          rowKey="name"
          loading={namespacesQuery.isLoading}
          pageSize={20}
        />
      )}

      <Modal
        title={editingNamespace ? `编辑命名空间 ${editingNamespace.name}` : '新建命名空间'}
        visible={namespaceModalVisible}
        footer={null}
        width={720}
        onCancel={() => {
          setNamespaceModalVisible(false)
          setEditingNamespace(null)
        }}
      >
        <Form
          key={editingNamespace?.name ?? 'namespace-new'}
          initValues={namespaceModalInitValues}
          onSubmit={(values) => {
            if (editingNamespace) {
              updateNamespaceMutation.mutate(values)
            } else {
              createNamespaceMutation.mutate(values)
            }
          }}
        >
          {!editingNamespace ? <Form.Input field="name" label="命名空间名称" rules={[{ required: true, message: '请输入命名空间名称' }]} /> : null}
          <Form.TextArea field="labels" label="Labels(JSON)" rows={8} />
          <Form.TextArea field="annotations" label="Annotations(JSON)" rows={8} />
          <div className="kc-form-actions">
            <Button
              onClick={() => {
                setNamespaceModalVisible(false)
                setEditingNamespace(null)
              }}
            >
              取消
            </Button>
            <Button htmlType="submit" theme="solid" loading={createNamespaceMutation.isPending || updateNamespaceMutation.isPending}>
              {editingNamespace ? '保存' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
