import { useState } from 'react'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import type { TableColumnsType } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { AdminTable } from '@/components/admin-table'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import { Alert, Button, Card, Input } from 'antd'
import { Link } from 'react-router-dom'
import { buildWorkloadDetailPath } from '@/features/platform/workloads-model'
import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { useWorkloadDetailScope } from './detail-scope'
import type { WorkloadMeta } from './detail-shell'
import type { WorkloadKind } from './types'
import type { Pod } from '@/types'
import { WorkloadPodsCard } from './workload-relations'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewFacts,
  WorkloadOverviewMetadata,
  type WorkloadOverviewFact,
} from './workload-overview'

export interface ChildJob {
  name: string
  namespace: string
  succeeded: number
  failed: number
  active: number
  ageSeconds: number
}

function controllerCount(detail: WorkloadMeta, field: string) {
  const value = detail[field]
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : undefined
}

function controllerSummary(
  detail: WorkloadMeta,
  resource: WorkloadKind,
  title: string,
  zh: boolean,
) {
  const desired = controllerCount(
    detail,
    resource === 'daemonsets' ? 'desiredNumber' : 'desiredReplicas',
  )
  const ready = controllerCount(detail, resource === 'daemonsets' ? 'readyNumber' : 'readyReplicas')
  const revisionsDiffer =
    resource === 'statefulsets' &&
    typeof detail.currentRevision === 'string' &&
    Boolean(detail.currentRevision.trim()) &&
    typeof detail.updateRevision === 'string' &&
    Boolean(detail.updateRevision.trim()) &&
    detail.currentRevision !== detail.updateRevision
  if (resource === 'cronjobs') {
    if (typeof detail.suspend !== 'boolean')
      return {
        type: 'info' as const,
        status: 'unknown',
        label: zh ? '调度状态未知' : 'Scheduling unknown',
        title: zh ? 'CronJob 调度状态尚未返回' : 'CronJob scheduling status is unavailable',
      }
    return detail.suspend
      ? {
          type: 'info' as const,
          status: 'disabled',
          label: zh ? '已暂停' : 'Suspended',
          title: zh ? 'CronJob 已暂停后续调度' : 'CronJob scheduling is suspended',
        }
      : {
          type: 'info' as const,
          status: 'enabled',
          label: zh ? '调度已启用' : 'Scheduling enabled',
          title: zh ? 'CronJob 调度已启用' : 'CronJob scheduling is enabled',
        }
  }
  if (resource === 'jobs') {
    const succeeded = controllerCount(detail, 'succeeded')
    const completions = controllerCount(detail, 'completions')
    const active = controllerCount(detail, 'active')
    const failed = controllerCount(detail, 'failed')
    if (
      succeeded !== undefined &&
      completions !== undefined &&
      completions > 0 &&
      succeeded >= completions
    )
      return {
        type: 'success' as const,
        status: 'completed',
        label: zh ? '完成数达标' : 'Completion target met',
        title: zh
          ? `Job 已完成 ${succeeded} 个执行`
          : `Job has ${succeeded} successful completions`,
      }
    if (active !== undefined && active > 0)
      return {
        type: 'info' as const,
        status: 'progressing',
        label: zh ? '执行中' : 'Executing',
        title: zh ? `Job 当前有 ${active} 个活跃 Pod` : `Job has ${active} active Pods`,
      }
    if (failed !== undefined && failed > 0)
      return {
        type: 'warning' as const,
        status: 'warning',
        label: zh ? '有失败记录' : 'Failures recorded',
        title: zh ? `Job 已记录 ${failed} 个失败 Pod` : `Job has recorded ${failed} failed Pods`,
      }
    return {
      type: 'info' as const,
      status: 'unknown',
      label: zh ? '暂无完成结果' : 'No completion result',
      title: zh ? 'Job 尚无完成结果' : 'Job has no completion result yet',
    }
  }
  if (desired === 0)
    return {
      type: 'info' as const,
      status: 'inactive',
      label: zh ? '期望为 0' : 'Zero desired',
      title: zh ? '期望数量为 0' : 'Desired count is zero',
    }
  if (desired === undefined || ready === undefined)
    return {
      type: 'info' as const,
      status: 'unknown',
      label: zh ? '就绪未知' : 'Readiness unknown',
      title: zh ? `${title} 就绪数据尚未返回` : `${title} readiness is unavailable`,
    }
  if (ready < desired)
    return {
      type: 'warning' as const,
      status: 'warning',
      label: zh ? '副本未就绪' : 'Replicas not ready',
      title: zh ? `${title} 仍有副本未就绪` : `${title} has replicas that are not ready`,
    }
  const updated = controllerCount(detail, 'updatedNumber')
  if (resource === 'daemonsets' && updated !== undefined && updated < desired)
    return {
      type: 'warning' as const,
      status: 'warning',
      label: zh ? '部分未更新' : 'Update incomplete',
      title: zh
        ? `${title} 当前副本已就绪，仍有副本未更新`
        : `${title} replicas are ready; some are not updated`,
    }
  if (revisionsDiffer)
    return {
      type: 'warning' as const,
      status: 'warning',
      label: zh ? '版本有差异' : 'Revisions differ',
      title: zh
        ? `${title} 当前副本已就绪，版本仍有差异`
        : `${title} replicas are ready; revisions still differ`,
    }
  const current = controllerCount(
    detail,
    resource === 'daemonsets' ? 'currentNumber' : 'currentReplicas',
  )
  const available = controllerCount(detail, 'availableNumber')
  if (resource === 'daemonsets' && available !== undefined && available < desired)
    return {
      type: 'warning' as const,
      status: 'warning',
      label: zh ? '部分不可用' : 'Availability incomplete',
      title: zh ? `${title} 仍有副本不可用` : `${title} has replicas that are not available`,
    }
  const healthy =
    current !== undefined &&
    current >= desired &&
    (resource === 'statefulsets'
      ? typeof detail.currentRevision === 'string' &&
        Boolean(detail.currentRevision.trim()) &&
        detail.currentRevision === detail.updateRevision
      : resource === 'daemonsets' &&
        updated !== undefined &&
        updated >= desired &&
        available !== undefined &&
        available >= desired)
  if (healthy)
    return {
      type: 'success' as const,
      status: 'healthy',
      label: zh ? '健康' : 'Healthy',
      title: zh ? `${title} 当前副本已就绪` : `${title} replicas are ready`,
    }
  return {
    type: 'success' as const,
    status: 'ready',
    label: zh ? '副本就绪' : 'Replicas ready',
    title: zh ? `${title} 当前副本已就绪` : `${title} replicas are ready`,
  }
}

