import { AdminTable } from '@/components/admin-table'
import { StatusTag } from '@/components/status-tag'
import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
import { formatContainerStateLabel } from '@/features/platform/workloads-model'
import type { PodDetail, WorkloadContainer } from '@/types'
import { formatDateTime } from '@/utils/time'
import { WorkloadOverview, WorkloadOverviewMetadata } from '../shared/workload-overview'
import { CheckCircleFilled, WarningOutlined } from '@ant-design/icons'
import { Card, Collapse, Descriptions, Tooltip, Typography } from 'antd'
import type { ReactNode } from 'react'
import { podContainerTerminated } from './container-status'

const { Text } = Typography
export function podContainerTone(item: WorkloadContainer) {
  const [state, stateReason] = (item.state || '').split(':', 2)
  const reason = item.reason || stateReason || ''
  if (state === 'running') return item.ready ? 'success' : 'warning'
  if (state === 'terminated' && reason === 'Completed') return 'success'
  if (/backoff|error|errimagepull|invalidimagename|oomkilled/i.test(reason)) return 'danger'
  return state === 'terminated' ? 'danger' : 'warning'
}
export function PodContainerSquare({
  item,
  onClick,
}: {
  item: WorkloadContainer
  onClick: () => void
}) {
  const label = `${item.name}: ${item.reason || item.state || 'Unknown'}`
  return (
    <Tooltip title={label}>
      <button
        type="button"
        className={`soha-pod-detail-square is-${podContainerTone(item)}`}
        aria-label={label}
        onClick={onClick}
      />
    </Tooltip>
  )
}
export function PodDetailOverview({
  pod,
  zh,
  expanded,
  onExpand,
  onContainerAction,
  canLogs,
  canExec,
  logsReason,
  execReason,
  nodeLink,
  namespaceLink,
  accountLink,
  ownerLink,
  onEvents,
  metrics,
  containerDetails,
  containerFeedback,
  containerExtra,
}: {
  pod: PodDetail
  zh: boolean
  expanded: string[]
  onExpand: (keys: string[]) => void
  onContainerAction: (key: string, name: string) => void
  canLogs: boolean
  canExec: boolean
  logsReason?: string
  execReason?: string
  nodeLink: ReactNode
  namespaceLink: ReactNode
  accountLink: ReactNode
  ownerLink: ReactNode
  onEvents: () => void
  metrics?: ReactNode
  containerDetails?: (container: WorkloadContainer) => ReactNode
  containerFeedback?: ReactNode
  containerExtra?: ReactNode
}) {
  const containers = [...(pod.containers ?? [])].sort(
    (a, b) => Number(b.role === 'init') - Number(a.role === 'init'),
  )
  const failed = containers.filter((c) => podContainerTone(c) === 'danger')
  const regular = containers.filter((c) => c.role !== 'init')
  const healthy =
    pod.phase === 'Running' &&
    regular.length > 0 &&
    regular.every((c) => c.ready && c.state === 'running') &&
    containers.every((c) => podContainerTone(c) === 'success') &&
    !(pod.conditions ?? []).some((c) => c.status !== 'True')
  const completed = pod.phase === 'Succeeded'
  const summary =
    failed[0]?.reason || (pod.conditions ?? []).find((c) => c.status === 'False')?.message
  return (
    <WorkloadOverview
      className="soha-pod-detail-overview-grid"
      aside={
        <>
          <Card
            className="soha-detail-card soha-pod-detail-card"
            title={zh ? '运行信息' : 'Runtime'}
          >
            <Descriptions
              column={1}
              size="small"
              items={[
                { key: 'namespace', label: zh ? '命名空间' : 'Namespace', children: namespaceLink },
                { key: 'node', label: zh ? '节点' : 'Node', children: nodeLink },
                {
                  key: 'podip',
                  label: 'Pod IP',
                  children: <Text copyable={Boolean(pod.podIp)}>{pod.podIp || '—'}</Text>,
                },
                { key: 'hostip', label: 'Host IP', children: pod.hostIp || '—' },
                { key: 'qos', label: 'QoS', children: pod.qosClass || '—' },
                {
                  key: 'account',
                  label: zh ? '服务账号' : 'Service account',
                  children: accountLink,
                },
                {
                  key: 'created',
                  label: zh ? '创建时间' : 'Created',
                  children: formatDateTime(pod.createdAt),
                },
                {
                  key: 'started',
                  label: zh ? '启动时间' : 'Started',
                  children: formatDateTime(pod.startTime),
                },
              ]}
            />
          </Card>
          {ownerLink && (
            <Card
              className="soha-detail-card soha-pod-detail-card"
              title={zh ? '所属工作负载' : 'Owner workload'}
            >
              {ownerLink}
            </Card>
          )}
        </>
      }
    >
      <div
        className={`soha-pod-detail-health ${healthy || completed ? 'is-healthy' : 'is-attention'}`}
      >
        {healthy || completed ? <CheckCircleFilled /> : <WarningOutlined />}
        <div>
          <strong>
            {completed
              ? zh
                ? 'Pod 已完成'
                : 'Pod completed'
              : healthy
                ? zh
                  ? 'Pod 运行正常'
                  : 'Pod is healthy'
                : zh
                  ? '检查 Pod 运行状态'
                  : 'Check pod status'}
          </strong>
          {summary && <p>{summary}</p>}
        </div>
        <button type="button" className="soha-pod-detail-text-link" onClick={onEvents}>
          {zh ? '查看事件' : 'View events'}
        </button>
      </div>
      {metrics}
      <Card
        className="soha-detail-card soha-pod-detail-card"
        title={zh ? '容器' : 'Containers'}
        extra={
          <span className="soha-pod-container-card-extra">
            <Text type="secondary">
              {regular.length} {zh ? '常规' : 'app'} · {containers.length - regular.length} init
            </Text>
            {containerExtra}
          </span>
        }
      >
        {containerFeedback}
        <Collapse
          className="soha-pod-container-collapse"
          ghost
          activeKey={expanded}
          onChange={(keys) => onExpand(Array.isArray(keys) ? keys : [keys])}
          items={containers.map((item) => ({
            key: item.name,
            label: (
              <span className="soha-pod-detail-container-label">
                <span className={`soha-pod-detail-static-square is-${podContainerTone(item)}`} />
                <strong>{item.name}</strong>
                <Text type="secondary">
                  {item.role === 'init'
                    ? zh
                      ? '初始化容器'
                      : 'Init container'
                    : zh
                      ? '常规容器'
                      : 'App container'}
                </Text>
                <StatusTag value={item.reason || item.state || 'Unknown'} />
                <Text type="secondary">
                  {zh ? '重启' : 'Restarts'} {item.restartCount ?? 0}
                </Text>
              </span>
            ),
            extra: (
              <KubernetesResourceActionsMenu
                name={item.name}
                resourceLabel={zh ? '容器' : 'Container'}
                localeCode={zh ? 'zh_CN' : 'en_US'}
                items={[
                  {
                    key: 'logs',
                    label: <Tooltip title={logsReason}>{zh ? '查看日志' : 'View logs'}</Tooltip>,
                    disabled: !canLogs,
                  },
                  {
                    key: 'terminal',
                    label: (
                      <Tooltip title={execReason}>{zh ? '打开终端' : 'Open terminal'}</Tooltip>
                    ),
                    disabled: !canExec || podContainerTerminated(item),
                  },
                ]}
                onSelect={(key) => onContainerAction(key, item.name)}
              />
            ),
            children: containerDetails ? (
              containerDetails(item)
            ) : (
              <Descriptions
                column={1}
                size="small"
                items={[
                  {
                    key: 'image',
                    label: zh ? '镜像' : 'Image',
                    children: (
                      <Text copyable className="soha-pod-detail-break">
                        {item.image || '—'}
                      </Text>
                    ),
                  },
                  {
                    key: 'ready',
                    label: zh ? '就绪' : 'Ready',
                    children: zh ? (item.ready ? '是' : '否') : item.ready ? 'Yes' : 'No',
                  },
                  {
                    key: 'start',
                    label: zh ? '启动时间' : 'Started',
                    children: formatDateTime(item.startedAt),
                  },
                  {
                    key: 'last',
                    label: zh ? '上次状态' : 'Previous state',
                    children: formatContainerStateLabel(item.lastState || '—'),
                  },
                  ...(item.message
                    ? [{ key: 'message', label: zh ? '消息' : 'Message', children: item.message }]
                    : []),
                  {
                    key: 'id',
                    label: 'Container ID',
                    children: (
                      <Text copyable={Boolean(item.containerId)} className="soha-pod-detail-break">
                        {item.containerId || '—'}
                      </Text>
                    ),
                  },
                ]}
              />
            ),
          }))}
        />
      </Card>
      <Card
        className="soha-detail-card soha-pod-detail-card"
        title={zh ? '运行条件' : 'Conditions'}
      >
        <Collapse
          ghost
          items={[
            {
              key: 'conditions',
              label: (
                <span className="soha-pod-detail-condition-summary">
                  {(pod.conditions ?? []).map((c) => (
                    <span
                      key={c.type}
                      className={c.status === 'True' ? 'is-success' : 'is-warning'}
                    >
                      {c.status === 'True' ? <CheckCircleFilled /> : <WarningOutlined />}
                      {c.type}
                    </span>
                  ))}
                  {!pod.conditions?.length && (
                    <Text type="secondary">{zh ? '暂无条件' : 'No conditions'}</Text>
                  )}
                </span>
              ),
              children: (
                <AdminTable
                  columns={[
                    { title: zh ? '条件' : 'Condition', dataIndex: 'type' },
                    {
                      title: zh ? '状态' : 'Status',
                      dataIndex: 'status',
                      render: (value: string) => <StatusTag value={value} />,
                    },
                    { title: zh ? '原因' : 'Reason', dataIndex: 'reason' },
                    { title: zh ? '消息' : 'Message', dataIndex: 'message' },
                    {
                      title: zh ? '最近变化' : 'Last transition',
                      dataIndex: 'lastTransitionTime',
                      render: formatDateTime,
                    },
                  ]}
                  dataSource={pod.conditions ?? []}
                  rowKey="type"
                  pagination={false}
                  enableColumnSelection={false}
                />
              ),
            },
          ]}
        />
      </Card>
      <WorkloadOverviewMetadata copyable labels={pod.labels} annotations={pod.annotations} />
    </WorkloadOverview>
  )
}
