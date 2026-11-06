import { Alert, Button, Card, Typography } from 'antd'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import type { ScopeKey, RolloutHistory } from '@/types'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewFacts,
  WorkloadOverviewMetadata,
} from '../shared/workload-overview'
import { WorkloadPodsTable } from '../shared/workload-pods-table'
import type { DaemonSetDetail } from './types'
import type { DaemonSetConfiguration } from './daemonset-configuration'
import { daemonSetState } from './daemonset-detail-model'
export function DaemonSetDetailOverview({
  detail,
  configuration,
  currentTemplate,
  scope,
  onTabChange,
}: {
  detail: DaemonSetDetail
  configuration?: DaemonSetConfiguration
  currentTemplate?: RolloutHistory
  scope: ScopeKey
  onTabChange: (key: string) => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN',
    state = daemonSetState(detail, configuration),
    desired = detail.desiredNumber
  const entries = (values: Record<string, string>) =>
    Object.entries(values)
      .map(([k, v]) => `${k}=${v}`)
      .join(', ') || '—'
  return (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '运行配置' : 'Configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                { label: zh ? '命名空间' : 'Namespace', children: detail.namespace },
                {
                  label: zh ? '更新策略' : 'Update strategy',
                  children: configuration?.strategy || detail.updateStrategy || '—',
                },
                {
                  label: zh ? '最大不可用' : 'Max unavailable',
                  children: configuration?.maxUnavailable ?? '—',
                },
                { label: zh ? '最大增量' : 'Max surge', children: configuration?.maxSurge ?? '—' },
                {
                  label: zh ? '就绪等待 (s)' : 'Min ready (s)',
                  children: configuration?.minReadySeconds ?? '—',
                },
                { label: zh ? '创建时间' : 'Created', children: formatDateTime(detail.createdAt) },
                { label: zh ? '选择器' : 'Selector', children: entries(detail.selector || {}) },
              ]}
            />
          </Card>
          <Card className="soha-detail-card" title={zh ? '调度配置' : 'Scheduling'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '节点选择器' : 'Node selector',
                  children: configuration ? entries(configuration.nodeSelector) : '—',
                },
                {
                  label: zh ? '容忍' : 'Tolerations',
                  children: configuration?.tolerations.length
                    ? configuration.tolerations.join('\n')
                    : '—',
                },
              ]}
            />
          </Card>
          <Card className="soha-detail-card" title={zh ? '版本' : 'Revisions'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '模板版本' : 'Template revision',
                  children: currentTemplate?.revision || '—',
                },
                {
                  label: zh ? '修订名称' : 'Revision name',
                  children: currentTemplate?.name || '—',
                },
                {
                  label: zh ? '历史保留数' : 'Revision retention',
                  children: configuration?.revisionHistoryLimit ?? '—',
                },
              ]}
            />
            <Button type="link" onClick={() => onTabChange('revisions')}>
              {zh ? '查看版本历史' : 'View revision history'}
            </Button>
          </Card>
        </>
      }
    >
      <Alert
        showIcon
        type={
          state === 'healthy'
            ? 'success'
            : state === 'unknown' || state === 'no-targets'
              ? 'info'
              : 'warning'
        }
        title={
          zh
            ? state === 'healthy'
              ? 'DaemonSet 当前目标节点已就绪'
              : state === 'no-targets'
                ? 'DaemonSet 当前无匹配目标节点'
                : state === 'progressing'
                  ? 'DaemonSet 正在更新'
                  : state === 'degraded'
                    ? 'DaemonSet 节点覆盖或就绪状态待检查'
                    : 'DaemonSet 状态尚未确认'
            : `DaemonSet · ${state}`
        }
        action={
          <Button type="link" onClick={() => onTabChange('events')}>
            {zh ? '查看事件' : 'View events'}
          </Button>
        }
      />
      <Card
        className="soha-detail-card"
        title={zh ? '节点覆盖与发布' : 'Node coverage and rollout'}
      >
        <WorkloadOverviewFacts
          facts={[
            {
              key: 'desired',
              label: zh ? '期望节点' : 'Desired nodes',
              value: desired,
              target: desired,
            },
            {
              key: 'current',
              label: zh ? '已调度' : 'Scheduled',
              value: detail.currentNumber,
              target: desired,
              showTarget: true,
            },
            {
              key: 'updated',
              label: zh ? '已更新' : 'Updated',
              value: detail.updatedNumber,
              target: desired,
              showTarget: true,
              tone: 'primary',
            },
            {
              key: 'ready',
              label: zh ? '就绪' : 'Ready',
              value: detail.readyNumber,
              target: desired,
              showTarget: true,
            },
            {
              key: 'available',
              label: zh ? '可用' : 'Available',
              value: detail.availableNumber,
              target: desired,
              showTarget: true,
            },
          ]}
        />
        <div className="soha-workload-overview-facts-footer">
          <span>{configuration?.strategy || detail.updateStrategy || '—'}</span>
          <span>
            {zh ? '不可用' : 'Unavailable'} {configuration?.unavailable ?? '—'} ·{' '}
            {zh ? '误调度' : 'Misscheduled'} {configuration?.misscheduled ?? '—'}
          </span>
          <span>
            Generation {configuration?.observedGeneration ?? '—'} /{' '}
            {configuration?.generation ?? '—'}
          </span>
          <Button type="link" onClick={() => onTabChange('pods')}>
            {zh ? '查看 Pods' : 'View Pods'}
          </Button>
        </div>
        {configuration?.strategy === 'OnDelete' && (
          <Typography.Paragraph type="secondary">
            {zh
              ? 'OnDelete 策略不会自动替换已有 Pod，模板更新后需按运维流程处理 Pod。'
              : 'OnDelete does not automatically replace existing Pods after a template update.'}
          </Typography.Paragraph>
        )}
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? '当前模板' : 'Current template'}
        extra={
          configuration
            ? `${configuration.containers.length} ${zh ? '个容器' : 'containers'}`
            : undefined
        }
      >
        {configuration?.containers.map((c) => (
          <div className="soha-controller-template" key={c.name}>
            <strong>
              {c.name} {c.init && <Typography.Text type="secondary">init</Typography.Text>}
            </strong>
            <Typography.Text copyable>{c.image}</Typography.Text>
          </div>
        ))}
        {!configuration && (
          <Typography.Text type="secondary">
            {zh
              ? '配置尚未读取，请查看上方读取状态。'
              : 'Configuration has not been read; see the read status above.'}
          </Typography.Text>
        )}
      </Card>
      <WorkloadPodsTable
        compact
        pods={detail.pods || []}
        scope={scope}
        onShowAll={() => onTabChange('pods')}
      />
      {Boolean(configuration?.conditions.length) && (
        <Card className="soha-detail-card" title={zh ? '运行条件' : 'Conditions'}>
          {configuration!.conditions.map((c) => (
            <div key={c.type}>
              <strong>{c.type}</strong>{' '}
              {['True', 'False'].includes(c.status) ? (
                <BooleanTag value={c.status === 'True'} />
              ) : (
                <StatusTag value={c.status} />
              )}{' '}
              {c.reason} {c.message}
            </div>
          ))}
        </Card>
      )}
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
