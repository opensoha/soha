import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementDensityButton,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, type MetadataTagTone } from '@/components/status-tag'
import { NodeStateTags } from '@/features/platform/shared/node-state-tags'
import { hasAllowedAction } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import {
  NodeResourcePanel,
  parseStringMap,
  parseTaints,
  stringifyMap,
  stringifyTaints,
} from '@/features/platform/node-resource-utils'
import { useKubernetesResourceStream } from '@/features/platform/shared/resource-stream'
import { ResourceStreamStatus } from '@/features/platform/shared/resource-stream-status'
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
  ClearOutlined,
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  PlayCircleOutlined,
  StopOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Alert,
  Button,
  Checkbox,
  Form,
  Grid,
  Input,
  InputNumber,
  Modal,
  Popover,
  Space,
  Spin,
  Typography,
  Tooltip,
} from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import './nodes-list-page.css'
import { nodeMutations } from './mutations'
import { nodeQueries } from './queries'
import { toClusterScope } from './scope'
import type { ClusterNode } from './types'

const taintTones: Record<string, MetadataTagTone> = {
  NoSchedule: 'purple',
  PreferNoSchedule: 'blue',
  NoExecute: 'orange',
}

export function ClusterNodesPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const pinColumns = Boolean(Grid.useBreakpoint().md)
  const queryClient = useQueryClient()
  const { clusterId } = usePlatformScopeStore()
  const scope = toClusterScope(clusterId)
  const [editingNodeName, setEditingNodeName] = useState<string | null>(null)
  const [drainingNodeName, setDrainingNodeName] = useState<string | null>(null)
  const [drainForm] = Form.useForm()
  const [nodeTableSize, setNodeTableSize] = useState<'middle' | 'small'>('small')
  const nodeListQuery = nodeQueries.list(scope)
  const nodeStream = useKubernetesResourceStream({
    clusterId,
    kinds: ['Node', 'Pod', 'Event'],
    onEvent: () => void queryClient.invalidateQueries({ queryKey: nodeListQuery.queryKey }),
    onFallback: () => void queryClient.invalidateQueries({ queryKey: nodeListQuery.queryKey }),
    onResyncRequired: () => queryClient.invalidateQueries({ queryKey: nodeListQuery.queryKey }),
  })
  const nodesQuery = useQuery(nodeListQuery)
  const visibleNodesQueryData = readableQueryData(nodesQuery)
  const nodes = visibleNodesQueryData ?? []
  const nodeDetailQuery = useQuery(nodeQueries.detail(scope, editingNodeName ?? ''))
  const visibleNodeDetailQueryData = readableQueryData(nodeDetailQuery)
  const updateNodeMutation = usePlatformMutation(nodeMutations.update(queryClient), {
    label: ['更新节点', 'Update node'],
    target: platformResourceTarget,
  })
  const deleteNodeMutation = usePlatformMutation(nodeMutations.remove(queryClient), {
    label: ['删除节点', 'Delete node'],
    target: platformResourceTarget,
  })
  const schedulabilityMutation = usePlatformMutation(nodeMutations.schedulability(queryClient), {
    label: ['调整节点调度', 'Change schedulability'],
    target: platformResourceTarget,
  })
  const drainNodeMutation = usePlatformMutation(nodeMutations.drain(queryClient), {
    label: ['排空节点', 'Drain node'],
    target: platformResourceTarget,
  })
  const failedDrainName =
    drainNodeMutation.isError && drainNodeMutation.variables?.scope.clusterId === scope.clusterId
      ? drainNodeMutation.variables.name
      : ''
  const drainStateQuery = useQuery(nodeQueries.detail(scope, failedDrainName))
  const drainState = readableQueryData(drainStateQuery)
  const platformWriteBlocked =
    updateNodeMutation.blocked ||
    deleteNodeMutation.blocked ||
    schedulabilityMutation.blocked ||
    drainNodeMutation.blocked

  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: '集群节点',
    entityKind: 'kubernetes.node-list',
    entityName: clusterId || 'nodes',
    clusterId: clusterId ?? undefined,
    node: editingNodeName ?? undefined,
    pinnedData: {
      nodeCount: visibleNodesQueryData?.length ?? 0,
      editingNodeName,
    },
  })

  const nodeDetail = visibleNodeDetailQueryData
  const nodeModalInitValues = useMemo(() => {
    if (!nodeDetail) return undefined
    return {
      labels: stringifyMap(nodeDetail.labels),
      taints: stringifyTaints(nodeDetail.taints),
    }
  }, [nodeDetail])

  const detailPath = (name: string) =>
    `/cluster-resources/nodes/${encodeURIComponent(name)}?clusterId=${encodeURIComponent(clusterId || '')}`
  const copy =
    localeCode === 'zh_CN'
      ? {
          unschedulable: '禁止调度',
          restoreScheduling: '恢复调度',
          drain: '排空节点',
          edit: '编辑节点',
          deleted: '节点对象已删除',
          updated: '节点配置已更新',
          drained: '节点已禁止调度，可迁移 Pod 已完成驱逐',
          drainWarning:
            '节点会先进入禁止调度状态，再驱逐可迁移的 Pod。DaemonSet 和静态 Pod 不会被驱逐。失败或超时后请核对最新调度状态和剩余 Pod，再决定是否恢复调度。',
          deleteDescription: '这会删除 Kubernetes 中的 Node 对象，不会自动回收底层机器。',
        }
      : {
          unschedulable: 'Disable Scheduling',
          restoreScheduling: 'Enable Scheduling',
          drain: 'Drain Node',
          edit: 'Edit Node',
          deleted: 'Node object deleted',
          updated: 'Node configuration updated',
          drained: 'Node cordoned and evictable pods drained',
          drainWarning:
            'The node will be cordoned before evictable pods are drained. DaemonSet and static pods are not evicted. After a failure or timeout, inspect the current schedulability and remaining pods before uncordoning.',
          deleteDescription:
            'This deletes the Kubernetes Node object without reclaiming the underlying machine.',
        }

  useEffect(() => {
    setEditingNodeName(null)
    setDrainingNodeName(null)
  }, [clusterId])
  const resourceActions = useKubernetesResourceActions<ClusterNode>({
    resourceKey: 'nodes',
    resourceLabel: localeCode === 'zh_CN' ? '节点' : 'Node',
    records: nodes,
    scopeKey: clusterId ?? '',
    getName: (record) => record.name,
    getKey: (record) => record.name,
    actions: (record) => {
      const name = record.name
      const schedulingLabel = record.unschedulable ? copy.restoreScheduling : copy.unschedulable
      return [
        {
          key: 'details',
          label: t('common.details', '详情'),
          icon: <EyeOutlined />,
          run: () => navigate(detailPath(name)),
        },
        ...(hasAllowedAction(record.allowedActions, 'update')
          ? [
              {
                key: 'edit',
                label: copy.edit,
                icon: <EditOutlined />,
                disabled: platformWriteBlocked,
                run: () => setEditingNodeName(name),
              },
              {
                key: record.unschedulable ? 'uncordon' : 'cordon',
                label: schedulingLabel,
                icon: record.unschedulable ? <PlayCircleOutlined /> : <StopOutlined />,
                disabled: platformWriteBlocked,
                loading:
                  schedulabilityMutation.isPending &&
                  schedulabilityMutation.variables?.scope.clusterId === clusterId &&
                  schedulabilityMutation.variables?.name === name,
                confirm: {
                  title:
                    localeCode === 'zh_CN'
                      ? `确认${schedulingLabel}节点 ${name}？`
                      : `Confirm ${schedulingLabel.toLowerCase()} for node ${name}?`,
                  description: record.unschedulable
                    ? localeCode === 'zh_CN'
                      ? '恢复后，新的 Pod 可以再次调度到该节点。'
                      : 'New pods can be scheduled to this node again.'
                    : localeCode === 'zh_CN'
                      ? '现有 Pod 不受影响，但新的 Pod 将不再调度到该节点。'
                      : 'Existing pods are unaffected, but new pods will not be scheduled here.',
                },
                prepare: () => schedulabilityMutation.reset(),
                feedback: schedulabilityMutation.feedback,
                run: () =>
                  schedulabilityMutation.mutateAsync({
                    scope,
                    name,
                    unschedulable: !record.unschedulable,
                  }),
              },
            ]
          : []),
        ...(hasAllowedAction(record.allowedActions, 'drain')
          ? [
              {
                key: 'drain',
                label: copy.drain,
                icon: <ClearOutlined />,
                danger: true,
                disabled: platformWriteBlocked,
                loading:
                  drainNodeMutation.isPending &&
                  drainNodeMutation.variables?.scope.clusterId === clusterId &&
                  drainNodeMutation.variables?.name === name,
                run: () => {
                  drainNodeMutation.reset()
                  drainForm.resetFields()
                  setDrainingNodeName(name)
                },
              },
            ]
          : []),
        ...(hasAllowedAction(record.allowedActions, 'delete')
          ? [
              {
                key: 'delete',
                label: t('common.delete', '删除'),
                icon: <DeleteOutlined />,
                danger: true,
                disabled: platformWriteBlocked,
                loading:
                  deleteNodeMutation.isPending &&
                  deleteNodeMutation.variables?.scope.clusterId === clusterId &&
                  deleteNodeMutation.variables?.name === name,
                confirm: kubernetesDeleteConfirmation(record, localeCode, copy.deleteDescription),
                prepare: () => deleteNodeMutation.reset(),
                feedback: deleteNodeMutation.feedback,
                run: () => deleteNodeMutation.mutateAsync({ scope, name }),
              },
            ]
          : []),
      ]
    },
  })

  const nodeColumns: TableColumnsType<ClusterNode> = [
    {
      title: t('common.name', '名称'),
      dataIndex: 'name',
      fixed: pinColumns ? 'left' : undefined,
      ellipsis: { showTitle: false },
      render: (name: string) => (
        <Tooltip title={name} trigger={['hover', 'focus']}>
          <Link className="soha-node-name" to={detailPath(name)}>
            {name}
          </Link>
        </Tooltip>
      ),
    },
    {
      ...tableColumnPresets.status,
      width: 80,
      title: t('common.status', '状态'),
      dataIndex: 'status',
      render: (status: string, record) => (
        <NodeStateTags
          status={status}
          cordoned={record.unschedulable}
          conditions={record.conditions}
          draining={
            drainNodeMutation.isPending &&
            drainNodeMutation.variables?.scope.clusterId === clusterId &&
            drainNodeMutation.variables?.name === record.name
          }
        />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '污点' : 'Taints',
      key: 'taints',
      width: 144,
      render: (_, node) =>
        node.taints == null ? (
          <Typography.Text type="secondary">
            {localeCode === 'zh_CN' ? '未提供' : 'Not reported'}
          </Typography.Text>
        ) : node.taints.length === 0 ? (
          <Typography.Text type="secondary">
            {localeCode === 'zh_CN' ? '无污点' : 'None'}
          </Typography.Text>
        ) : (
          <div className="soha-node-taints">
            {node.taints.map((taint) => {
              const label = `${taint.key}${taint.value ? `=${taint.value}` : ''}:${taint.effect}`
              const shortLabel = `${taint.key.split('/').pop()}${taint.value ? `=${taint.value}` : ''}`
              return (
                <Popover
                  key={`${taint.key}:${taint.effect}`}
                  trigger={['hover', 'click']}
                  title={localeCode === 'zh_CN' ? '节点污点' : 'Node taint'}
                  content={
                    <Typography.Text className="soha-node-taint-full" code>
                      {label}
                    </Typography.Text>
                  }
                >
                  <button
                    type="button"
                    className="soha-node-taint-trigger"
                    aria-label={`${localeCode === 'zh_CN' ? '查看污点' : 'View taint'} ${label}`}
                  >
                    <MetadataTag
                      tone={taintTones[taint.effect] || 'default'}
                      label={<span className="soha-node-taint-label">{shortLabel}</span>}
                    />
                  </button>
                </Popover>
              )
            })}
          </div>
        ),
    },
    {
      title: t('common.role', '角色'),
      dataIndex: 'roles',
      width: 96,
      className: 'soha-table-cell-wrap',
      render: (roles: string[]) =>
        roles?.length ? (
          <Space size={[4, 4]} wrap>
            {roles.map((role) => (
              <MetadataTag key={role} label={role} />
            ))}
          </Space>
        ) : (
          '-'
        ),
    },
    { title: 'IP', width: 100, dataIndex: 'internalIp', render: (value: string) => value || '-' },
    {
      title: t('common.version', '版本'),
      dataIndex: 'version',
      width: 112,
      render: (value: string) => value || '-',
    },
    { title: t('common.pods', 'Pods'), width: 48, dataIndex: 'podCount' },
    {
      ...tableColumnPresets.datetime,
      width: 68,
      title: t('common.age', '时长'),
      dataIndex: 'ageSeconds',
      render: (value: number) => formatAgeSeconds(value),
    },
  ]

  return (
    <div className="soha-page soha-cluster-nodes-page">
      <ReadFeedback canRead={nodeDetailQuery.isEnabled} query={nodeDetailQuery} label="节点详情" />

      {updateNodeMutation.feedback}
      {resourceActions.confirmation}
      {!resourceActions.isConfirming && deleteNodeMutation.feedback}
      {!resourceActions.isConfirming && schedulabilityMutation.feedback}
      {!drainingNodeName && drainNodeMutation.feedback}

      {!clusterId ? (
        <ManagementState
          compact
          kind="select-scope"
          title={t('common.pleaseSelectCluster', 'Please select a cluster')}
        />
      ) : (
        <KubernetesResourceTable
          resourceKey={'nodes'}
          resourceLabel={t('common.nodes', 'Nodes')}
          allRows={nodes}
          filterKey={''}
          scopeMode={'cluster'}
          error={nodesQuery.isEnabled === false ? null : nodesQuery.error}
          hasReadData={nodesQuery.data !== undefined}
          onRetry={nodesQuery.isEnabled === false ? undefined : () => nodesQuery.refetch()}
          columnSettingIconOnly
          columnSettingPlacement="header"
          shellClassName="soha-management-table-shell"
          headerExtra={
            <ManagementTableToolbar>
              <ResourceStreamStatus
                status={nodeStream.status}
                lastEventAt={nodeStream.lastEventAt}
                localeCode={localeCode}
              />
              <ManagementDensityButton
                aria-label={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
                title={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
                tooltip={localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'}
                onClick={() =>
                  setNodeTableSize((current) => (current === 'middle' ? 'small' : 'middle'))
                }
              />
              <ManagementRefreshButton
                aria-label={t('common.refresh', '刷新')}
                loading={nodesQuery.isFetching}
                title={t('common.refresh', '刷新')}
                tooltip={t('common.refresh', '刷新')}
                onClick={() => void nodesQuery.refetch()}
              />
            </ManagementTableToolbar>
          }
          columns={[...nodeColumns, resourceActions.column]}
          dataSource={nodes}
          rowKey="name"
          loading={nodesQuery.isLoading}
          localSorting
          tableSize={nodeTableSize}
          tableLayout="fixed"
          scroll={{ x: 960 }}
          expandable={{ columnWidth: 48, fixed: pinColumns ? 'left' : undefined }}
          expandedRowRender={(record: ClusterNode) => <NodeResourcePanel node={record} />}
          hideExpandedColumn={false}
        />
      )}

      <Modal
        okButtonProps={{ disabled: platformWriteBlocked }}
        title={editingNodeName ? `${copy.edit} ${editingNodeName}` : copy.edit}
        open={!!editingNodeName}
        footer={null}
        width={720}
        onCancel={() => {
          if (!platformWriteBlocked) (() => setEditingNodeName(null))?.()
        }}
      >
        {updateNodeMutation.feedback}
        {deleteNodeMutation.feedback}
        {schedulabilityMutation.feedback}

        {nodeDetailQuery.isLoading && !nodeDetail ? (
          <div className="flex items-center justify-center h-48">
            <Spin size="large" />
          </div>
        ) : (
          <Form
            key={editingNodeName ?? 'node'}
            layout="vertical"
            initialValues={nodeModalInitValues}
            onFinish={(values) => {
              if (
                !editingNodeName ||
                platformWriteBlocked ||
                !nodes.some(
                  (node) =>
                    node.name === editingNodeName &&
                    hasAllowedAction(node.allowedActions, 'update'),
                )
              )
                return
              updateNodeMutation.mutate(
                {
                  scope,
                  name: editingNodeName,
                  input: {
                    labels: parseStringMap(values.labels, 'Labels'),
                    taints: parseTaints(values.taints),
                  },
                },
                {
                  onSuccess: () => {
                    setEditingNodeName(null)
                  },
                },
              )
            }}
          >
            <Form.Item name="labels" label="Labels(JSON)">
              <Input.TextArea rows={8} />
            </Form.Item>
            <Form.Item name="taints" label="Taints(JSON Array)">
              <Input.TextArea rows={8} />
            </Form.Item>
            <div className="soha-form-actions">
              <Button onClick={() => setEditingNodeName(null)}>{t('common.cancel', '取消')}</Button>
              <Button htmlType="submit" type="primary" loading={updateNodeMutation.isPending}>
                {t('common.save', '保存')}
              </Button>
            </div>
          </Form>
        )}
      </Modal>

      <Modal
        title={drainingNodeName ? `${copy.drain} ${drainingNodeName}` : copy.drain}
        open={!!drainingNodeName}
        okText={localeCode === 'zh_CN' ? '确认排空' : 'Confirm Drain'}
        cancelText={t('common.cancel', '取消')}
        okButtonProps={{ disabled: platformWriteBlocked, danger: true }}
        confirmLoading={drainNodeMutation.isPending}
        mask={{ closable: !drainNodeMutation.isPending }}
        onCancel={() => {
          if (!platformWriteBlocked)
            (() => !drainNodeMutation.isPending && setDrainingNodeName(null))?.()
        }}
        onOk={() => drainForm.submit()}
      >
        {drainNodeMutation.feedback}

        {failedDrainName === drainingNodeName && (
          <>
            <ReadFeedback
              query={drainStateQuery}
              label={localeCode === 'zh_CN' ? '排水后的节点状态' : 'Node state after drain'}
            />
            {drainStateQuery.isFetching && <Spin size="small" />}
            {drainState && (
              <Alert
                type="info"
                showIcon
                title={
                  localeCode === 'zh_CN' ? '失败后的节点观测' : 'Node observation after failure'
                }
                description={
                  <Space orientation="vertical">
                    <NodeStateTags
                      status={drainState.status}
                      cordoned={drainState.unschedulable}
                      conditions={drainState.conditions}
                    />
                    <Typography.Text>
                      {localeCode === 'zh_CN'
                        ? `当前绑定 Pod：${drainState.podCount}（含未参与驱逐的 DaemonSet / 静态 Pod）`
                        : `Currently bound pods: ${drainState.podCount} (includes DaemonSet / static pods excluded from eviction)`}
                    </Typography.Text>
                    <Link to={detailPath(failedDrainName)}>
                      {localeCode === 'zh_CN'
                        ? '查看节点与剩余 Pod'
                        : 'Inspect node and remaining pods'}
                    </Link>
                  </Space>
                }
              />
            )}
          </>
        )}

        <Alert type="warning" showIcon title={copy.drainWarning} />
        <Form
          form={drainForm}
          layout="vertical"
          initialValues={{ deleteEmptyDirData: false, force: false, timeoutSeconds: 300 }}
          onFinish={(values) => {
            if (
              !drainingNodeName ||
              platformWriteBlocked ||
              !nodes.some(
                (node) =>
                  node.name === drainingNodeName && hasAllowedAction(node.allowedActions, 'drain'),
              )
            )
              return
            drainNodeMutation.mutate(
              {
                scope,
                name: drainingNodeName,
                force: Boolean(values.force),
                deleteEmptyDirData: Boolean(values.deleteEmptyDirData),
                timeoutSeconds: values.timeoutSeconds,
              },
              {
                onSuccess: () => {
                  setDrainingNodeName(null)
                },
              },
            )
          }}
        >
          <Form.Item
            name="timeoutSeconds"
            label={localeCode === 'zh_CN' ? '排水超时（秒）' : 'Drain timeout (seconds)'}
            rules={[{ required: true, type: 'number', min: 30, max: 1800 }]}
          >
            <InputNumber min={30} max={1800} step={30} />
          </Form.Item>
          <Form.Item name="deleteEmptyDirData" valuePropName="checked">
            <Checkbox>
              {localeCode === 'zh_CN'
                ? '允许删除使用 emptyDir 的 Pod（本地临时数据会丢失）'
                : 'Allow deletion of pods using emptyDir (local temporary data will be lost)'}
            </Checkbox>
          </Form.Item>
          <Form.Item name="force" valuePropName="checked">
            <Checkbox>
              {localeCode === 'zh_CN'
                ? '强制驱逐没有控制器管理的 Pod'
                : 'Force eviction of pods without a controller'}
            </Checkbox>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
