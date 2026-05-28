import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { App, Button, Card, Descriptions, Form, Input, Modal, Select, Space, Spin, Tag, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PageHeader } from '@/components/page-header'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { StatusTag } from '@/components/status-tag'
import { tableColumnPresets } from '@/utils/table-columns'
import type { Cluster, ClusterDetail, ApiResponse, Node } from '@/types'
import type { TableColumnsType } from 'antd'

const { Text } = Typography

type ConnectionMode = 'direct_kubeconfig' | 'agent'

interface ClusterFormValues {
  name?: string
  provider?: string
  environment?: string
  connectionMode: ConnectionMode
  kubeconfig?: string
  agentEndpoint?: string
  agentToken?: string
  prometheusBaseUrl?: string
  prometheusBearerToken?: string
}

const clusterTypeOptions = [
  { value: 'standard_kubernetes', labelZh: '标准 Kubernetes', labelEn: 'Standard Kubernetes' },
  { value: 'gke', labelZh: 'GKE', labelEn: 'GKE' },
  { value: 'ack', labelZh: 'ACK', labelEn: 'ACK' },
  { value: 'tke', labelZh: 'TKE', labelEn: 'TKE' },
  { value: 'aks', labelZh: 'AKS', labelEn: 'AKS' },
]

function formatClusterType(value: string | undefined, localeCode: string) {
  const item = clusterTypeOptions.find((option) => option.value === value)
  if (!item) return value || '-'
  return localeCode === 'zh_CN' ? item.labelZh : item.labelEn
}

function clusterTypeOf(cluster: Pick<Cluster, 'region' | 'labels'>) {
  const provider = cluster.labels?.provider
  return typeof provider === 'string' && provider.trim() !== '' ? provider.trim() : cluster.region
}

