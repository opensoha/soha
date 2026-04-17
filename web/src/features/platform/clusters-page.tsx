import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Button, Modal, Form, Toast, Space, Card, Spin, Typography, Tag, Descriptions } from '@douyinfe/semi-ui'
import { IconPlus, IconEdit, IconDelete } from '@douyinfe/semi-icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PageHeader } from '@/components/page-header'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { StatusTag } from '@/components/status-tag'
import { tableColumnPresets } from '@/utils/table-columns'
import type { Cluster, ClusterDetail, ApiResponse, Node } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

const { Text } = Typography

type ConnectionMode = 'direct_kubeconfig' | 'agent'

interface ClusterFormValues {
  id?: string
  name?: string
  region?: string
  environment?: string
  connectionMode: ConnectionMode
  kubeconfig?: string
  context?: string
  agentEndpoint?: string
  agentToken?: string
  prometheusBaseUrl?: string
  prometheusBearerToken?: string
  prometheusClusterLabel?: string
}

export function ClustersPage() {
  const { t } = useI18n()
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
      Toast.success('集群创建成功')
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put<ApiResponse<Cluster>>(`/clusters/${id}`, values),
    onSuccess: () => {
      Toast.success('集群更新成功')
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      setModalVisible(false)
      setEditingCluster(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/clusters/${id}`),
    onSuccess: () => {
      Toast.success('集群已删除')
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const clusters = data?.data ?? []

  const columns: ColumnProps<Cluster>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (_: unknown, record: Cluster) => (
        <Button theme="borderless" type="primary" onClick={() => navigate(`/clusters/${record.id}`)}>
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
    { title: 'Region', dataIndex: 'region' },
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
            icon={<IconEdit />}
            theme="borderless"
            size="small"
            onClick={() => {
              setEditingCluster(record)
              setModalVisible(true)
            }}
          />
          <Button
            icon={<IconDelete />}
            theme="borderless"
            type="danger"
            size="small"
            onClick={() => {
              Modal.confirm({
                title: `确认删除集群 ${record.name}？`,
                content: '删除后会移除该集群在 KubeCrux 中的注册信息。',
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
        prometheusClusterLabel: 'cluster',
      }
    }

    const detail = clusterDetailQuery.data?.data
    return {
      name: editingCluster.name,
      region: editingCluster.region,
      environment: editingCluster.environment,
      connectionMode: ((detail?.connection.mode || editingCluster.connectionMode) as ConnectionMode) || 'direct_kubeconfig',
      context: detail?.connection.context || '',
      agentEndpoint: detail?.connection.endpoint || '',
      prometheusBaseUrl: detail?.monitoring.prometheus.baseUrl || '',
      prometheusClusterLabel: detail?.monitoring.prometheus.clusterLabel || 'cluster',
    }
  }, [editingCluster, clusterDetailQuery.data])

  const formKey = editingCluster
    ? `cluster-edit:${editingCluster.id}:${clusterDetailQuery.data ? 'ready' : 'loading'}:${connectionMode}`
    : `cluster-create:${connectionMode}`

  const agentConfigExample = `app:
  name: kubecrux-agent

http:
  addr: :18080

auth:
  bearer_token: demo-agent-token

kubernetes:
  kubeconfig: /abs/path/to/kubeconfig
  context: ""
`

  const handleSubmit = (values: Record<string, unknown>) => {
    if (editingCluster) {
      updateMutation.mutate({ id: editingCluster.id, values })
    } else {
      createMutation.mutate(values)
    }
  }

  return (
    <div className="kc-page">
      <PageHeader
        title={t('page.clusters.title', 'Cluster Management')}
        description={t('page.clusters.desc', 'Manage cluster onboarding, health state, and connection settings in one place.')}
        actions={
          <Button
            icon={<IconPlus />}
            theme="solid"
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
        visible={modalVisible}
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
          <Form key={formKey} onSubmit={handleSubmit} initValues={initialValues}>
            {!editingCluster && <Form.Input field="id" label="集群 ID" rules={[{ required: true, message: '请输入集群 ID' }]} />}
            <Form.Input field="name" label="集群名称" rules={[{ required: true, message: '请输入集群名称' }]} />
            <Form.Input field="region" label="Region" />
            <Form.Input field="environment" label="Environment" />
            <Form.Select
              field="connectionMode"
              label="连接方式"
              initValue={connectionMode}
              optionList={[
                { value: 'direct_kubeconfig', label: '直接 Kubeconfig' },
                { value: 'agent', label: 'Agent' },
              ]}
              onChange={(value) => setConnectionMode(value as ConnectionMode)}
            />

            {connectionMode === 'direct_kubeconfig' ? (
              <>
                <Form.Input field="context" label="Kube Context" placeholder="可选，留空使用 kubeconfig 默认 context" />
                <Form.TextArea
                  field="kubeconfig"
                  label="Kubeconfig"
                  placeholder={editingCluster ? '留空则沿用现有 kubeconfig；切换到直连模式时请填写新的 kubeconfig' : '请输入 kubeconfig 内容'}
                  rules={editingCluster ? undefined : [{ required: true, message: '请输入 Kubeconfig' }]}
                  rows={8}
                />
              </>
            ) : (
              <>
                <Form.Input field="agentEndpoint" label="Agent Endpoint" placeholder="http://127.0.0.1:18080" rules={[{ required: true, message: '请输入 Agent Endpoint' }]} />
                <Form.Input field="agentToken" label="Agent Token" mode="password" placeholder={editingCluster ? '留空则沿用现有 token' : '与 agent 配置中的 auth.bearer_token 一致'} />
                <Card className="kc-detail-card">
                  <div className="kc-detail-meta">
                    <Text strong>Agent 部署方式</Text>
                    <pre className="kc-code-block">{agentConfigExample}</pre>
                    <pre className="kc-code-block">{`go run ./cmd/agent\nKC_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml go run ./cmd/agent`}</pre>
                  </div>
                </Card>
              </>
            )}

            <Card className="kc-detail-card">
              <div className="kc-detail-meta">
                <Text strong>Prometheus</Text>
                <Text type="tertiary" size="small">节点 CPU、内存指标依赖这里的集群级 Prometheus 地址。</Text>
              </div>
              <Form.Input field="prometheusBaseUrl" label="Prometheus URL" placeholder="http://prometheus:9090" />
              <Form.Input field="prometheusBearerToken" label="Prometheus Token" mode="password" placeholder={editingCluster ? '留空则沿用现有 token' : ''} />
              <Form.Input field="prometheusClusterLabel" label="Prometheus Cluster Label" placeholder="cluster" />
            </Card>

            <div className="kc-form-actions">
              <Button onClick={() => setModalVisible(false)}>取消</Button>
              <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
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
  const nodeColumns: ColumnProps<Node>[] = [
    {
      title: localeCode === 'zh_CN' ? '节点' : 'Node',
      dataIndex: 'name',
      render: (value: string) => (
        <Button
          theme="borderless"
          type="primary"
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
      <div className="kc-page">
        <PageHeader title={localeCode === 'zh_CN' ? '集群详情' : 'Cluster Detail'} description={localeCode === 'zh_CN' ? '当前集群不存在或详情不可用。' : 'The cluster was not found or its detail is unavailable.'} />
        <Card>
          <Text type="tertiary">{t('common.notFound', 'Not found')}</Text>
        </Card>
      </div>
    )
  }

  return (
    <div className="kc-page">
      <PageHeader
        title={`${localeCode === 'zh_CN' ? '集群详情' : 'Cluster Detail'}: ${summary.name}`}
        description={localeCode === 'zh_CN' ? '查看集群标签、版本、连接方式和运行诊断信息。' : 'Inspect cluster labels, version, connectivity, and runtime diagnostics.'}
        actions={(
          <Space>
            <Button onClick={() => navigate('/clusters')}>{localeCode === 'zh_CN' ? '返回列表' : 'Back'}</Button>
            <Button
              theme="light"
              onClick={() => {
                setClusterId(summary.id)
                navigate('/cluster-resources/nodes')
              }}
            >
              {localeCode === 'zh_CN' ? '查看节点' : 'Open Nodes'}
            </Button>
            <Button
              theme="solid"
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
        <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '基础信息' : 'Summary'}>
          <Descriptions
            data={[
              { key: localeCode === 'zh_CN' ? '名称' : 'Name', value: summary.name },
              { key: localeCode === 'zh_CN' ? '状态' : 'Status', value: <StatusTag value={summary.health?.status ?? 'unknown'} /> },
              { key: localeCode === 'zh_CN' ? '版本' : 'Version', value: summary.version || '-' },
              { key: 'Region', value: summary.region || '-' },
              { key: 'Environment', value: summary.environment || '-' },
              { key: localeCode === 'zh_CN' ? '连接方式' : 'Mode', value: summary.connectionMode || '-' },
              { key: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked', value: summary.health?.lastChecked || '-' },
              { key: localeCode === 'zh_CN' ? '状态信息' : 'Message', value: summary.health?.message || '-' },
            ]}
          />
          <div className="kc-detail-meta">
            <Text strong>{localeCode === 'zh_CN' ? '集群 Labels:' : 'Cluster Labels:'}</Text>
            {Object.keys(summary.labels || {}).length === 0 ? (
              <Text type="tertiary" size="small">{localeCode === 'zh_CN' ? '未配置标签' : 'No labels configured'}</Text>
            ) : (
              <div className="kc-tag-list">
                {Object.entries(summary.labels || {}).map(([key, value]) => (
                  <Tag key={key} size="small">{key}={value}</Tag>
                ))}
              </div>
            )}
          </div>
          <div className="kc-detail-meta">
            <Text strong>{localeCode === 'zh_CN' ? '能力:' : 'Capabilities:'}</Text>
            {summary.capabilities?.length ? (
              <div className="kc-tag-list">
                {summary.capabilities.map((item) => (
                  <Tag key={item} size="small">{item}</Tag>
                ))}
              </div>
            ) : (
              <Text type="tertiary" size="small">{localeCode === 'zh_CN' ? '无额外能力声明' : 'No explicit capabilities reported'}</Text>
            )}
          </div>
        </Card>

        <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '连接与诊断' : 'Connection & Diagnostics'}>
          <Descriptions
            data={[
              { key: localeCode === 'zh_CN' ? '连接模式' : 'Connection Mode', value: detail.connection.mode || '-' },
              { key: localeCode === 'zh_CN' ? '凭据类型' : 'Credential Type', value: detail.connection.credentialType || '-' },
              { key: localeCode === 'zh_CN' ? '来源类型' : 'Source Type', value: detail.connection.sourceType || '-' },
              { key: localeCode === 'zh_CN' ? 'Context' : 'Context', value: detail.connection.context || '-' },
              { key: localeCode === 'zh_CN' ? 'Endpoint' : 'Endpoint', value: detail.connection.endpoint || '-' },
              { key: localeCode === 'zh_CN' ? 'Informer Cache' : 'Informer Cache', value: detail.connection.usesInformerCache ? 'Yes' : 'No' },
              { key: localeCode === 'zh_CN' ? '同步策略' : 'Sync Strategy', value: detail.diagnostics.syncStrategy || '-' },
              { key: localeCode === 'zh_CN' ? '缓存状态' : 'Cache Status', value: detail.diagnostics.cacheStatus || '-' },
              { key: localeCode === 'zh_CN' ? '连接状态' : 'Connection State', value: detail.diagnostics.connectionState || '-' },
              { key: localeCode === 'zh_CN' ? '诊断信息' : 'Diagnostic Message', value: detail.diagnostics.message || '-' },
            ]}
          />
        </Card>
      </div>

      <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '监控配置' : 'Monitoring'}>
        <Descriptions
          data={[
            { key: localeCode === 'zh_CN' ? 'Prometheus URL' : 'Prometheus URL', value: detail.monitoring.prometheus.baseUrl || '-' },
            { key: localeCode === 'zh_CN' ? 'Prometheus Cluster Label' : 'Prometheus Cluster Label', value: detail.monitoring.prometheus.clusterLabel || '-' },
            { key: localeCode === 'zh_CN' ? 'Bearer Token' : 'Bearer Token', value: detail.monitoring.prometheus.hasBearerToken ? (localeCode === 'zh_CN' ? '已配置' : 'Configured') : (localeCode === 'zh_CN' ? '未配置' : 'Not configured') },
          ]}
        />
      </Card>

      <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '节点快照' : 'Node Snapshot'}>
        <div className="kc-detail-meta">
          <Text type="tertiary">
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
