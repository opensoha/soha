import { useEffect, useMemo, useState } from 'react'
import type { Key } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { App, Button, Card, Descriptions, Form, Input, Modal, Popconfirm, Select, Space, Spin, Tag, Typography } from 'antd'
import {
  DeleteOutlined,
  DownOutlined,
  EditOutlined,
  EyeOutlined,
  PlusOutlined,
  UpOutlined,
} from '@ant-design/icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementBatchBar,
  ManagementDensityButton,
  ManagementDetailHeader,
  ManagementIconButton,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { StatusTag } from '@/components/status-tag'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import type { Cluster, ClusterDetail, ApiResponse, Node } from '@/types'
import type { TableColumnsType } from 'antd'
import './platform-pages.css'

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

function formatConnectionMode(value: string | undefined, localeCode: string) {
  if (value === 'direct_kubeconfig') {
    return localeCode === 'zh_CN' ? '直连' : 'Direct'
  }
  if (value === 'agent') return 'Agent'
  return value || '-'
}

function clusterHealthTone(value?: string) {
  const normalized = (value || '').trim().toLowerCase()
  if (['healthy', 'connected', 'ready', 'available', 'running', 'normal'].includes(normalized)) return 'success'
  if (['error', 'failed', 'disconnected', 'critical', 'notready', 'lost'].includes(normalized)) return 'error'
  if (['pending', 'warning', 'queued', 'waiting'].includes(normalized)) return 'warning'
  if (['syncing', 'checking', 'initializing'].includes(normalized)) return 'processing'
  return 'muted'
}

