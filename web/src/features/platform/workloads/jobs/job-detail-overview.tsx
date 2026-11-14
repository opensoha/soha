import { Alert, Button, Card, Typography } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { BooleanTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import type { ScopeKey } from '@/types'
import { Link } from 'react-router-dom'
import { buildRelatedResourcePath } from '@/features/platform/workloads-model'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewFacts,
  WorkloadOverviewMetadata,
} from '../shared/workload-overview'
import { WorkloadPodsTable } from '../shared/workload-pods-table'
import { WorkloadConditions } from '../shared/workload-conditions'
import {
  formatJobDuration,
  jobCompletionTarget,
  jobDuration,
  jobEndTime,
  jobState,
  jobStateLabel,
  type JobTemplateContainer,
} from './job-detail-model'
import type { JobDetail } from './types'

export function JobDetailOverview({
  detail,
  scope,
  template,
  onTabChange,
}: {
  detail: JobDetail
  scope: ScopeKey
  template?: JobTemplateContainer[]
  onTabChange: (tab: string) => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const state = jobState(detail),
    target = jobCompletionTarget(detail),
    duration = jobDuration(detail)
  const owner = detail.relatedResources?.find(
    (r) => r.kind === 'CronJob' && r.relation === 'owner' && r.namespace === detail.namespace,
  )
  const date = (value?: string) => (value ? formatDateTime(value) : '—')
  const path = owner && buildRelatedResourcePath(owner, detail.namespace, scope.clusterId)
  return (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '运行配置' : 'Configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                { label: zh ? '命名空间' : 'Namespace', children: detail.namespace },
                { label: zh ? '并行度' : 'Parallelism', children: detail.parallelism ?? '—' },
                {
                  label: zh ? '完成模式' : 'Completion mode',
                  children: detail.completionMode || '—',
                },
                { label: zh ? '重试上限' : 'Backoff limit', children: detail.backoffLimit ?? '—' },
                {
                  label: zh ? '截止时长 (s)' : 'Deadline (s)',
                  children: detail.activeDeadlineSeconds ?? '—',
                },
                {
                  label: zh ? '完成后保留 (s)' : 'TTL after finish (s)',
                  children: detail.ttlSecondsAfterFinished ?? '—',
                },
                {
                  label: zh ? '已暂停' : 'Suspended',
                  children:
                    detail.suspend === undefined ? '—' : <BooleanTag value={detail.suspend} />,
                },
              ]}
            />
          </Card>
          <Card className="soha-detail-card" title={zh ? '执行时间' : 'Execution times'}>
            <WorkloadOverviewConfiguration
              items={[
                { label: zh ? '开始' : 'Started', children: date(detail.startTime) },
                { label: zh ? '完成' : 'Completed', children: date(detail.completionTime) },
                {
                  label: zh ? '终态时间' : 'Terminal condition',
                  children: date(jobEndTime(detail)),
                },
                {
                  label: zh ? '本轮耗时' : 'Elapsed',
                  children: duration === undefined ? '—' : formatJobDuration(duration),
                },
                { label: zh ? '创建' : 'Created', children: date(detail.createdAt) },
              ]}
            />
          </Card>
          {owner && (
            <Card className="soha-detail-card" title={zh ? '所属 CronJob' : 'Owning CronJob'}>
              {path && hasPermission(permissions, 'platform.workloads.cron-jobs.view') ? (
                <Link to={path}>{owner.name}</Link>
              ) : (
                owner.name
              )}
            </Card>
          )}
        </>
      }
    >
      <Alert
        showIcon
        type={
          state === 'failed'
            ? 'error'
            : state === 'completed'
              ? 'success'
              : ['terminating', 'failures-recorded'].includes(state)
                ? 'warning'
                : 'info'
        }
        title={`Job ${jobStateLabel(state, zh)}`}
        description={
          detail.conditions?.find(
            (c) => ['Failed', 'FailureTarget'].includes(c.type) && c.status === 'True',
          )?.message
        }
        action={
          <Button type="link" onClick={() => onTabChange('events')}>
            {zh ? '查看事件' : 'View events'}
          </Button>
        }
      />
      <Card className="soha-detail-card" title={zh ? '执行进度' : 'Execution progress'}>
        <WorkloadOverviewFacts
          facts={[
            {
              key: 'succeeded',
              label: zh ? '成功 / 目标' : 'Succeeded / target',
              value: detail.succeeded,
              target,
              showTarget: true,
            },
            {
              key: 'active',
              label: zh ? '活跃 Pods' : 'Active Pods',
              value: detail.active,
              tone: 'primary',
            },
            {
              key: 'failed',
              label: zh ? '失败 Pod 记录' : 'Failed Pods',
              value: detail.failed,
              tone: 'warning',
            },
          ]}
        />
        <div className="soha-workload-overview-facts-footer">
          <Typography.Text type="secondary">
            {zh
              ? '失败 Pod 记录不等于 Job 终态失败'
              : 'Failed Pods do not imply a terminal Job failure'}
            {target === undefined
              ? zh
                ? ' · 未指定固定完成目标'
                : ' · No fixed completion target'
              : ''}
          </Typography.Text>
          <Button type="link" onClick={() => onTabChange('logs')}>
            {zh ? '查看日志' : 'View logs'}
          </Button>
        </div>
      </Card>
      <Card className="soha-detail-card" title={zh ? '执行模板' : 'Execution template'}>
        {template === undefined ? (
          <Typography.Text type="secondary">
            {zh ? '模板尚未返回' : 'Template unavailable'}
          </Typography.Text>
        ) : (
          template.map((c) => (
            <div className="soha-job-template" key={`${c.init}/${c.name}`}>
              <strong>
                {c.name} {c.init ? (zh ? '· 初始化容器' : '· init') : ''}
              </strong>
              <Typography.Text copyable>{c.image}</Typography.Text>
              {Boolean(c.command.length || c.args.length) && (
                <code>{[...c.command, ...c.args].join(' ')}</code>
              )}
            </div>
          ))
        )}
      </Card>
      <WorkloadPodsTable
        compact
        pods={detail.pods || []}
        scope={scope}
        onShowAll={() => onTabChange('pods')}
      />
      <WorkloadConditions
        title={zh ? '执行条件' : 'Conditions'}
        conditions={detail.conditions}
        showTransitionTime
      />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
