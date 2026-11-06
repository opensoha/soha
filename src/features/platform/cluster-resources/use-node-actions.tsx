import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Checkbox,
  Form,
  Input,
  InputNumber,
  Modal,
  Space,
  Spin,
  Typography,
} from 'antd'
import {
  ClearOutlined,
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  PlayCircleOutlined,
  StopOutlined,
} from '@ant-design/icons'
import { hasAllowedAction } from '@/features/auth'
import { useI18n } from '@/i18n'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { NodeStateTags } from '../shared/node-state-tags'
import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
  type KubernetesResourceAction,
} from '../shared/use-kubernetes-resource-actions'
import { platformResourceTarget, usePlatformMutation } from '../shared/use-platform-mutation'
import { parseStringMap, parseTaints, stringifyMap, stringifyTaints } from '../node-resource-utils'
import { nodeMutations } from './mutations'
import { nodeQueries } from './queries'
import type { ClusterNode, ClusterScope } from './types'

export function useNodeActions({
  scope,
  nodes,
  onDeleted,
}: {
  scope: ClusterScope
  nodes: readonly ClusterNode[]
  onDeleted?: (name: string) => void
}) {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const clusterId = scope.clusterId
  const [editingTarget, setEditingTarget] = useState<{
    clusterId: string | null
    name: string
  } | null>(null)
  const [drainingTarget, setDrainingTarget] = useState<{
    clusterId: string | null
    name: string
  } | null>(null)
  const editingNodeName = editingTarget?.clusterId === clusterId ? editingTarget.name : null
  const drainingNodeName = drainingTarget?.clusterId === clusterId ? drainingTarget.name : null
  const setEditingNodeName = (name: string | null) =>
    setEditingTarget(name ? { clusterId, name } : null)
  const setDrainingNodeName = (name: string | null) =>
    setDrainingTarget(name ? { clusterId, name } : null)
  const currentCluster = useRef(clusterId)
  currentCluster.current = clusterId
  const [drainForm] = Form.useForm()
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
    setEditingTarget(null)
    setDrainingTarget(null)
  }, [clusterId])
  const entries = (record: ClusterNode): KubernetesResourceAction[] => {
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
              run: async () => {
                await deleteNodeMutation.mutateAsync({ scope, name })
                if (currentCluster.current === clusterId) onDeleted?.(name)
              },
            },
          ]
        : []),
    ]
  }
  const resourceActions = useKubernetesResourceActions<ClusterNode>({
    resourceKey: 'nodes',
    resourceLabel: localeCode === 'zh_CN' ? '节点' : 'Node',
    records: nodes,
    scopeKey: clusterId ?? '',
    getName: (record) => record.name,
    getKey: (record) => record.name,
    actions: entries,
  })
  const feedback = (
    <>
      <ReadFeedback
        canRead={nodeDetailQuery.isEnabled}
        query={nodeDetailQuery}
        label={localeCode === 'zh_CN' ? '节点详情' : 'Node detail'}
      />
      {updateNodeMutation.feedback}
      {resourceActions.confirmation}
      {!resourceActions.isConfirming && deleteNodeMutation.feedback}
      {!resourceActions.isConfirming && schedulabilityMutation.feedback}
      {!drainingNodeName && drainNodeMutation.feedback}
    </>
  )
  const buttons = (node: ClusterNode, destructive = false) =>
    entries(node)
      .filter((action) =>
        destructive ? action.key === 'delete' : !['details', 'delete'].includes(action.key),
      )
      .map((action) => (
        <Button
          key={action.key}
          autoInsertSpace={false}
          danger={action.danger}
          disabled={action.disabled}
          loading={action.loading}
          onClick={() => resourceActions.selectAction(node, action.key)}
        >
          {action.label}
        </Button>
      ))
  return {
    ...resourceActions,
    editingNodeName,
    drainingNodeName,
    isDraining: (name: string) =>
      drainNodeMutation.isPending &&
      drainNodeMutation.variables?.scope.clusterId === clusterId &&
      drainNodeMutation.variables?.name === name,
    feedback,
    buttons,
    dialogs: (
      <>
        <Modal
          destroyOnHidden
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
              key={`${clusterId}:${editingNodeName}`}
              layout="vertical"
              disabled={platformWriteBlocked || nodeDetailQuery.isError || !nodeDetail}
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
              <Form.Item
                name="labels"
                label="Labels(JSON)"
                rules={[
                  {
                    validator: async (_, value) => {
                      parseStringMap(value, 'Labels')
                    },
                  },
                ]}
              >
                <Input.TextArea rows={8} />
              </Form.Item>
              <Form.Item
                name="taints"
                label="Taints(JSON Array)"
                rules={[
                  {
                    validator: async (_, value) => {
                      parseTaints(value)
                    },
                  },
                ]}
              >
                <Input.TextArea rows={8} />
              </Form.Item>
              <div className="soha-form-actions">
                <Button onClick={() => setEditingNodeName(null)}>
                  {t('common.cancel', '取消')}
                </Button>
                <Button htmlType="submit" type="primary" loading={updateNodeMutation.isPending}>
                  {t('common.save', '保存')}
                </Button>
              </div>
            </Form>
          )}
        </Modal>

        <Modal
          destroyOnHidden
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
                    node.name === drainingNodeName &&
                    hasAllowedAction(node.allowedActions, 'drain'),
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
      </>
    ),
  }
}
