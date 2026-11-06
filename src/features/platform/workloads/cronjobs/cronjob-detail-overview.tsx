import { Alert, Button, Card, Typography } from 'antd'
import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { formatDateTime } from '@/utils/time'
import { jobState } from '../shared/job-execution'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewFacts,
  WorkloadOverviewMetadata,
} from '../shared/workload-overview'
import { CronJobJobsTable } from './cronjob-jobs-table'
import type { CronJobConfiguration } from './cronjob-detail-model'
import type { CronJobDetail, CronJobChildJob } from './types'

export function CronJobDetailOverview({
  detail,
  jobs,
  configuration,
  scope,
  onTabChange,
}: {
  detail: CronJobDetail
  jobs?: CronJobChildJob[]
  configuration?: CronJobConfiguration
  scope: ScopeKey
  onTabChange: (tab: string) => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const date = (v?: string) => (v ? formatDateTime(v) : '—')
  return (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '调度配置' : 'Schedule configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                { label: zh ? '命名空间' : 'Namespace', children: detail.namespace },
                {
                  label: zh ? '时区' : 'Time zone',
                  children:
                    detail.timeZone ||
                    (zh ? '控制器时区（未显式设置）' : 'Controller time zone (not explicit)'),
                },
                {
                  label: zh ? '并发策略' : 'Concurrency',
                  children: detail.concurrencyPolicy || '—',
                },
                {
                  label: zh ? '调度截止 (s)' : 'Starting deadline (s)',
                  children:
                    configuration?.startingDeadlineSeconds ??
                    (configuration ? (zh ? '未设置' : 'Not set') : '—'),
                },
                {
                  label: zh ? '成功历史上限' : 'Successful history',
                  children: configuration?.successfulJobsHistoryLimit ?? '—',
                },
                {
                  label: zh ? '失败历史上限' : 'Failed history',
                  children: configuration?.failedJobsHistoryLimit ?? '—',
                },
                { label: zh ? '创建时间' : 'Created', children: date(detail.createdAt) },
              ]}
            />
          </Card>
          <Card className="soha-detail-card" title={zh ? 'Job 配置' : 'Job configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '并行度' : 'Parallelism',
                  children: configuration?.parallelism ?? '—',
                },
                {
                  label: zh ? '完成目标' : 'Completions',
                  children: configuration?.completions ?? '—',
                },
                {
                  label: zh ? '重试上限' : 'Backoff limit',
                  children: configuration?.backoffLimit ?? '—',
                },
                {
                  label: zh ? '执行截止 (s)' : 'Active deadline (s)',
                  children:
                    configuration?.activeDeadlineSeconds ??
                    (configuration ? (zh ? '未设置' : 'Not set') : '—'),
                },
              ]}
            />
          </Card>
        </>
      }
    >
      <Alert
        showIcon
        type={detail.suspend ? 'warning' : 'info'}
        title={
          detail.suspend === undefined
            ? zh
              ? '调度状态未返回'
              : 'Schedule state unavailable'
            : detail.suspend
              ? zh
                ? '后续调度已暂停'
                : 'Future scheduling is suspended'
              : zh
                ? '调度已启用'
                : 'Scheduling is enabled'
        }
        description={
          detail.suspend
            ? zh
              ? '已启动的 Jobs 仍会继续执行。'
              : 'Existing Jobs continue executing.'
            : zh
              ? '调度启用不代表每次执行成功，执行结果见 Jobs。'
              : 'Enabled scheduling does not guarantee successful executions. See Jobs.'
        }
        action={
          <Button type="link" onClick={() => onTabChange('events')}>
            {zh ? '查看事件' : 'View events'}
          </Button>
        }
      />
      <Card className="soha-detail-card" title={zh ? '调度与执行' : 'Schedule and executions'}>
        <div className="soha-cronjob-schedule">
          <Typography.Text code>{detail.schedule}</Typography.Text>
          <Typography.Text type="secondary">
            {detail.timeZone || (zh ? '控制器时区' : 'Controller time zone')}
          </Typography.Text>
        </div>
        <WorkloadOverviewFacts
          facts={[
            {
              key: 'active',
              label: zh ? '活跃 Jobs' : 'Active Jobs',
              value: detail.activeJobs,
              tone: 'primary',
            },
            {
              key: 'retained',
              label: zh ? '保留执行' : 'Retained executions',
              value: jobs?.length,
            },
            {
              key: 'success',
              label: zh ? '已完成' : 'Completed',
              value: jobs?.filter((j) => jobState(j) === 'completed').length,
            },
            {
              key: 'failed',
              label: zh ? '终态失败' : 'Terminal failures',
              value: jobs?.filter((j) => jobState(j) === 'failed').length,
              tone: 'warning',
            },
          ]}
        />
        <WorkloadOverviewConfiguration
          items={[
            { label: zh ? '最近调度' : 'Last schedule', children: date(detail.lastScheduleTime) },
            {
              label: zh ? '最近成功' : 'Last successful',
              children: date(configuration?.lastSuccessfulTime),
            },
          ]}
        />
      </Card>
      <Card className="soha-detail-card" title={zh ? '执行模板' : 'Execution template'}>
        {configuration?.containers.length ? (
          configuration.containers.map((c) => (
            <article className="soha-cronjob-template" key={`${c.init}/${c.name}`}>
              <strong>
                {c.name} {c.init && <Typography.Text type="secondary">init</Typography.Text>}
              </strong>
              <Typography.Text copyable>{c.image}</Typography.Text>
              {(c.command.length > 0 || c.args.length > 0) && (
                <pre>{[...c.command, ...c.args].join(' ')}</pre>
              )}
            </article>
          ))
        ) : (
          <Typography.Text type="secondary">
            {zh
              ? '模板尚未返回，完整配置可在 YAML 查看'
              : 'Template unavailable; see YAML for full configuration'}
          </Typography.Text>
        )}
      </Card>
      <CronJobJobsTable jobs={jobs} scope={scope} compact onShowAll={() => onTabChange('jobs')} />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
