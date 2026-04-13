import { useEffect, useMemo, useState } from 'react'
import { Button, Modal, Form, Toast, Space, Card, Spin, Typography } from '@douyinfe/semi-ui'
import { IconPlus, IconEdit, IconDelete } from '@douyinfe/semi-icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PageHeader } from '@/components/page-header'
import { api } from '@/services/api-client'
import { StatusTag } from '@/components/status-tag'
import { tableColumnPresets } from '@/utils/table-columns'
import type { Cluster, ClusterDetail, ApiResponse } from '@/types'
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
  grafanaBaseUrl?: string
}

export function ClustersPage() {
  const { t } = useI18n()
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
      render: (_: unknown, record: Cluster) => record.name,
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
      grafanaBaseUrl: detail?.monitoring.prometheus.grafanaBaseUrl || '',
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
                <Text strong>Prometheus / Grafana</Text>
                <Text type="tertiary" size="small">节点 CPU、内存指标依赖这里的集群级 Prometheus 地址。</Text>
              </div>
              <Form.Input field="prometheusBaseUrl" label="Prometheus URL" placeholder="http://prometheus:9090" />
              <Form.Input field="prometheusBearerToken" label="Prometheus Token" mode="password" placeholder={editingCluster ? '留空则沿用现有 token' : ''} />
              <Form.Input field="prometheusClusterLabel" label="Prometheus Cluster Label" placeholder="cluster" />
              <Form.Input field="grafanaBaseUrl" label="Grafana URL" placeholder="http://grafana:3000" />
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