function formatClusterHealth(value: string | undefined, localeCode: string) {
  const normalized = (value || '').trim().toLowerCase()
  if (localeCode !== 'zh_CN') return value || 'unknown'
  if (['healthy', 'connected', 'ready', 'available', 'running', 'normal'].includes(normalized)) return '正常'
  if (['error', 'failed', 'disconnected', 'critical', 'notready', 'lost'].includes(normalized)) return '异常'
  if (['pending', 'warning', 'queued', 'waiting'].includes(normalized)) return '等待'
  if (['syncing', 'checking', 'initializing'].includes(normalized)) return '同步中'
  return value || '未知'
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
  const [searchText, setSearchText] = useState('')
  const [statusFilter, setStatusFilter] = useState<string>()
  const [typeFilter, setTypeFilter] = useState<string>()
  const [modeFilter, setModeFilter] = useState<string>()
  const [appliedSearchText, setAppliedSearchText] = useState('')
  const [appliedStatusFilter, setAppliedStatusFilter] = useState<string>()
  const [appliedTypeFilter, setAppliedTypeFilter] = useState<string>()
  const [appliedModeFilter, setAppliedModeFilter] = useState<string>()
  const [queryExpanded, setQueryExpanded] = useState(false)
  const [selectedRowKeys, setSelectedRowKeys] = useState<string[]>([])
  const [tableSize, setTableSize] = useState<'middle' | 'small'>('small')
  const setClusterId = usePlatformScopeStore((state) => state.setClusterId)

  const { data, isLoading, isFetching, refetch } = useQuery({
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

  const clusters = useMemo(() => data?.data ?? [], [data?.data])

  useEffect(() => {
    const clusterIds = new Set(clusters.map((cluster) => cluster.id))
    setSelectedRowKeys((current) => {
      const next = current.filter((id) => clusterIds.has(id))
      return next.length === current.length ? current : next
    })
  }, [clusters])

  const statusOptions = useMemo(() => {
    const values = Array.from(new Set(clusters.map((cluster) => cluster.health?.status || 'unknown')))
    return values.sort().map((value) => ({ value, label: value }))
  }, [clusters])

  const typeOptions = useMemo(() => {
    const values = new Set(clusterTypeOptions.map((option) => option.value))
    clusters.forEach((cluster) => {
      const type = clusterTypeOf(cluster)
      if (type) values.add(type)
    })
    return Array.from(values).map((value) => ({
      value,
      label: formatClusterType(value, localeCode),
    }))
  }, [clusters, localeCode])

  const modeOptions = useMemo(() => {
    const values = Array.from(new Set(clusters.map((cluster) => cluster.connectionMode).filter(Boolean)))
    return values.sort().map((value) => ({ value, label: formatConnectionMode(value, localeCode) }))
  }, [clusters, localeCode])

  const filteredClusters = useMemo(() => {
    const keyword = appliedSearchText.trim().toLowerCase()
    return clusters.filter((cluster) => {
      const clusterType = clusterTypeOf(cluster)
      const haystack = [
        cluster.name,
        cluster.environment,
        cluster.version,
        cluster.connectionMode,
        cluster.health?.status,
        clusterType,
        formatClusterType(clusterType, localeCode),
      ].filter(Boolean).join(' ').toLowerCase()

      if (keyword && !haystack.includes(keyword)) return false
      if (appliedStatusFilter && (cluster.health?.status || 'unknown') !== appliedStatusFilter) return false
      if (appliedTypeFilter && clusterType !== appliedTypeFilter) return false
      if (appliedModeFilter && cluster.connectionMode !== appliedModeFilter) return false
      return true
    })
  }, [appliedModeFilter, appliedSearchText, appliedStatusFilter, appliedTypeFilter, clusters, localeCode])

  const columns: TableColumnsType<Cluster> = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 180,
      render: (_: unknown, record: Cluster) => (
        <div className="soha-cluster-name-cell">
          <Button className="soha-cluster-name-link" type="link" onClick={() => navigate(`/clusters/${record.id}`)}>
            {record.name}
          </Button>
        </div>
      ),
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'health',
      width: 104,
      render: (health: Cluster['health']) => {
        const status = health?.status ?? 'unknown'
        return (
          <span className={`soha-cluster-status is-${clusterHealthTone(status)}`}>
            <span className="soha-cluster-status-dot" />
            {formatClusterHealth(status, localeCode)}
          </span>
        )
      },
    },
    {
      title: localeCode === 'zh_CN' ? '类型' : 'Type',
      dataIndex: 'region',
      width: 120,
      render: (_: string, record: Cluster) => formatClusterType(clusterTypeOf(record), localeCode),
    },
    { title: localeCode === 'zh_CN' ? '版本' : 'Version', dataIndex: 'version', width: 124, render: (value: string) => value || '-' },
    { title: 'Env', dataIndex: 'environment', width: 132, render: (value: string) => value || '-' },
    {
      title: localeCode === 'zh_CN' ? '连接方式' : 'Mode',
      dataIndex: 'connectionMode',
      width: 104,
      render: (value: string) => <Tag>{formatConnectionMode(value, localeCode)}</Tag>,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked',
      dataIndex: ['health', 'lastChecked'],
      render: (_: unknown, record: Cluster) => record.health?.lastChecked ? formatDateTime(record.health.lastChecked) : '-',
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      width: 112,
      render: (_: unknown, record: Cluster) => (
        <Space size={2} className="soha-cluster-action-links">
          <ManagementIconButton
            aria-label={localeCode === 'zh_CN' ? '查看集群详情' : 'View cluster detail'}
            icon={<EyeOutlined />}
            size="small"
            tooltip={localeCode === 'zh_CN' ? '详情' : 'Detail'}
            onClick={() => navigate(`/clusters/${record.id}`)}
          />
          <ManagementIconButton
            aria-label={localeCode === 'zh_CN' ? '编辑集群' : 'Edit cluster'}
            icon={<EditOutlined />}
            size="small"
            tooltip={localeCode === 'zh_CN' ? '编辑' : 'Edit'}
            onClick={() => {
              setEditingCluster(record)
              setModalVisible(true)
            }}
          />
          <Popconfirm
            title={localeCode === 'zh_CN' ? `确认删除集群 ${record.name}？` : `Delete cluster ${record.name}?`}
            description={localeCode === 'zh_CN' ? '删除后会移除该集群在 Soha 中的注册信息。' : 'This removes the cluster registration from Soha.'}
            okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
            cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
            okButtonProps={{ danger: true, loading: deleteMutation.isPending && deleteMutation.variables === record.id }}
            placement="topRight"
            onConfirm={() => deleteMutation.mutate(record.id)}
          >
            <ManagementIconButton
              aria-label={localeCode === 'zh_CN' ? '删除集群' : 'Delete cluster'}
              danger
              icon={<DeleteOutlined />}
              loading={deleteMutation.isPending && deleteMutation.variables === record.id}
              size="small"
              tooltip={localeCode === 'zh_CN' ? '删除' : 'Delete'}
            />
          </Popconfirm>
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

  const openCreateModal = () => {
    setEditingCluster(null)
    setModalVisible(true)
  }

  const hasActiveFilters = Boolean(
    searchText.trim() ||
    statusFilter ||
    typeFilter ||
    modeFilter ||
    appliedSearchText.trim() ||
    appliedStatusFilter ||
    appliedTypeFilter ||
    appliedModeFilter,
  )
  const applyFilters = () => {
    setAppliedSearchText(searchText)
    setAppliedStatusFilter(statusFilter)
    setAppliedTypeFilter(typeFilter)
    setAppliedModeFilter(modeFilter)
  }

  const resetFilters = () => {
    setSearchText('')
    setStatusFilter(undefined)
    setTypeFilter(undefined)
    setModeFilter(undefined)
    setAppliedSearchText('')
    setAppliedStatusFilter(undefined)
    setAppliedTypeFilter(undefined)
    setAppliedModeFilter(undefined)
  }

  const handleBatchDelete = async () => {
    if (selectedRowKeys.length === 0) return
    try {
      await Promise.all(selectedRowKeys.map((id) => api.delete(`/clusters/${id}`)))
      void message.success('集群已删除')
      setSelectedRowKeys([])
      await queryClient.invalidateQueries({ queryKey: ['clusters'] })
    } catch (err) {
      void message.error((err as Error).message)
      throw err
    }
  }

  const openSelectedNodes = () => {
    if (selectedRowKeys.length !== 1) return
    setClusterId(selectedRowKeys[0])
    navigate('/cluster-resources/nodes')
  }

  const queryPanel = (
    <ManagementQueryPanel
      expanded={queryExpanded}
      onFinish={applyFilters}
      actions={(
        <>
          <Button autoInsertSpace={false} disabled={!hasActiveFilters} htmlType="button" onClick={resetFilters}>
            {localeCode === 'zh_CN' ? '重置' : 'Reset'}
          </Button>
          <Button autoInsertSpace={false} htmlType="submit" type="primary">
            {localeCode === 'zh_CN' ? '查询' : 'Search'}
          </Button>
          <Button
            autoInsertSpace={false}
            icon={queryExpanded ? <UpOutlined /> : <DownOutlined />}
            iconPlacement="end"
            type="link"
            onClick={() => setQueryExpanded((current) => !current)}
          >
            {queryExpanded
              ? (localeCode === 'zh_CN' ? '收起' : 'Collapse')
              : (localeCode === 'zh_CN' ? '展开' : 'Expand')}
          </Button>
        </>
      )}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '集群名称' : 'Cluster Name'}>
        <Input
          allowClear
          className="soha-clusters-search-input"
          placeholder={localeCode === 'zh_CN' ? '请输入' : 'Search'}
          value={searchText}
          variant="filled"
          onChange={(event) => setSearchText(event.target.value)}
          onPressEnter={applyFilters}
        />
      </ManagementQueryField>
      <ManagementQueryField label={localeCode === 'zh_CN' ? '集群类型' : 'Cluster Type'}>
        <Select
          allowClear
          className="soha-clusters-filter-control"
          options={typeOptions}
          placeholder={localeCode === 'zh_CN' ? '请选择' : 'Select'}
          value={typeFilter}
          variant="filled"
          onChange={(value) => setTypeFilter(value)}
        />
      </ManagementQueryField>
      {queryExpanded ? (
        <>
          <ManagementQueryField label={localeCode === 'zh_CN' ? '状态' : 'Status'}>
            <Select
              allowClear
              className="soha-clusters-filter-control"
              options={statusOptions}
              placeholder={localeCode === 'zh_CN' ? '请选择' : 'Select'}
              value={statusFilter}
              variant="filled"
              onChange={(value) => setStatusFilter(value)}
            />
          </ManagementQueryField>
          <ManagementQueryField label={localeCode === 'zh_CN' ? '连接方式' : 'Mode'}>
            <Select
              allowClear
              className="soha-clusters-filter-control"
              options={modeOptions}
              placeholder={localeCode === 'zh_CN' ? '请选择' : 'Select'}
              value={modeFilter}
              variant="filled"
              onChange={(value) => setModeFilter(value)}
            />
          </ManagementQueryField>
        </>
      ) : null}
    </ManagementQueryPanel>
  )

  const tableToolbarExtra = selectedRowKeys.length > 0 ? (
    <ManagementBatchBar
      selectedCount={selectedRowKeys.length}
      selectedLabel={localeCode === 'zh_CN' ? `已选 ${selectedRowKeys.length} 项` : `${selectedRowKeys.length} selected`}
    >
      <Button autoInsertSpace={false} size="small" onClick={openSelectedNodes} disabled={selectedRowKeys.length !== 1}>
        {localeCode === 'zh_CN' ? '查看节点' : 'Open Nodes'}
      </Button>
      <Popconfirm
        title={localeCode === 'zh_CN' ? `确认删除 ${selectedRowKeys.length} 个集群？` : `Delete ${selectedRowKeys.length} clusters?`}
        description={localeCode === 'zh_CN' ? '删除后会移除这些集群在 Soha 中的注册信息。' : 'This removes these cluster registrations from Soha.'}
        okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
        cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
        okButtonProps={{ danger: true }}
        placement="top"
        onConfirm={handleBatchDelete}
      >
        <Button autoInsertSpace={false} size="small" danger>
          {localeCode === 'zh_CN' ? '批量删除' : 'Batch Delete'}
        </Button>
      </Popconfirm>
      <Button autoInsertSpace={false} size="small" type="text" onClick={() => setSelectedRowKeys([])}>
        {localeCode === 'zh_CN' ? '清空' : 'Clear'}
      </Button>
    </ManagementBatchBar>
  ) : null

  const tableHeaderExtra = (
    <ManagementTableToolbar batchBar={tableToolbarExtra}>
      <Button
        autoInsertSpace={false}
        className="soha-clusters-create-button"
        icon={<PlusOutlined />}
        type="primary"
        onClick={openCreateModal}
      >
        {t('common.create', 'Create')}
      </Button>
      <ManagementDensityButton
        aria-label={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
        title={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
        tooltip={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
        onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
      />
      <ManagementRefreshButton
        aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
        loading={isFetching}
        title={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
        tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
        onClick={() => void refetch()}
      />
    </ManagementTableToolbar>
  )

  return (
    <div className="soha-page soha-clusters-page">
      {queryPanel}
      <AdminTable
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={tableHeaderExtra}
        columns={columns}
        dataSource={filteredClusters}
        rowKey="id"
        loading={isLoading}
        paginationSummary={localeCode === 'zh_CN' ? `当前 ${filteredClusters.length} / ${clusters.length} 条` : `${filteredClusters.length} / ${clusters.length} items`}
        pageSize={20}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        rowSelection={{
          selectedRowKeys,
          onChange: (nextSelectedRowKeys: Key[]) => {
            setSelectedRowKeys(nextSelectedRowKeys.filter((key): key is string => typeof key === 'string'))
          },
        }}
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
        <ManagementDetailHeader
          title={localeCode === 'zh_CN' ? '集群详情' : 'Cluster Detail'}
          description={localeCode === 'zh_CN' ? '当前集群不存在或详情不可用。' : 'The cluster was not found or its detail is unavailable.'}
        />
        <ManagementState kind="not-found" description={t('common.notFound', 'Not found')} />
      </div>
    )
  }

  return (
    <div className="soha-page soha-cluster-detail-page">
      <ManagementDetailHeader
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

      <div className="soha-cluster-detail-grid">
        <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '基础信息' : 'Summary'}>
          <Descriptions
            size="small"
            items={[
              { key: localeCode === 'zh_CN' ? '名称' : 'Name', label: localeCode === 'zh_CN' ? '名称' : 'Name', children: summary.name },
              { key: localeCode === 'zh_CN' ? '状态' : 'Status', label: localeCode === 'zh_CN' ? '状态' : 'Status', children: <StatusTag value={summary.health?.status ?? 'unknown'} /> },
              { key: localeCode === 'zh_CN' ? '版本' : 'Version', label: localeCode === 'zh_CN' ? '版本' : 'Version', children: summary.version || '-' },
              { key: localeCode === 'zh_CN' ? '类型' : 'Type', label: localeCode === 'zh_CN' ? '类型' : 'Type', children: formatClusterType(clusterTypeOf(summary), localeCode) },
              { key: 'Environment', label: 'Environment', children: summary.environment || '-' },
              { key: localeCode === 'zh_CN' ? '连接方式' : 'Mode', label: localeCode === 'zh_CN' ? '连接方式' : 'Mode', children: summary.connectionMode || '-' },
              { key: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked', label: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked', children: summary.health?.lastChecked ? formatDateTime(summary.health.lastChecked) : '-' },
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
            size="small"
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
          size="small"
          items={[
            { key: localeCode === 'zh_CN' ? 'Prometheus URL' : 'Prometheus URL', label: localeCode === 'zh_CN' ? 'Prometheus URL' : 'Prometheus URL', children: detail.monitoring.prometheus.baseUrl || '-' },
            { key: localeCode === 'zh_CN' ? 'Prometheus Cluster Label' : 'Prometheus Cluster Label', label: localeCode === 'zh_CN' ? 'Prometheus Cluster Label' : 'Prometheus Cluster Label', children: detail.monitoring.prometheus.clusterLabel || '-' },
            { key: localeCode === 'zh_CN' ? 'Bearer Token' : 'Bearer Token', label: localeCode === 'zh_CN' ? 'Bearer Token' : 'Bearer Token', children: detail.monitoring.prometheus.hasBearerToken ? (localeCode === 'zh_CN' ? '已配置' : 'Configured') : (localeCode === 'zh_CN' ? '未配置' : 'Not configured') },
            { key: localeCode === 'zh_CN' ? 'Grafana URL' : 'Grafana URL', label: localeCode === 'zh_CN' ? 'Grafana URL' : 'Grafana URL', children: detail.monitoring.prometheus.grafanaBaseUrl || '-' },
          ]}
        />
      </Card>

      <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '节点快照' : 'Node Snapshot'}>
        {nodesQuery.isError ? (
          <ManagementState
            compact
            description={(nodesQuery.error as Error)?.message || (localeCode === 'zh_CN' ? '节点快照加载失败' : 'Failed to load node snapshot')}
            kind="error"
            title={localeCode === 'zh_CN' ? '节点快照加载失败' : 'Failed to load node snapshot'}
          />
        ) : (
          <AdminTable
            shellClassName="soha-management-table-shell"
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
