import { Alert, Button, Card, Typography } from 'antd'
import type { ReactNode } from 'react'
import type { ScopeKey } from '@/types'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewFacts,
  WorkloadOverviewMetadata,
} from './workload-overview'
import { WorkloadPodsTable } from './workload-pods-table'
import { WorkloadConditions } from './workload-conditions'
import {
  replicaControllerHealth,
  type ReplicaControllerConfiguration,
} from './replica-controller-model'
import type { ReplicationControllerDetail } from '../replicationcontrollers/types'
import './replica-controller-overview.css'
export function ReplicaControllerOverview({
  kind,
  detail,
  scope,
  configuration,
  currentReplicas,
  onTabChange,
  asideExtra,
  summaryExtra,
  summaryNote,
}: {
  kind: 'ReplicaSet' | 'ReplicationController'
  detail: Omit<ReplicationControllerDetail, 'currentReplicas'>
  scope: ScopeKey
  configuration?: ReplicaControllerConfiguration
  currentReplicas?: number
  onTabChange: (tab: string) => void
  asideExtra?: ReactNode
  summaryExtra?: ReactNode
  summaryNote?: ReactNode
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const health = replicaControllerHealth(
    { ...detail, currentReplicas },
    configuration,
    kind === 'ReplicationController',
  )
  const titles = {
    healthy: zh ? `${kind} 当前副本已就绪且可用` : `${kind} replicas are ready and available`,
    warning: zh ? `${kind} 仍有副本未就绪或不可用` : `${kind} has replicas not ready or available`,
    inactive: zh ? '期望副本为 0' : 'Desired replica count is zero',
    unknown: zh ? '副本状态数据未完整返回' : 'Replica status is incomplete',
    failed: zh ? `${kind} 报告副本创建失败` : `${kind} reports a replica failure`,
  }
  return (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '运行配置' : 'Configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                { label: zh ? '命名空间' : 'Namespace', children: detail.namespace },
                {
                  label: zh ? '创建时间' : 'Created',
                  children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
                },
                { label: 'Generation', children: configuration?.generation ?? '—' },
                {
                  label: zh ? '已观察 Generation' : 'Observed generation',
                  children: configuration?.observedGeneration ?? '—',
                },
                {
                  label: zh ? '最短就绪 (s)' : 'Min ready (s)',
                  children: configuration?.minReadySeconds ?? '—',
                },
                {
                  label: zh ? '完整标签副本' : 'Fully labeled replicas',
                  children: configuration?.fullyLabeledReplicas ?? '—',
                },
                {
                  label: zh ? '选择器' : 'Selector',
                  children:
                    Object.entries(detail.selector || {})
                      .map(([k, v]) => `${k}=${v}`)
                      .join(', ') || '—',
                },
              ]}
            />
          </Card>
          {asideExtra}
        </>
      }
    >
      <Alert
        showIcon
        type={
          health === 'healthy'
            ? 'success'
            : health === 'failed'
              ? 'error'
              : health === 'warning'
                ? 'warning'
                : 'info'
        }
        title={titles[health]}
        description={
          configuration?.conditions.find((c) => c.type === 'ReplicaFailure' && c.status === 'True')
            ?.message
        }
        action={
          <Button type="link" onClick={() => onTabChange('events')}>
            {zh ? '查看事件' : 'View events'}
          </Button>
        }
      />
      <Card
        className="soha-detail-card"
        title={zh ? '副本与模板' : 'Replicas and template'}
        extra={summaryExtra}
      >
        <WorkloadOverviewFacts
          facts={[
            {
              key: 'desired',
              label: zh ? '期望' : 'Desired',
              value: detail.desiredReplicas,
              target: detail.desiredReplicas,
            },
            {
              key: 'current',
              label: zh ? '当前' : 'Current',
              value: currentReplicas,
              target: detail.desiredReplicas,
              showTarget: true,
            },
            {
              key: 'ready',
              label: zh ? '就绪' : 'Ready',
              value: detail.readyReplicas,
              target: detail.desiredReplicas,
              showTarget: true,
            },
            {
              key: 'available',
              label: zh ? '可用' : 'Available',
              value: detail.availableReplicas,
              target: detail.desiredReplicas,
              showTarget: true,
              tone: detail.availableReplicas < detail.desiredReplicas ? 'warning' : 'success',
            },
          ]}
        />
        {summaryNote && <Typography.Text type="secondary">{summaryNote}</Typography.Text>}
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? 'Pod 模板' : 'Pod template'}
        extra={
          configuration
            ? `${configuration.containers.length} ${zh ? '个容器' : 'containers'}`
            : undefined
        }
      >
        {configuration?.containers.length ? (
          configuration.containers.map((c) => (
            <article className="soha-replica-controller-template" key={`${c.init}/${c.name}`}>
              <strong>
                {c.name} {c.init && <Typography.Text type="secondary">init</Typography.Text>}
              </strong>
              <Typography.Text copyable>{c.image}</Typography.Text>
            </article>
          ))
        ) : (
          <Typography.Text type="secondary">
            {zh
              ? '模板未返回，完整配置可在 YAML 查看'
              : 'Template unavailable; see YAML for full configuration'}
          </Typography.Text>
        )}
      </Card>
      <WorkloadPodsTable
        pods={detail.pods || []}
        scope={scope}
        compact
        onShowAll={() => onTabChange('pods')}
      />
      <WorkloadConditions conditions={configuration?.conditions} />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
