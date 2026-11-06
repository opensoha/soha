import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementBatchBar,
  ManagementDensityButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementTableToolbar,
} from '@/components/management-list'
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
import { CopyOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { AgentInstallation } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { App, Button, Input, Modal, Popconfirm, Select, Space, Typography } from 'antd'
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
import type { Cluster } from './types'
import { ClusterEditor } from './cluster-editor'
import { KubernetesResourceActionsMenu } from '../shared/kubernetes-resource-actions-menu'
import { WorkloadRefresh, useWorkloadRefresh } from '../workloads/shared/workload-refresh'

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
  const [deletingCluster, setDeletingCluster] = useState<Cluster | null>(null)
  const refresh = useWorkloadRefresh()
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
  const canReadNodes = hasPermission(permissionSnapshot, 'platform.nodes.view')
  const nameCollator = useMemo(
    () => new Intl.Collator(localeCode === 'zh_CN' ? 'zh-CN' : 'en-US', { numeric: true }),
    [localeCode],
  )

  const canRead = hasPermission(permissionSnapshot, 'platform.clusters.view')
  const clustersQuery = useQuery({
    ...clusterQueries.list(canRead),
    refetchInterval: refresh.interval,
  })
  const visibleClustersQueryData = readableQueryData(clustersQuery)
  const agentInstallationMutation = usePlatformMutation(
    clusterMutations.createAgentInstallation(),
    {
      label: ['生成 Agent 安装配置', 'Generate Agent installation'],
      target: platformResourceTarget,
    },
  )
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
    agentInstallationMutation.blocked || deleteMutation.blocked || batchDeleteMutation.blocked

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
    return values.sort().map((value) => ({ value, label: formatClusterHealth(value, localeCode) }))
  }, [clusters, localeCode])

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
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 280,
      fixed: 'left',
      ellipsis: true,
      sorter: (a, b) => nameCollator.compare(a.name, b.name),
      defaultSortOrder: 'ascend',
      render: (_: unknown, record: Cluster) => (
        <div className="soha-cluster-name-cell">
          <Button
            className="soha-cluster-name-link"
            type="link"
            title={record.name}
            onClick={() => navigate(`/clusters/${record.id}`)}
          >
            {record.name}
          </Button>
        </div>
      ),
    },
    {
      ...tableColumnPresets.status,
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'health',
      width: 104,
      sorter: (a, b) =>
        nameCollator.compare(
          formatClusterHealth(a.health?.status || 'unknown', localeCode),
          formatClusterHealth(b.health?.status || 'unknown', localeCode),
        ),
      render: (health: Cluster['health']) => {
        const status = health?.status ?? 'unknown'
        return <StatusTag label={formatClusterHealth(status, localeCode)} value={status} />
      },
    },
    {
      title: localeCode === 'zh_CN' ? '类型' : 'Type',
      dataIndex: 'region',
      width: 120,
      ellipsis: true,
      sorter: (a, b) =>
        nameCollator.compare(
          formatClusterType(clusterTypeOf(a), localeCode),
          formatClusterType(clusterTypeOf(b), localeCode),
        ),
      render: (_: string, record: Cluster) => formatClusterType(clusterTypeOf(record), localeCode),
    },
    {
      title: localeCode === 'zh_CN' ? '版本' : 'Version',
      dataIndex: 'version',
      width: 124,
      ellipsis: true,
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '环境' : 'Environment',
      dataIndex: 'environment',
      width: 132,
      ellipsis: true,
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '连接方式' : 'Mode',
      dataIndex: 'connectionMode',
      width: 104,
      sorter: (a, b) =>
        nameCollator.compare(
          formatConnectionMode(a.connectionMode, localeCode),
          formatConnectionMode(b.connectionMode, localeCode),
        ),
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
      key: 'actions',
      title: '',
      width: 64,
      fixed: 'right',
      className: 'soha-table-actions-column',
      render: (_: unknown, record: Cluster) => (
        <KubernetesResourceActionsMenu
          name={record.name}
          resourceLabel="Cluster"
          localeCode={localeCode}
          items={[
            { key: 'view', label: localeCode === 'zh_CN' ? '查看详情' : 'View details' },
            ...(canUpdate
              ? [
                  {
                    key: 'edit',
                    label: localeCode === 'zh_CN' ? '编辑集群' : 'Edit cluster',
                    disabled: platformWriteBlocked,
                  },
                  ...(record.connectionMode === 'agent'
                    ? [
                        {
                          key: 'install',
                          label:
                            localeCode === 'zh_CN'
                              ? '生成 Agent 安装命令'
                              : 'Generate Agent install command',
                          disabled: platformWriteBlocked,
                        },
                        {
                          key: 'upgrade',
                          label: localeCode === 'zh_CN' ? '更新 Agent' : 'Update Agent',
                          disabled: platformWriteBlocked,
                        },
                      ]
                    : []),
                ]
              : []),
            ...(canDelete
              ? [
                  {
                    key: 'delete',
                    label: localeCode === 'zh_CN' ? '删除集群' : 'Delete cluster',
                    danger: true,
                    disabled: platformWriteBlocked,
                  },
                ]
              : []),
          ]}
          onSelect={(key) => {
            if (key === 'view') navigate(`/clusters/${record.id}`)
            if (key === 'edit' && canUpdate && !platformWriteBlocked) {
              setEditingCluster(record)
              setModalVisible(true)
            }
            if (key === 'install' && canUpdate && !platformWriteBlocked)
              showAgentInstallation(record.id)
            if (key === 'upgrade' && canUpdate && !platformWriteBlocked) setUpgradingCluster(record)
            if (key === 'delete' && canDelete && !platformWriteBlocked) setDeletingCluster(record)
          }}
        />
      ),
    },
  ]

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
    if (!canDelete || platformWriteBlocked || selectedRowKeys.length === 0) return
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
    if (!canReadNodes || selectedRowKeys.length !== 1) return
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
        label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
        placeholder={
          localeCode === 'zh_CN' ? '搜索名称 / 环境 / 版本' : 'Search name / environment / version'
        }
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
        {canReadNodes ? (
          <Button
            autoInsertSpace={false}
            size="small"
            onClick={openSelectedNodes}
            disabled={selectedRowKeys.length !== 1}
          >
            {localeCode === 'zh_CN' ? '查看节点' : 'Open Nodes'}
          </Button>
        ) : null}
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
      <WorkloadRefresh
        settings={refresh}
        queries={[clustersQuery]}
        resourceLabel={localeCode === 'zh_CN' ? '集群' : 'Clusters'}
        compact
        label={localeCode === 'zh_CN' ? '刷新集群' : 'Refresh clusters'}
        onRefresh={() => clustersQuery.refetch()}
      />
    </ManagementTableToolbar>
  )

  return (
    <div className="soha-page soha-clusters-page">
      {agentInstallationMutation.feedback}
      {!deletingCluster && deleteMutation.feedback}
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
        scroll={{ x: 1200 }}
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
        {agentInstallationMutation.feedback}
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
        title={
          localeCode === 'zh_CN'
            ? `确认删除集群 ${deletingCluster?.name ?? ''}？`
            : `Delete cluster ${deletingCluster?.name ?? ''}?`
        }
        open={Boolean(deletingCluster)}
        destroyOnHidden
        okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
        cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
        okButtonProps={{
          danger: true,
          disabled: platformWriteBlocked || !canDelete,
          loading: deleteMutation.isPending,
        }}
        onCancel={() => {
          if (!deleteMutation.isPending && !platformWriteBlocked) setDeletingCluster(null)
        }}
        onOk={async () => {
          if (!deletingCluster || !canDelete || platformWriteBlocked) return
          try {
            await deleteMutation.mutateAsync(targetFor(deletingCluster.id))
            setDeletingCluster(null)
          } catch {
            /* Scoped feedback and the uncertain-write fence remain visible. */
          }
        }}
      >
        <p>
          {localeCode === 'zh_CN'
            ? '仅移除 Soha 中的集群注册，不会删除 Kubernetes 集群。'
            : 'Only removes the Soha registration; the Kubernetes cluster is retained.'}
        </p>
        {deleteMutation.feedback}
      </Modal>
      {modalVisible && (
        <ClusterEditor
          key={editingCluster?.id || 'create'}
          cluster={editingCluster}
          writeBlocked={platformWriteBlocked}
          onClose={(created) => {
            setModalVisible(false)
            setEditingCluster(null)
            if (created?.connectionMode === 'agent') showAgentInstallation(created.id)
          }}
        />
      )}
    </div>
  )
}
