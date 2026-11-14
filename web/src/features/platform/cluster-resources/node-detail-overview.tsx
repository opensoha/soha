import { Alert, Button, Card, Typography, Tooltip } from 'antd'
import { useI18n } from '@/i18n'
import { formatCpu, formatBytesAsG } from '../node-resource-utils'
import type { Pod } from '@/types'
import { formatAgeSeconds } from '@/utils/time'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../workloads/shared/workload-overview'
import { WorkloadPodsTable } from '../workloads/shared/workload-pods-table'
import { nodeReady, conditionHealthy, nodeUsage } from './node-list-model'
import { NodeTaints, NodeUsageCell } from './node-cells'
import type { ClusterNodeDetail, ClusterScope, NodeCondition } from './types'
import type { MonitoringView } from '../resource-monitoring/types'
import { StatusTag } from '@/components/status-tag'
import { AdminTable } from '@/components/admin-table'
import { formatDateTime } from '@/utils/time'

export function NodeConditions({
  conditions,
  compact = false,
  onShowAll,
}: {
  conditions?: NodeCondition[]
  compact?: boolean
  onShowAll?: () => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const status = (c: NodeCondition) => (
    <StatusTag
      value={
        conditionHealthy(c) === undefined ? 'unknown' : conditionHealthy(c) ? 'healthy' : 'warning'
      }
      label={c.status}
    />
  )
  return (
    <Card
      className="soha-detail-card"
      title={zh ? '运行条件' : 'Conditions'}
      extra={
        compact && (
          <Button type="link" onClick={onShowAll}>
            {zh ? '查看详情' : 'View details'}
          </Button>
        )
      }
    >
      {!conditions?.length ? (
        <Typography.Text type="secondary">
          {zh ? '未提供条件信息' : 'Conditions not reported'}
        </Typography.Text>
      ) : compact ? (
        <div className="soha-node-conditions">
          {conditions.map((c) => (
            <span key={c.type}>
              {c.type} {status(c)}
            </span>
          ))}
        </div>
      ) : (
        <AdminTable
          dataSource={conditions}
          pagination={false}
          rowKey="type"
          tableSize="small"
          scroll={{ x: 950 }}
          columns={[
            { title: zh ? '条件' : 'Condition', dataIndex: 'type', width: 180 },
            {
              title: zh ? '状态' : 'Status',
              key: 'status',
              width: 90,
              render: (_: unknown, c: NodeCondition) => status(c),
            },
            {
              title: zh ? '原因' : 'Reason',
              dataIndex: 'reason',
              width: 220,
              render: (v: string) => v || '—',
            },
            {
              title: zh ? '消息' : 'Message',
              dataIndex: 'message',
              render: (v: string) => v || '—',
            },
            {
              title: zh ? '最近变更' : 'Last transition',
              dataIndex: 'lastTransitionTime',
              width: 160,
              render: (v: string) => (v ? formatDateTime(v) : '—'),
            },
          ]}
        />
      )}
    </Card>
  )
}
export function NodeDetailOverview({
  node,
  scope,
  pods,
  canReadPods,
  monitoring,
  canReadMetrics,
  metricsFailed,
  onTabChange,
}: {
  node: ClusterNodeDetail
  scope: ClusterScope
  pods: Pod[]
  canReadPods: boolean
  monitoring?: MonitoringView
  canReadMetrics: boolean
  metricsFailed: boolean
  onTabChange: (key: string) => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const ready = nodeReady(node.status)
  return (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '节点信息' : 'Node information'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '调度' : 'Scheduling',
                  children: node.unschedulable
                    ? zh
                      ? '禁止调度'
                      : 'Cordoned'
                    : zh
                      ? '允许调度'
                      : 'Schedulable',
                },
                {
                  label: 'IP',
                  children: node.internalIp ? (
                    <Typography.Text copyable>{node.internalIp}</Typography.Text>
                  ) : (
                    '—'
                  ),
                },
                { label: zh ? '角色' : 'Roles', children: (node.roles ?? []).join(', ') || '—' },
                {
                  label: zh ? 'Kubernetes 版本' : 'Kubernetes version',
                  children: node.version || '—',
                },
                { label: zh ? '创建时长' : 'Age', children: formatAgeSeconds(node.ageSeconds) },
                { label: zh ? '关联 Pods' : 'Related Pods', children: node.podCount },
                {
                  label: zh ? '命名空间数' : 'Namespaces',
                  children: node.pods ? new Set(node.pods.map((p) => p.namespace)).size : '—',
                },
              ]}
            />
          </Card>
          <Card className="soha-detail-card" title={zh ? '污点' : 'Taints'}>
            <NodeTaints taints={node.taints} />
          </Card>
        </>
      }
    >
      <Alert
        showIcon
        type={ready ? 'success' : 'warning'}
        title={
          ready ? (zh ? '节点已就绪' : 'Node is ready') : zh ? '节点尚未就绪' : 'Node is not ready'
        }
        description={
          node.unschedulable
            ? zh
              ? '节点已禁止调度，现有 Pod 不受影响。'
              : 'The node is cordoned; existing Pods are unaffected.'
            : undefined
        }
        action={
          <Button type="link" onClick={() => onTabChange('events')}>
            {zh ? '查看事件' : 'View events'}
          </Button>
        }
      />
      <Card
        className="soha-detail-card"
        title={zh ? '主机资源使用' : 'Host resource usage'}
        extra={
          <Button type="link" onClick={() => onTabChange('metrics')}>
            {zh ? '查看指标' : 'View metrics'}
          </Button>
        }
      >
        <div className="soha-node-resource-grid">
          {(['cpu', 'memory'] as const).map((kind) => (
            <div key={kind}>
              <div>{kind === 'cpu' ? 'CPU' : zh ? '内存' : 'Memory'}</div>
              <NodeUsageCell
                kind={kind}
                usage={nodeUsage(monitoring, scope.clusterId, node.name, kind)}
                canRead={canReadMetrics}
                failed={metricsFailed}
              />
            </div>
          ))}
        </div>
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? '资源分配' : 'Resource allocation'}
        extra={
          <Typography.Text type="secondary">
            {zh ? 'Pod 请求 / 可分配' : 'Pod requests / allocatable'}
          </Typography.Text>
        }
      >
        <WorkloadOverviewConfiguration
          items={[
            {
              label: 'CPU',
              children: (
                <Tooltip
                  title={`${node.resources?.requests?.cpu ?? '—'} / ${node.resources?.allocatable?.cpu ?? '—'}`}
                >
                  <span>
                    {formatCpu(node.resources?.requests?.cpu)} /{' '}
                    {formatCpu(node.resources?.allocatable?.cpu)}
                  </span>
                </Tooltip>
              ),
            },
            {
              label: zh ? '内存' : 'Memory',
              children: (
                <Tooltip
                  title={`${node.resources?.requests?.memory ?? '—'} / ${node.resources?.allocatable?.memory ?? '—'}`}
                >
                  <span>
                    {formatBytesAsG(node.resources?.requests?.memory).replace(/G$/, ' GiB')} /{' '}
                    {formatBytesAsG(node.resources?.allocatable?.memory).replace(/G$/, ' GiB')}
                  </span>
                </Tooltip>
              ),
            },
            {
              label: zh ? '临时存储' : 'Ephemeral storage',
              children: (
                <Tooltip
                  title={`${node.resources?.requests?.ephemeralStorage ?? '—'} / ${node.resources?.allocatable?.ephemeralStorage ?? '—'}`}
                >
                  <span>
                    {formatBytesAsG(node.resources?.requests?.ephemeralStorage).replace(
                      /G$/,
                      ' GiB',
                    )}{' '}
                    /{' '}
                    {formatBytesAsG(node.resources?.allocatable?.ephemeralStorage).replace(
                      /G$/,
                      ' GiB',
                    )}
                  </span>
                </Tooltip>
              ),
            },
            {
              label: 'Pods',
              children: `${node.podCount} / ${node.resources?.allocatable?.pods ?? '—'}`,
            },
          ]}
        />
      </Card>
      {canReadPods && (
        <WorkloadPodsTable
          pods={pods}
          scope={scope}
          compact
          onShowAll={() => onTabChange('pods')}
        />
      )}
      <NodeConditions
        conditions={node.conditions}
        compact
        onShowAll={() => onTabChange('conditions')}
      />
      <WorkloadOverviewMetadata labels={node.labels} annotations={node.annotations} copyable />
    </WorkloadOverview>
  )
}