export function ClustersPage() {
  const { t } = useI18n()
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [modalVisible, setModalVisible] = useState(false)
  const [editingCluster, setEditingCluster] = useState<Cluster | null>(null)
  const [connectionMode, setConnectionMode] = useState<ConnectionMode>('direct_kubeconfig')

  const { data, isLoading } = useQuery({
    queryKey: ['clusters'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
  })

  const clusterDetailQuery = useQuery({
    queryKey: ['cluster-edit-detail', editingCluster?.id],
    queryFn: () => api.get<ApiResponse<ClusterDetail>>(`/clusters/${editingCluster!.id}/detail`),
    enabled: modalVisible && !!editingCluster,
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post<ApiResponse<Cluster>>('/clusters', values),
    onSuccess: () => {
      void message.success('集群创建成功')
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      setModalVisible(false)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put<ApiResponse<Cluster>>(`/clusters/${id}`, values),
    onSuccess: () => {
      void message.success('集群更新成功')
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      setModalVisible(false)
      setEditingCluster(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/clusters/${id}`),
    onSuccess: () => {
      void message.success('集群已删除')
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const clusters = data?.data ?? []

  const columns: TableColumnsType<Cluster> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (_: unknown, record: Cluster) => (
        <Button type="text" onClick={() => navigate(`/clusters/${record.id}`)}>
          {record.name}
        </Button>
      ),
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'health',
      render: (health: Cluster['health']) => <StatusTag value={health?.status ?? 'unknown'} />,
    },
    { title: localeCode === 'zh_CN' ? '类型' : 'Type', dataIndex: 'region', render: (_: string, record: Cluster) => formatClusterType(clusterTypeOf(record), localeCode) },
    { title: 'Env', dataIndex: 'environment' },
    { title: 'Mode', dataIndex: 'connectionMode' },
    { title: '版本', dataIndex: 'version', render: (value: string) => value || '-' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: Cluster) => (
        <Space>
          <Button
            icon={<EditOutlined />}
            type="text"
            size="small"
            onClick={() => {
              setEditingCluster(record)
              setModalVisible(true)
            }}
          />
          <Button
            icon={<DeleteOutlined />}
            type="text"
            danger
            size="small"
            onClick={() => {
              Modal.confirm({
                title: `确认删除集群 ${record.name}？`,
                content: '删除后会移除该集群在 Soha 中的注册信息。',
                onOk: () => deleteMutation.mutate(record.id),
              })
            }}
          />
        </Space>
      ),
    },
  ]

  useEffect(() => {
    if (!modalVisible) return
    if (editingCluster) {
      setConnectionMode((clusterDetailQuery.data?.data.connection.mode as ConnectionMode) || (editingCluster.connectionMode as ConnectionMode) || 'direct_kubeconfig')
      return
    }
    setConnectionMode('direct_kubeconfig')
  }, [modalVisible, editingCluster, clusterDetailQuery.data])

  const initialValues = useMemo<ClusterFormValues>(() => {
    if (!editingCluster) {
      return {
        connectionMode: 'direct_kubeconfig',
        provider: 'standard_kubernetes',
      }
    }
    const detail = clusterDetailQuery.data?.data
    return {
      name: editingCluster.name,
      provider: clusterTypeOf(editingCluster) || undefined,
      environment: editingCluster.environment,
      connectionMode: ((detail?.connection.mode || editingCluster.connectionMode) as ConnectionMode) || 'direct_kubeconfig',
      agentEndpoint: detail?.connection.endpoint || '',
      prometheusBaseUrl: detail?.monitoring.prometheus.baseUrl || '',
    }
  }, [editingCluster, clusterDetailQuery.data])

  const formKey = editingCluster
    ? `cluster-edit:${editingCluster.id}:${clusterDetailQuery.data ? 'ready' : 'loading'}:${connectionMode}`
    : `cluster-create:${connectionMode}`

  const agentConfigExample = `app:
  name: soha-agent

http:
  addr: :18080

auth:
  bearer_token: demo-agent-token

kubernetes:
  kubeconfig: /abs/path/to/kubeconfig
  context: ""
`

  const handleSubmit = (values: Record<string, unknown>) => {
    const provider = typeof values.provider === 'string' ? values.provider.trim() : ''
    const labels = { ...(editingCluster?.labels ?? {}) }
    if (provider) {
      labels.provider = provider
    } else {
      delete labels.provider
    }
    const payload = {
      ...values,
      labels,
    }
    delete (payload as Record<string, unknown>).provider
    if (editingCluster) {
      updateMutation.mutate({ id: editingCluster.id, values: payload })
    } else {
      createMutation.mutate(payload)
    }
  }

  return (
    <div className="soha-page">
      <PageHeader
        title={t('page.clusters.title', 'Cluster Management')}
        description={t('page.clusters.desc', 'Manage cluster onboarding, health state, and connection settings in one place.')}
        actions={
          <Button
            icon={<PlusOutlined />}
            type="primary"
            onClick={() => {
              setEditingCluster(null)
              setModalVisible(true)
            }}
          >
            {t('common.create', 'Create')}
          </Button>
        }
      />

      <AdminTable
        columns={columns}
        dataSource={clusters}
        rowKey="id"
        loading={isLoading}
      />

      <Modal
        title={editingCluster ? '编辑集群' : '添加集群'}
        open={modalVisible}
        width={760}
        onCancel={() => {
          setModalVisible(false)
          setEditingCluster(null)
        }}
        footer={null}
      >
        {editingCluster && clusterDetailQuery.isLoading && !clusterDetailQuery.data ? (
          <div className="flex items-center justify-center h-64">
            <Spin size="large" />
          </div>
        ) : (
          <Form key={formKey} layout="vertical" onFinish={handleSubmit} initialValues={initialValues}>
            <Form.Item name="name" label="集群名称" rules={[{ required: true, message: '请输入集群名称' }]}>
              <Input />
            </Form.Item>
            <Form.Item name="provider" label={localeCode === 'zh_CN' ? '集群类型' : 'Cluster Type'} rules={[{ required: true, message: localeCode === 'zh_CN' ? '请选择集群类型' : 'Select a cluster type' }]}>
              <Select
                options={clusterTypeOptions.map((item) => ({
                  value: item.value,
                  label: localeCode === 'zh_CN' ? item.labelZh : item.labelEn,
                }))}
              />
            </Form.Item>
            <Form.Item name="environment" label="Environment">
              <Input />
            </Form.Item>
            <Form.Item name="connectionMode" label="连接方式">
              <Select
                options={[
                  { value: 'direct_kubeconfig', label: '直接 Kubeconfig' },
                  { value: 'agent', label: 'Agent' },
                ]}
                onChange={(value) => setConnectionMode(value as ConnectionMode)}
              />
            </Form.Item>

            {connectionMode === 'direct_kubeconfig' ? (
              <Form.Item
                name="kubeconfig"
                label="Kubeconfig"
                rules={editingCluster ? undefined : [{ required: true, message: '请输入 Kubeconfig' }]}
              >
                <Input.TextArea
                  placeholder={editingCluster ? '留空则沿用现有 kubeconfig' : '请输入 kubeconfig 内容'}
                  rows={8}
                />
              </Form.Item>
            ) : (
              <>
                <Form.Item name="agentEndpoint" label="Agent Endpoint" rules={editingCluster ? undefined : [{ required: true, message: '请输入 Agent Endpoint' }]}>
                  <Input placeholder={editingCluster ? '留空则沿用现有 endpoint' : 'http://127.0.0.1:18080'} />
                </Form.Item>
                <Form.Item name="agentToken" label="Agent Token">
                  <Input.Password placeholder={editingCluster ? '留空则沿用现有 token' : '与 agent 配置中的 auth.bearer_token 一致'} />
                </Form.Item>
                <Card className="soha-detail-card">
                  <div className="soha-detail-meta">
                    <Text strong>Agent 部署方式</Text>
                    <pre className="soha-code-block">{agentConfigExample}</pre>
                    <pre className="soha-code-block">{`go run ./cmd/agent\nKC_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml go run ./cmd/agent`}</pre>
                  </div>
                </Card>
              </>
            )}

            <Card className="soha-detail-card">
              <div className="soha-detail-meta">
                <Text strong>Prometheus</Text>
              </div>
              <Form.Item name="prometheusBaseUrl" label="Prometheus URL">
                <Input placeholder="http://prometheus:9090" />
              </Form.Item>
              <Form.Item name="prometheusBearerToken" label="Prometheus Token">
                <Input.Password placeholder={editingCluster ? '留空则沿用现有 token' : ''} />
              </Form.Item>
            </Card>

            <div className="soha-form-actions">
              <Button onClick={() => setModalVisible(false)}>取消</Button>
              <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>
                {editingCluster ? '更新' : '创建'}
              </Button>
            </div>
          </Form>
        )}
      </Modal>
    </div>
  )
}

export function ClusterDetailPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = useParams()
  const setClusterId = usePlatformScopeStore((state) => state.setClusterId)

  const clusterDetailQuery = useQuery({
    queryKey: ['cluster-detail-page', clusterId],
    queryFn: () => api.get<ApiResponse<ClusterDetail>>(`/clusters/${clusterId}/detail`),
    enabled: !!clusterId,
  })
  const nodesQuery = useQuery({
    queryKey: ['cluster-detail-nodes', clusterId],
    queryFn: () => api.get<ApiResponse<Node[]>>(`/clusters/${clusterId}/infrastructure/nodes`),
    enabled: !!clusterId,
  })

  const detail = clusterDetailQuery.data?.data
  const summary = detail?.summary
  const nodeColumns: TableColumnsType<Node> = [
    {
      title: localeCode === 'zh_CN' ? '节点' : 'Node',
      dataIndex: 'name',
      render: (value: string) => (
        <Button
          type="text"
          onClick={() => {
            setClusterId(summary?.id ?? null)
            navigate(`/cluster-resources/nodes/${encodeURIComponent(value)}?clusterId=${encodeURIComponent(summary?.id ?? '')}`)
          }}
        >
          {value}
        </Button>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '角色' : 'Roles',
      dataIndex: 'roles',
      render: (roles: string[]) => roles?.length ? roles.join(', ') : '-',
    },
    {
      title: localeCode === 'zh_CN' ? '版本' : 'Version',
      dataIndex: 'version',
      render: (value: string) => value || '-',
    },
    {
      title: 'IP',
      dataIndex: 'internalIp',
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? 'Pod 数量' : 'Pods',
      dataIndex: 'podCount',
    },
  ]

  if (clusterDetailQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  }

  if (!detail || !summary) {
    return (
      <div className="soha-page">
        <PageHeader title={localeCode === 'zh_CN' ? '集群详情' : 'Cluster Detail'} description={localeCode === 'zh_CN' ? '当前集群不存在或详情不可用。' : 'The cluster was not found or its detail is unavailable.'} />
        <Card>
          <Text type="secondary">{t('common.notFound', 'Not found')}</Text>
        </Card>
      </div>
    )
  }

  return (
    <div className="soha-page">
      <PageHeader
        title={`${localeCode === 'zh_CN' ? '集群详情' : 'Cluster Detail'}: ${summary.name}`}
        description={localeCode === 'zh_CN' ? '查看集群标签、版本、连接方式和运行诊断信息。' : 'Inspect cluster labels, version, connectivity, and runtime diagnostics.'}
        actions={(
          <Space>
            <Button onClick={() => navigate('/clusters')}>{localeCode === 'zh_CN' ? '返回列表' : 'Back'}</Button>
            <Button
              variant="outlined"
              onClick={() => {
                setClusterId(summary.id)
                navigate('/cluster-resources/nodes')
              }}
            >
              {localeCode === 'zh_CN' ? '查看节点' : 'Open Nodes'}
            </Button>
            <Button
              type="primary"
              onClick={() => {
                setClusterId(summary.id)
                navigate('/workloads/overview')
              }}
            >
              {localeCode === 'zh_CN' ? '查看工作负载' : 'Open Workloads'}
            </Button>
          </Space>
        )}
      />

      <div className="grid gap-4 xl:grid-cols-2">
        <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '基础信息' : 'Summary'}>
          <Descriptions
            items={[
              { key: localeCode === 'zh_CN' ? '名称' : 'Name', label: localeCode === 'zh_CN' ? '名称' : 'Name', children: summary.name },
              { key: localeCode === 'zh_CN' ? '状态' : 'Status', label: localeCode === 'zh_CN' ? '状态' : 'Status', children: <StatusTag value={summary.health?.status ?? 'unknown'} /> },
              { key: localeCode === 'zh_CN' ? '版本' : 'Version', label: localeCode === 'zh_CN' ? '版本' : 'Version', children: summary.version || '-' },
              { key: localeCode === 'zh_CN' ? '类型' : 'Type', label: localeCode === 'zh_CN' ? '类型' : 'Type', children: formatClusterType(clusterTypeOf(summary), localeCode) },
              { key: 'Environment', label: 'Environment', children: summary.environment || '-' },
              { key: localeCode === 'zh_CN' ? '连接方式' : 'Mode', label: localeCode === 'zh_CN' ? '连接方式' : 'Mode', children: summary.connectionMode || '-' },
              { key: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked', label: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked', children: summary.health?.lastChecked || '-' },
              { key: localeCode === 'zh_CN' ? '状态信息' : 'Message', label: localeCode === 'zh_CN' ? '状态信息' : 'Message', children: summary.health?.message || '-' },
            ]}
          />
          <div className="soha-detail-meta">
            <Text strong>{localeCode === 'zh_CN' ? '集群 Labels:' : 'Cluster Labels:'}</Text>
            {Object.keys(summary.labels || {}).length === 0 ? (
              <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '未配置标签' : 'No labels configured'}</Text>
            ) : (
              <div className="soha-tag-list">
                {Object.entries(summary.labels || {}).map(([key, value]) => (
                  <Tag key={key}>{key}={value}</Tag>
                ))}
              </div>
            )}
          </div>
          <div className="soha-detail-meta">
            <Text strong>{localeCode === 'zh_CN' ? '能力:' : 'Capabilities:'}</Text>
            {summary.capabilities?.length ? (
              <div className="soha-tag-list">
                {summary.capabilities.map((item) => (
                  <Tag key={item}>{item}</Tag>
                ))}
              </div>
            ) : (
              <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '无额外能力声明' : 'No explicit capabilities reported'}</Text>
            )}
          </div>
        </Card>

        <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '连接与诊断' : 'Connection & Diagnostics'}>
          <Descriptions
            items={[
              { key: localeCode === 'zh_CN' ? '连接模式' : 'Connection Mode', label: localeCode === 'zh_CN' ? '连接模式' : 'Connection Mode', children: detail.connection.mode || '-' },
              { key: localeCode === 'zh_CN' ? '凭据类型' : 'Credential Type', label: localeCode === 'zh_CN' ? '凭据类型' : 'Credential Type', children: detail.connection.credentialType || '-' },
              { key: localeCode === 'zh_CN' ? '来源类型' : 'Source Type', label: localeCode === 'zh_CN' ? '来源类型' : 'Source Type', children: detail.connection.sourceType || '-' },
              { key: localeCode === 'zh_CN' ? 'Context' : 'Context', label: localeCode === 'zh_CN' ? 'Context' : 'Context', children: detail.connection.context || '-' },
              { key: localeCode === 'zh_CN' ? 'Endpoint' : 'Endpoint', label: localeCode === 'zh_CN' ? 'Endpoint' : 'Endpoint', children: detail.connection.endpoint || '-' },
              { key: localeCode === 'zh_CN' ? 'Informer Cache' : 'Informer Cache', label: localeCode === 'zh_CN' ? 'Informer Cache' : 'Informer Cache', children: detail.connection.usesInformerCache ? 'Yes' : 'No' },
              { key: localeCode === 'zh_CN' ? '同步策略' : 'Sync Strategy', label: localeCode === 'zh_CN' ? '同步策略' : 'Sync Strategy', children: detail.diagnostics.syncStrategy || '-' },
              { key: localeCode === 'zh_CN' ? '缓存状态' : 'Cache Status', label: localeCode === 'zh_CN' ? '缓存状态' : 'Cache Status', children: detail.diagnostics.cacheStatus || '-' },
              { key: localeCode === 'zh_CN' ? '连接状态' : 'Connection State', label: localeCode === 'zh_CN' ? '连接状态' : 'Connection State', children: detail.diagnostics.connectionState || '-' },
              { key: localeCode === 'zh_CN' ? '诊断信息' : 'Diagnostic Message', label: localeCode === 'zh_CN' ? '诊断信息' : 'Diagnostic Message', children: detail.diagnostics.message || '-' },
            ]}
          />
        </Card>
      </div>

      <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '监控配置' : 'Monitoring'}>
        <Descriptions
          items={[
            { key: localeCode === 'zh_CN' ? 'Prometheus URL' : 'Prometheus URL', label: localeCode === 'zh_CN' ? 'Prometheus URL' : 'Prometheus URL', children: detail.monitoring.prometheus.baseUrl || '-' },
            { key: localeCode === 'zh_CN' ? 'Prometheus Cluster Label' : 'Prometheus Cluster Label', label: localeCode === 'zh_CN' ? 'Prometheus Cluster Label' : 'Prometheus Cluster Label', children: detail.monitoring.prometheus.clusterLabel || '-' },
            { key: localeCode === 'zh_CN' ? 'Bearer Token' : 'Bearer Token', label: localeCode === 'zh_CN' ? 'Bearer Token' : 'Bearer Token', children: detail.monitoring.prometheus.hasBearerToken ? (localeCode === 'zh_CN' ? '已配置' : 'Configured') : (localeCode === 'zh_CN' ? '未配置' : 'Not configured') },
            { key: localeCode === 'zh_CN' ? 'Grafana URL' : 'Grafana URL', label: localeCode === 'zh_CN' ? 'Grafana URL' : 'Grafana URL', children: detail.monitoring.prometheus.grafanaBaseUrl || '-' },
          ]}
        />
      </Card>

      <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '节点快照' : 'Node Snapshot'}>
        <div className="soha-detail-meta">
          <Text type="secondary">
            {localeCode === 'zh_CN'
              ? '点击节点可进入独立详情页，继续查看污点、YAML、Labels 和承载 Pod。'
              : 'Open a node to inspect taints, YAML, labels, and scheduled pods in a dedicated workspace.'}
          </Text>
        </div>
        {nodesQuery.isError ? (
          <Text type="warning">{(nodesQuery.error as Error)?.message || (localeCode === 'zh_CN' ? '节点快照加载失败' : 'Failed to load node snapshot')}</Text>
        ) : (
          <AdminTable
            columns={nodeColumns}
            dataSource={nodesQuery.data?.data ?? []}
            rowKey="name"
            loading={nodesQuery.isLoading}
            pageSize={10}
            enableColumnSelection={false}
          />
        )}
      </Card>
    </div>
  )
}