export function ControllerStatusTag({
  detail,
  resource,
  title,
}: {
  detail: WorkloadMeta
  resource: WorkloadKind
  title: string
}) {
  const { localeCode } = useI18n()
  const summary = controllerSummary(detail, resource, title, localeCode === 'zh_CN')
  return <StatusTag value={summary.status} label={summary.label} />
}

export function ControllerHeaderFacts({
  detail,
  resource,
}: {
  detail: WorkloadMeta
  resource: WorkloadKind
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const count = (field: string) => controllerCount(detail, field) ?? '—'
  const text = (field: string) =>
    typeof detail[field] === 'string' ? (detail[field] as string).trim() : ''
  const revision = (field: string) => {
    const full = text(field)
    if (!full) return null
    const prefix = `${detail.name}-`
    const short = full.startsWith(prefix) ? full.slice(prefix.length) : full
    return (
      <span className="soha-controller-header-revision" title={full}>
        {field === 'currentRevision'
          ? zh
            ? '当前版本'
            : 'Current revision'
          : zh
            ? '目标版本'
            : 'Target revision'}{' '}
        <span>{short}</span>
      </span>
    )
  }
  if (resource === 'cronjobs')
    return (
      <>
        <span title={text('schedule')}>
          {zh ? '调度' : 'Schedule'} {text('schedule') || '—'}
        </span>
        <span>
          {zh ? '活跃 Jobs' : 'Active Jobs'} {count('activeJobs')}
        </span>
        {text('timeZone') && <span>{text('timeZone')}</span>}
      </>
    )
  if (resource === 'jobs')
    return (
      <>
        <span>
          {zh ? '完成' : 'Completed'} {count('succeeded')} / {count('completions')}
        </span>
        <span>
          {zh ? '活跃' : 'Active'} {count('active')}
        </span>
        <span>
          {zh ? '失败记录' : 'Failed Pods'} {count('failed')}
        </span>
        {text('completionMode') && <span>{text('completionMode')}</span>}
      </>
    )
  return (
    <>
      <span>
        {count(resource === 'daemonsets' ? 'readyNumber' : 'readyReplicas')} /{' '}
        {count(resource === 'daemonsets' ? 'desiredNumber' : 'desiredReplicas')}{' '}
        {zh ? '就绪' : 'ready'}
      </span>
      {resource === 'statefulsets' && (
        <>
          {revision('currentRevision')}
          {text('updateRevision') !== text('currentRevision') && revision('updateRevision')}
        </>
      )}
      {resource === 'replicasets' && detail.annotations?.['deployment.kubernetes.io/revision'] && (
        <span title="deployment.kubernetes.io/revision">
          {zh ? '版本' : 'Revision'} {detail.annotations['deployment.kubernetes.io/revision']}
        </span>
      )}
      {resource === 'daemonsets' && (
        <span>
          {zh ? '已更新' : 'Updated'} {count('updatedNumber')} / {count('desiredNumber')}
        </span>
      )}
      {text('updateStrategy') && <span>{text('updateStrategy')}</span>}
    </>
  )
}

export function WorkloadJobsCard({
  jobs,
  preview = false,
}: {
  jobs: ChildJob[]
  preview?: boolean
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const scope = useWorkloadDetailScope()
  const canViewJobs = hasPermission(
    readableQueryData(usePermissionSnapshot())?.data,
    'platform.workloads.jobs.view',
  )
  const [, changeTab] = useResourceDetailTab()
  const [keyword, setKeyword] = useState('')
  const columns: TableColumnsType<ChildJob> = [
    {
      title: 'Job',
      dataIndex: 'name',
      width: 300,
      render: (name: string, job) =>
        canViewJobs ? (
          <Link
            to={buildWorkloadDetailPath(
              'jobs',
              name,
              scope.namespace,
              job.namespace,
              scope.clusterId,
            )}
          >
            {name}
          </Link>
        ) : (
          name
        ),
    },
    { title: zh ? '成功' : 'Succeeded', dataIndex: 'succeeded', width: 80 },
    { title: zh ? '失败' : 'Failed', dataIndex: 'failed', width: 80 },
    { title: zh ? '活跃' : 'Active', dataIndex: 'active', width: 80 },
    {
      title: zh ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 100,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  const rowKey = (job: ChildJob) => `${job.namespace}/${job.name}`
  if (preview)
    return (
      <Card
        className="soha-detail-card"
        title={`${zh ? '关联 Jobs' : 'Related Jobs'} ${jobs.length}`}
        extra={
          <Button type="link" onClick={() => changeTab('jobs')}>
            {zh ? '查看全部' : 'View all'}
          </Button>
        }
      >
        <AdminTable
          pagination={false}
          enableColumnSelection={false}
          dataSource={jobs.slice(0, 3)}
          rowKey={rowKey}
          columns={columns}
          scroll={{ x: 720 }}
        />
      </Card>
    )
  return (
    <div className="soha-detail-stack">
      <div className="soha-k8s-detail-toolbar">
        <Input
          aria-label={zh ? '搜索 Job' : 'Search Jobs'}
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          allowClear
          placeholder={zh ? '搜索 Job / 命名空间' : 'Search Job / namespace'}
        />
      </div>
      <KubernetesResourceTable
        resourceKey="cronjob-jobs"
        resourceLabel="Jobs"
        allRows={jobs}
        dataSource={jobs.filter((job) =>
          `${job.name} ${job.namespace}`.toLowerCase().includes(keyword.trim().toLowerCase()),
        )}
        columns={columns}
        rowKey={rowKey}
        filterKey={keyword}
        enableColumnSelection={false}
        localSorting
      />
    </div>
  )
}

export function ControllerOverview({
  detail,
  resource,
  title,
}: {
  detail: WorkloadMeta
  resource: WorkloadKind
  title: string
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const value = (field: string) =>
    typeof detail[field] === 'number' || typeof detail[field] === 'string'
      ? String(detail[field])
      : '—'
  const number = (field: string) => controllerCount(detail, field)
  const desired = number(resource === 'daemonsets' ? 'desiredNumber' : 'desiredReplicas')
  const fields =
    resource === 'daemonsets'
      ? [
          ['desiredNumber', '期望', 'Desired'],
          ['updatedNumber', '已更新', 'Updated'],
          ['readyNumber', '就绪', 'Ready'],
          ['availableNumber', '可用', 'Available'],
        ]
      : resource === 'jobs'
        ? [
            ['completions', '目标完成数', 'Target completions'],
            ['succeeded', '成功', 'Succeeded'],
            ['failed', '失败', 'Failed'],
            ['active', '活跃', 'Active'],
          ]
        : resource === 'cronjobs'
          ? [['activeJobs', '活跃 Jobs', 'Active Jobs']]
          : [
              ['desiredReplicas', '期望', 'Desired'],
              ...(resource === 'replicasets' ? [] : [['currentReplicas', '当前', 'Current']]),
              ['readyReplicas', '就绪', 'Ready'],
              ...(resource === 'statefulsets' ? [] : [['availableReplicas', '可用', 'Available']]),
            ]
  const isReplicaController = resource !== 'jobs' && resource !== 'cronjobs'
  const facts: WorkloadOverviewFact[] = fields.map(([field, cn, en], index) => ({
    key: field,
    label: zh ? cn : en,
    value: number(field),
    target: isReplicaController ? desired : undefined,
    showTarget: isReplicaController && index !== 0,
    tone:
      field === 'updatedNumber'
        ? 'primary'
        : desired !== undefined && (number(field) ?? desired) < desired
          ? 'warning'
          : 'success',
  }))
  const extra =
    resource === 'statefulsets'
      ? [
          ['updateStrategy', '更新策略', 'Update strategy'],
          ['serviceName', 'Service', 'Service'],
        ]
      : resource === 'daemonsets'
        ? [['updateStrategy', '更新策略', 'Update strategy']]
        : resource === 'jobs'
          ? [
              ['parallelism', '并行度', 'Parallelism'],
              ['completionMode', '模式', 'Mode'],
              ['startTime', '开始时间', 'Start time'],
              ['completionTime', '完成时间', 'Completion time'],
            ]
          : resource === 'cronjobs'
            ? [
                ['schedule', '调度计划', 'Schedule'],
                ['lastScheduleTime', '上次调度', 'Last schedule'],
                ['concurrencyPolicy', '并发策略', 'Concurrency'],
                ['timeZone', '时区', 'Time zone'],
              ]
            : []
  const selector =
    detail.selector && typeof detail.selector === 'object'
      ? Object.entries(detail.selector as Record<string, string>)
          .map(([key, val]) => `${key}=${val}`)
          .join(', ')
      : ''
  const conditions = Array.isArray(detail.conditions) ? detail.conditions : []
  const summary = controllerSummary(detail, resource, title, zh)
  const [, changeTab] = useResourceDetailTab()
  return (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '运行配置' : 'Configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                { label: zh ? '命名空间' : 'Namespace', children: detail.namespace },
                ...extra.map(([field, cn, en]) => ({
                  label: zh ? cn : en,
                  children:
                    field.endsWith('Time') && typeof detail[field] === 'string'
                      ? formatDateTime(detail[field] as string)
                      : value(field),
                })),
                {
                  label: zh ? '创建时间' : 'Created',
                  children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
                },
                { label: zh ? '选择器' : 'Selector', children: selector || '—' },
              ]}
            />
          </Card>
          {resource === 'statefulsets' && (
            <Card className="soha-detail-card" title={zh ? '版本' : 'Revisions'}>
              <WorkloadOverviewConfiguration
                items={[
                  {
                    label: zh ? '当前版本' : 'Current revision',
                    children: value('currentRevision'),
                  },
                  { label: zh ? '目标版本' : 'Update revision', children: value('updateRevision') },
                ]}
              />
            </Card>
          )}
        </>
      }
    >
      <Alert
        showIcon
        type={summary.type}
        title={summary.title}
        action={
          <Button type="link" onClick={() => changeTab('events')}>
            {zh ? '查看事件' : 'View events'}
          </Button>
        }
      />
      <Card
        className="soha-detail-card soha-controller-overview"
        title={
          resource === 'cronjobs'
            ? zh
              ? '调度与执行'
              : 'Schedule and execution'
            : resource === 'jobs'
              ? zh
                ? '执行状态'
                : 'Execution'
              : zh
                ? '副本与发布'
                : 'Replicas and rollout'
        }
      >
        <WorkloadOverviewFacts facts={facts} />
        <div className="soha-workload-overview-facts-footer">
          {(resource === 'statefulsets' || resource === 'daemonsets') && (
            <span>
              {zh ? '策略' : 'Strategy'} {value('updateStrategy')}
            </span>
          )}
          {resource === 'daemonsets' && (
            <span>
              {zh ? '当前' : 'Current'} {value('currentNumber')}
            </span>
          )}
          {resource === 'cronjobs' && typeof detail.suspend === 'boolean' && (
            <span>
              {detail.suspend
                ? zh
                  ? '已暂停调度'
                  : 'Scheduling suspended'
                : zh
                  ? '调度中'
                  : 'Scheduling active'}
            </span>
          )}
          <Button type="link" onClick={() => changeTab(resource === 'cronjobs' ? 'jobs' : 'pods')}>
            {zh
              ? resource === 'cronjobs'
                ? '查看 Jobs'
                : '查看 Pods'
              : resource === 'cronjobs'
                ? 'View Jobs'
                : 'View Pods'}
          </Button>
        </div>
      </Card>
      {resource !== 'cronjobs' && (
        <WorkloadPodsCard
          preview
          pods={Array.isArray(detail.pods) ? (detail.pods as Pod[]) : []}
          namespace={detail.namespace}
        />
      )}
      {resource === 'cronjobs' && (
        <WorkloadJobsCard
          preview
          jobs={Array.isArray(detail.jobs) ? (detail.jobs as ChildJob[]) : []}
        />
      )}
      {conditions.length > 0 && (
        <Card className="soha-detail-card" title={zh ? '运行条件' : 'Conditions'}>
          <AdminTable
            pagination={false}
            enableColumnSelection={false}
            dataSource={conditions}
            rowKey="type"
            scroll={{ x: 720, y: 360 }}
            columns={[
              { title: zh ? '条件' : 'Condition', dataIndex: 'type', width: 200 },
              {
                title: zh ? '状态' : 'Status',
                dataIndex: 'status',
                width: 100,
                render: (v: string) => <BooleanTag value={v === 'True'} />,
              },
              { title: zh ? '原因' : 'Reason', dataIndex: 'reason', width: 180 },
              { title: zh ? '消息' : 'Message', dataIndex: 'message' },
            ]}
          />
        </Card>
      )}
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
