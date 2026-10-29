import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementBatchBar,
  ManagementDensityButton,
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { encodeAIContextForElement, useAIPageContext } from '@/features/copilot'
import {
  platformBatchReceipt,
  platformBatchUnknown,
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import {
  CloudUploadOutlined,
  CopyOutlined,
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  EyeOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import type { AgentInstallation } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  App,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Typography,
} from 'antd'
import type { Key } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { AgentUpgradeModal } from './agent-upgrade-modal'
import { clusterMutations } from './mutations'
import {
  clusterTypeOf,
  clusterTypeOptions,
  formatClusterHealth,
  formatClusterType,
  formatConnectionMode,
} from './presentation'
import { clusterQueries } from './queries'
import './styles.css'
import type { Cluster, ClusterFormValues, ConnectionMode } from './types'

const { Text } = Typography

export function ClustersPage() {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [modalVisible, setModalVisible] = useState(false)
  const [agentInstallation, setAgentInstallation] = useState<AgentInstallation | null>(null)
  const [upgradingCluster, setUpgradingCluster] = useState<Cluster | null>(null)
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
  const [selectedRowKeys, setSelectedRowKeys] = useState<string[]>([])
  const [tableSize, setTableSize] = useState<'middle' | 'small'>('small')
  const setClusterId = usePlatformScopeStore((state) => state.setClusterId)
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const canCreate = hasPermission(permissionSnapshot, 'platform.clusters.create')
  const canUpdate = hasPermission(permissionSnapshot, 'platform.clusters.update')
  const canDelete = hasPermission(permissionSnapshot, 'platform.clusters.delete')

  const clustersQuery = useQuery(clusterQueries.list())
  const visibleClustersQueryData = readableQueryData(clustersQuery)
  const editingScope = toScopeKey(editingCluster?.id, null)
  const editDetailOptions = clusterQueries.detail(editingScope)
  const clusterDetailQuery = useQuery({
    ...editDetailOptions,
    enabled: Boolean(editDetailOptions.enabled) && modalVisible && Boolean(editingCluster),
  })
  const visibleClusterDetailQueryData =
    Boolean(editDetailOptions.enabled) && modalVisible && Boolean(editingCluster)
      ? readableQueryData(clusterDetailQuery)
      : undefined
  const createMutation = usePlatformMutation(clusterMutations.create(queryClient), {
    label: ['创建集群', 'Create cluster'],
    target: (v) => String(v.name || 'Cluster'),
  })
  const agentInstallationMutation = usePlatformMutation(
    clusterMutations.createAgentInstallation(),
    {
      label: ['生成 Agent 安装配置', 'Generate Agent installation'],
      target: platformResourceTarget,
    },
  )
  const updateMutation = usePlatformMutation(clusterMutations.update(queryClient), {
    label: ['更新资源', 'Update resource'],
    target: platformResourceTarget,
  })
  const deleteMutation = usePlatformMutation(clusterMutations.remove(queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const batchDeleteMutation = usePlatformMutation(clusterMutations.removeMany(queryClient), {
    label: ['批量删除集群', 'Delete clusters'],
    target: (v) => v.scopes.map((scope) => platformResourceTarget({ scope })).join(' · '),
    resultError: platformBatchUnknown,
    receipt: (results, variables) =>
      platformBatchReceipt(
        results,
        variables.scopes.map((scope) => platformResourceTarget({ scope })),
        localeCode,
      ),
  })
  const platformWriteBlocked =
    createMutation.blocked ||
    agentInstallationMutation.blocked ||
    updateMutation.blocked ||
    deleteMutation.blocked ||
    batchDeleteMutation.blocked

  const clusters = visibleClustersQueryData ?? []
  const targetFor = (id: string) => ({ scope: toScopeKey(id, null) })
  const showAgentInstallation = (clusterId: string) => {
    agentInstallationMutation.mutate(targetFor(clusterId), {
      onSuccess: setAgentInstallation,
    })
  }

  useEffect(() => {
    const clusterIds = new Set(clusters.map((cluster) => cluster.id))
    setSelectedRowKeys((current) => {
      const next = current.filter((id) => clusterIds.has(id))
      return next.length === current.length ? current : next
    })
  }, [clusters])

  const statusOptions = useMemo(() => {
    const values = Array.from(
      new Set(clusters.map((cluster) => cluster.health?.status || 'unknown')),
    )
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
    const values = Array.from(
      new Set(clusters.map((cluster) => cluster.connectionMode).filter(Boolean)),
    )
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
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()

      if (keyword && !haystack.includes(keyword)) return false
      if (appliedStatusFilter && (cluster.health?.status || 'unknown') !== appliedStatusFilter)
        return false
      if (appliedTypeFilter && clusterType !== appliedTypeFilter) return false
      if (appliedModeFilter && cluster.connectionMode !== appliedModeFilter) return false
      return true
    })
  }, [
    appliedModeFilter,
    appliedSearchText,
    appliedStatusFilter,
    appliedTypeFilter,
    clusters,
    localeCode,
  ])
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: localeCode === 'zh_CN' ? '集群' : 'Clusters',
    entityKind: 'Cluster',
    visibleFilters: {
      keyword: appliedSearchText || undefined,
      status: appliedStatusFilter,
      type: appliedTypeFilter,
      mode: appliedModeFilter,
    },
    pinnedData: { itemCount: clusters.length },
  })

  const columns: TableColumnsType<Cluster> = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 180,
      render: (_: unknown, record: Cluster) => (
        <div className="soha-cluster-name-cell">
          <Button
            className="soha-cluster-name-link"
            type="link"
            onClick={() => navigate(`/clusters/${record.id}`)}
          >
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
        return <StatusTag label={formatClusterHealth(status, localeCode)} value={status} />
      },
    },
    {
      title: localeCode === 'zh_CN' ? '类型' : 'Type',
      dataIndex: 'region',
      width: 120,
      render: (_: string, record: Cluster) => formatClusterType(clusterTypeOf(record), localeCode),
    },
    {
      title: localeCode === 'zh_CN' ? '版本' : 'Version',
      dataIndex: 'version',
      width: 124,
      render: (value: string) => value || '-',
    },
    { title: 'Env', dataIndex: 'environment', width: 132, render: (value: string) => value || '-' },
    {
      title: localeCode === 'zh_CN' ? '连接方式' : 'Mode',
      dataIndex: 'connectionMode',
      width: 104,
      render: (value: string) => <MetadataTag label={formatConnectionMode(value, localeCode)} />,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked',
      dataIndex: ['health', 'lastChecked'],
      render: (_: unknown, record: Cluster) =>
        record.health?.lastChecked ? formatDateTime(record.health.lastChecked) : '-',
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      width: 144,
      render: (_: unknown, record: Cluster) => (
        <Space size={2} className="soha-cluster-action-links">
          <ManagementIconButton
            aria-label={localeCode === 'zh_CN' ? '查看集群详情' : 'View cluster detail'}
            icon={<EyeOutlined />}
            size="small"
            tooltip={localeCode === 'zh_CN' ? '详情' : 'Detail'}
            onClick={() => navigate(`/clusters/${record.id}`)}
          />
          {canUpdate ? (
            record.connectionMode === 'agent' ? (
              <ManagementIconButton
                aria-label={
                  localeCode === 'zh_CN' ? '生成 Agent 安装命令' : 'Generate Agent install command'
                }
                icon={<DownloadOutlined />}
                loading={
                  agentInstallationMutation.isPending &&
                  agentInstallationMutation.variables?.scope.clusterId === record.id
                }
                size="small"
                tooltip={localeCode === 'zh_CN' ? '安装 Agent' : 'Install Agent'}
                onClick={() => showAgentInstallation(record.id)}
              />
            ) : null
          ) : null}
          {canUpdate ? (
            <ManagementIconButton
              disabled={platformWriteBlocked}
              aria-label={localeCode === 'zh_CN' ? '编辑集群' : 'Edit cluster'}
              icon={<EditOutlined />}
              size="small"
              tooltip={localeCode === 'zh_CN' ? '编辑' : 'Edit'}
              onClick={() => {
                setEditingCluster(record)
                setModalVisible(true)
              }}
            />
          ) : null}
          {canUpdate && record.connectionMode === 'agent' ? (
            <ManagementIconButton
              aria-label={localeCode === 'zh_CN' ? '更新 Agent' : 'Update Agent'}
              icon={<CloudUploadOutlined />}
              size="small"
              tooltip={localeCode === 'zh_CN' ? '更新 Agent' : 'Update Agent'}
              onClick={() => setUpgradingCluster(record)}
            />
          ) : null}
          {canDelete ? (
            <Popconfirm
              title={
                localeCode === 'zh_CN'
                  ? `确认删除集群 ${record.name}？`
                  : `Delete cluster ${record.name}?`
              }
              description={
                localeCode === 'zh_CN'
                  ? '删除后会移除该集群在 Soha 中的注册信息。'
                  : 'This removes the cluster registration from Soha.'
              }
              okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
              cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
              okButtonProps={{
                disabled: platformWriteBlocked,
                danger: true,
                loading:
                  deleteMutation.isPending &&
                  deleteMutation.variables?.scope.clusterId === record.id,
              }}
              placement="topRight"
              onConfirm={() => deleteMutation.mutate(targetFor(record.id), {})}
            >
              <ManagementIconButton
                disabled={platformWriteBlocked}
                aria-label={localeCode === 'zh_CN' ? '删除集群' : 'Delete cluster'}
                danger
                icon={<DeleteOutlined />}
                loading={
                  deleteMutation.isPending &&
                  deleteMutation.variables?.scope.clusterId === record.id
                }
                size="small"
                tooltip={localeCode === 'zh_CN' ? '删除' : 'Delete'}
              />
            </Popconfirm>
          ) : null}
        </Space>
      ),
    },
  ]

  useEffect(() => {
    if (!modalVisible) return
    if (editingCluster) {
      setConnectionMode(
        (visibleClusterDetailQueryData?.connection.mode as ConnectionMode) ||
          (editingCluster.connectionMode as ConnectionMode) ||
          'direct_kubeconfig',
      )
      return
    }
    setConnectionMode('direct_kubeconfig')
  }, [modalVisible, editingCluster, visibleClusterDetailQueryData])

  const initialValues = useMemo<ClusterFormValues>(() => {
    if (!editingCluster) {
      return {
        connectionMode: 'direct_kubeconfig',
        prometheusTransport: 'direct',
        agentCustomResourceRules: [],
        agentActionProfile: 'workbench',
        provider: 'standard_kubernetes',
      }
    }
    const detail = visibleClusterDetailQueryData
    return {
      name: editingCluster.name,
      provider: clusterTypeOf(editingCluster) || undefined,
      environment: editingCluster.environment,
      connectionMode:
        ((detail?.connection.mode || editingCluster.connectionMode) as ConnectionMode) ||
        'direct_kubeconfig',
      prometheusTransport: detail?.monitoring.prometheus.transport || 'direct',
      agentCustomResourceRules: detail?.connection.customResourceRules || [],
      agentActionProfile: detail?.connection.actionProfile || 'legacy',
      prometheusBaseUrl: detail?.monitoring.prometheus.baseUrl || '',
    }
  }, [editingCluster, visibleClusterDetailQueryData])

  const formKey = editingCluster
    ? `cluster-edit:${editingCluster.id}:${visibleClusterDetailQueryData ? 'ready' : 'loading'}`
    : 'cluster-create'

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
      prometheusTransport:
        values.connectionMode === 'agent' ? values.prometheusTransport : 'direct',
      labels,
    }
    delete (payload as Record<string, unknown>).provider
    if (editingCluster) {
      updateMutation.mutate(
        { scope: toScopeKey(editingCluster.id, null), values: payload },
        {
          onSuccess: () => {
            setModalVisible(false)
            setEditingCluster(null)
          },
        },
      )
    } else {
      createMutation.mutate(payload, {
        onSuccess: (cluster) => {
          setModalVisible(false)
          if (values.connectionMode === 'agent') {
            showAgentInstallation(cluster.id)
          }
        },
      })
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
      const scopes = selectedRowKeys.map((id) => toScopeKey(id, null))
      const results = await batchDeleteMutation.mutateAsync({
        scopes,
      })
      setSelectedRowKeys(
        scopes
          .filter((_scope, index) => results[index].status === 'rejected')
          .map((scope) => scope.clusterId)
          .filter((id): id is string => Boolean(id)),
      )
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
      onFinish={applyFilters}
      actions={
        <ManagementQueryActions
          disabledReset={!hasActiveFilters}
          onReset={resetFilters}
          resetLabel={localeCode === 'zh_CN' ? '重置' : 'Reset'}
          submitLabel={localeCode === 'zh_CN' ? '查询' : 'Search'}
        />
      }
    >
      <ManagementKeywordField
        label={localeCode === 'zh_CN' ? '集群名称' : 'Cluster Name'}
        placeholder={localeCode === 'zh_CN' ? '请输入' : 'Search'}
        value={searchText}
        onChange={setSearchText}
        inputProps={{
          onPressEnter: applyFilters,
        }}
      />
      <ManagementQueryField label={localeCode === 'zh_CN' ? '集群类型' : 'Cluster Type'}>
        <Select
          allowClear
          options={typeOptions}
          placeholder={localeCode === 'zh_CN' ? '请选择' : 'Select'}
          value={typeFilter}
          onChange={(value) => setTypeFilter(value)}
        />
      </ManagementQueryField>
      <ManagementQueryField label={localeCode === 'zh_CN' ? '状态' : 'Status'}>
        <Select
          allowClear
          options={statusOptions}
          placeholder={localeCode === 'zh_CN' ? '请选择' : 'Select'}
          value={statusFilter}
          onChange={(value) => setStatusFilter(value)}
        />
      </ManagementQueryField>
      <ManagementQueryField label={localeCode === 'zh_CN' ? '连接方式' : 'Mode'}>
        <Select
          allowClear
          options={modeOptions}
          placeholder={localeCode === 'zh_CN' ? '请选择' : 'Select'}
          value={modeFilter}
          onChange={(value) => setModeFilter(value)}
        />
      </ManagementQueryField>
    </ManagementQueryPanel>
  )

  const tableToolbarExtra =
    selectedRowKeys.length > 0 ? (
      <ManagementBatchBar
        selectedCount={selectedRowKeys.length}
        selectedLabel={
          localeCode === 'zh_CN'
            ? `已选 ${selectedRowKeys.length} 项`
            : `${selectedRowKeys.length} selected`
        }
      >
        <Button
          autoInsertSpace={false}
          size="small"
          onClick={openSelectedNodes}
          disabled={selectedRowKeys.length !== 1}
        >
          {localeCode === 'zh_CN' ? '查看节点' : 'Open Nodes'}
        </Button>
        {canDelete ? (
          <Popconfirm
            title={
              localeCode === 'zh_CN'
                ? `确认删除 ${selectedRowKeys.length} 个集群？`
                : `Delete ${selectedRowKeys.length} clusters?`
            }
            description={
              localeCode === 'zh_CN'
                ? '删除后会移除这些集群在 Soha 中的注册信息。'
                : 'This removes these cluster registrations from Soha.'
            }
            okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
            cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
            okButtonProps={{ disabled: platformWriteBlocked, danger: true }}
            placement="top"
            onConfirm={handleBatchDelete}
          >
            <Button autoInsertSpace={false} size="small" danger disabled={platformWriteBlocked}>
              {localeCode === 'zh_CN' ? '批量删除' : 'Batch Delete'}
            </Button>
          </Popconfirm>
        ) : null}
        <Button
          autoInsertSpace={false}
          size="small"
          type="text"
          onClick={() => setSelectedRowKeys([])}
        >
          {localeCode === 'zh_CN' ? '清空' : 'Clear'}
        </Button>
      </ManagementBatchBar>
    ) : null

  const tableHeaderExtra = (
    <ManagementTableToolbar batchBar={tableToolbarExtra}>
      {canCreate ? (
        <Button
          autoInsertSpace={false}
          className="soha-clusters-create-button"
          disabled={platformWriteBlocked}
          icon={<PlusOutlined />}
          type="primary"
          onClick={openCreateModal}
        >
          {t('common.create', 'Create')}
        </Button>
      ) : null}
      <ManagementDensityButton
        aria-label={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
        title={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
        tooltip={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
        onClick={() => setTableSize((current) => (current === 'middle' ? 'small' : 'middle'))}
      />
      <ManagementRefreshButton
        aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
        loading={clustersQuery.isFetching}
        title={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
        tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
        onClick={() => void clustersQuery.refetch()}
      />
    </ManagementTableToolbar>
  )

  return (
    <div className="soha-page soha-clusters-page">
      <ReadFeedback
        canRead={clusterDetailQuery.isEnabled}
        query={clusterDetailQuery}
        label="集群详情"
      />

      {createMutation.feedback}
      {agentInstallationMutation.feedback}
      {updateMutation.feedback}
      {deleteMutation.feedback}
      {batchDeleteMutation.feedback}

      {queryPanel}
      {upgradingCluster ? (
        <AgentUpgradeModal
          key={upgradingCluster.id}
          cluster={upgradingCluster}
          onClose={() => setUpgradingCluster(null)}
        />
      ) : null}
      <KubernetesResourceTable
        resourceKey={'clusters'}
        resourceLabel={t('common.clusters', 'Clusters')}
        allRows={clusters}
        filterKey={JSON.stringify([
          appliedSearchText,
          appliedStatusFilter,
          appliedTypeFilter,
          appliedModeFilter,
        ])}
        scopeMode={'global'}
        error={clustersQuery.isEnabled === false ? null : clustersQuery.error}
        hasReadData={clustersQuery.data !== undefined}
        onRetry={clustersQuery.isEnabled === false ? undefined : () => clustersQuery.refetch()}
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={tableHeaderExtra}
        columns={columns}
        dataSource={filteredClusters}
        onRow={(record: Cluster) => ({
          'data-ai-context': encodeAIContextForElement({
            sourceWorkbench: 'platform',
            entityKind: 'Cluster',
            entityName: record.name,
            clusterId: record.id,
          }),
        })}
        rowKey="id"
        loading={clustersQuery.isLoading}
        localSorting
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        rowSelection={{
          selectedRowKeys,
          onChange: (nextSelectedRowKeys: Key[]) => {
            setSelectedRowKeys(
              nextSelectedRowKeys.filter((key): key is string => typeof key === 'string'),
            )
          },
        }}
      />

      <Modal
        okButtonProps={{ disabled: platformWriteBlocked }}
        title={localeCode === 'zh_CN' ? 'Agent 安装命令' : 'Agent install command'}
        open={Boolean(agentInstallation)}
        destroyOnHidden
        onCancel={() => {
          if (!platformWriteBlocked) (() => setAgentInstallation(null))?.()
        }}
        footer={null}
      >
        {createMutation.feedback}
        {agentInstallationMutation.feedback}
        {updateMutation.feedback}
        {deleteMutation.feedback}
        {batchDeleteMutation.feedback}

        {agentInstallation ? (
          <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
            <Input.TextArea
              value={agentInstallation.command}
              readOnly
              autoSize={{ minRows: 2, maxRows: 5 }}
            />
            <Space wrap>
              <Button
                icon={<CopyOutlined />}
                onClick={() => {
                  void navigator.clipboard
                    .writeText(agentInstallation.command)
                    .then(() =>
                      message.success(localeCode === 'zh_CN' ? '命令已复制' : 'Command copied'),
                    )
                    .catch(() => message.error(localeCode === 'zh_CN' ? '复制失败' : 'Copy failed'))
                }}
              >
                {localeCode === 'zh_CN' ? '复制' : 'Copy'}
              </Button>
              <Button
                icon={<ReloadOutlined />}
                loading={agentInstallationMutation.isPending}
                onClick={() => showAgentInstallation(agentInstallation.clusterId)}
              >
                {localeCode === 'zh_CN' ? '刷新命令' : 'Refresh command'}
              </Button>
              <Text type="secondary">
                {localeCode === 'zh_CN' ? '有效期至' : 'Expires'}{' '}
                {formatDateTime(agentInstallation.expiresAt)}
              </Text>
            </Space>
            <div className="soha-form-actions" style={{ marginTop: 0 }}>
              <Button type="primary" onClick={() => setAgentInstallation(null)}>
                {localeCode === 'zh_CN' ? '完成' : 'Done'}
              </Button>
            </div>
          </Space>
        ) : null}
      </Modal>

      <Modal
        okButtonProps={{ disabled: platformWriteBlocked }}
        destroyOnHidden
        title={editingCluster ? '编辑集群' : '添加集群'}
        open={modalVisible}
        width={760}
        styles={{ body: { maxHeight: 'calc(100vh - 220px)', overflowY: 'auto' } }}
        onCancel={() => {
          if (!platformWriteBlocked)
            (() => {
              setModalVisible(false)
              setEditingCluster(null)
            })?.()
        }}
        footer={null}
      >
        {createMutation.feedback}
        {agentInstallationMutation.feedback}
        {updateMutation.feedback}
        {deleteMutation.feedback}
        {batchDeleteMutation.feedback}

        {editingCluster && clusterDetailQuery.isLoading && !visibleClusterDetailQueryData ? (
          <div className="flex items-center justify-center h-64">
            <Spin size="large" />
          </div>
        ) : (
          <Form
            key={formKey}
            layout="vertical"
            onFinish={handleSubmit}
            initialValues={initialValues}
            preserve={false}
          >
            <Form.Item
              name="name"
              label="集群名称"
              rules={[{ required: true, message: '请输入集群名称' }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="provider"
              label={localeCode === 'zh_CN' ? '集群类型' : 'Cluster Type'}
              rules={[
                {
                  required: true,
                  message: localeCode === 'zh_CN' ? '请选择集群类型' : 'Select a cluster type',
                },
              ]}
            >
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
                rules={
                  editingCluster ? undefined : [{ required: true, message: '请输入 Kubeconfig' }]
                }
              >
                <Input.TextArea
                  placeholder={
                    editingCluster ? '留空则沿用现有 kubeconfig' : '请输入 kubeconfig 内容'
                  }
                  rows={8}
                />
              </Form.Item>
            ) : null}

            {connectionMode === 'agent' ? (
              <Form.Item
                name="agentActionProfile"
                label={localeCode === 'zh_CN' ? 'Agent 动作预设' : 'Agent action preset'}
                extra={
                  localeCode === 'zh_CN'
                    ? '工作台预设启用资源 CRUD、维护/排水、终端、转发与 Helm；只读预设关闭写入和执行。保存不会改变运行中的 Agent，需重新生成并核对安装清单后应用。CRD 定义删除仍需单独启用。'
                    : 'Workbench enables resource CRUD, maintenance/drain, terminal, forwarding and Helm; read-only disables writes and execution. Saving does not change a running Agent. Regenerate and review its installation manifest before applying. CRD definition deletion remains a separate opt-in.'
                }
              >
                <Select
                  options={[
                    { value: 'workbench', label: localeCode === 'zh_CN' ? '工作台' : 'Workbench' },
                    { value: 'read-only', label: localeCode === 'zh_CN' ? '只读' : 'Read-only' },
                    {
                      value: 'legacy',
                      label:
                        localeCode === 'zh_CN'
                          ? '保留原安装权限'
                          : 'Preserve legacy installation permissions',
                    },
                  ]}
                />
              </Form.Item>
            ) : null}

            <Card className="soha-detail-card">
              <div className="soha-detail-meta">
                <Text strong>Prometheus</Text>
              </div>
              <Form.Item
                name="prometheusTransport"
                label="访问方式"
                extra={
                  connectionMode === 'agent'
                    ? '选择 Agent 时，保存后请重新应用 Agent 安装清单，以同步 Prometheus 地址和凭据。'
                    : undefined
                }
              >
                <Select
                  options={[
                    { value: 'direct', label: 'Core 直连' },
                    ...(connectionMode === 'agent'
                      ? [{ value: 'agent', label: 'Agent 代理' }]
                      : []),
                  ]}
                />
              </Form.Item>
              <Form.Item name="prometheusBaseUrl" label="Prometheus URL">
                <Input placeholder="http://prometheus:9090" />
              </Form.Item>
              <Form.Item name="prometheusBearerToken" label="Prometheus Token">
                <Input.Password placeholder={editingCluster ? '留空则沿用现有 token' : ''} />
              </Form.Item>
            </Card>

            {connectionMode === 'agent' ? (
              <Card className="soha-detail-card" title="自定义资源授权">
                <Text type="secondary">
                  按 API Group 和资源名称授权。保存后重新应用 Agent
                  安装清单；移除命名空间授权时，还需删除旧的 Role 和 RoleBinding。
                </Text>
                <Form.List name="agentCustomResourceRules">
                  {(fields, { add, remove }) => (
                    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
                      {fields.map(({ key, name, ...rest }) => (
                        <div key={key}>
                          <Form.Item
                            {...rest}
                            preserve
                            name={[name, 'apiGroup']}
                            label="API Group"
                            rules={[{ required: true }]}
                          >
                            <Input placeholder="example.io" />
                          </Form.Item>
                          <Form.Item
                            {...rest}
                            preserve
                            name={[name, 'resources']}
                            label="资源名称（复数）"
                            rules={[{ required: true }]}
                          >
                            <Select mode="tags" placeholder="widgets" />
                          </Form.Item>
                          <Form.Item
                            {...rest}
                            preserve
                            name={[name, 'verbs']}
                            label="权限"
                            rules={[{ required: true }]}
                          >
                            <Select
                              mode="multiple"
                              options={[
                                'get',
                                'list',
                                'watch',
                                'create',
                                'update',
                                'patch',
                                'delete',
                              ].map((value) => ({ value, label: value }))}
                            />
                          </Form.Item>
                          <Form.Item
                            {...rest}
                            preserve
                            name={[name, 'namespaces']}
                            label="命名空间"
                            extra="留空表示整个集群，包括集群级资源；填写后只授权指定命名空间。"
                          >
                            <Select mode="tags" placeholder="apps" />
                          </Form.Item>
                          <Button onClick={() => remove(name)} danger>
                            移除授权
                          </Button>
                        </div>
                      ))}
                      <Button
                        icon={<PlusOutlined />}
                        onClick={() =>
                          add({
                            apiGroup: '',
                            resources: [],
                            verbs: ['get', 'list', 'watch'],
                            namespaces: [],
                          })
                        }
                      >
                        添加授权
                      </Button>
                    </Space>
                  )}
                </Form.List>
              </Card>
            ) : null}

            <div className="soha-form-actions">
              <Button onClick={() => setModalVisible(false)}>取消</Button>
              <Button
                htmlType="submit"
                type="primary"
                loading={createMutation.isPending || updateMutation.isPending}
              >
                {editingCluster ? '更新' : '创建'}
              </Button>
            </div>
          </Form>
        )}
      </Modal>
    </div>
  )
}
