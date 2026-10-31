import { useState } from 'react'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import type { TableColumnsType } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { AdminTable } from '@/components/admin-table'
import { BooleanTag } from '@/components/status-tag'
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

export function ControllerHeaderFacts({
  detail,
  resource,
}: {
  detail: WorkloadMeta
  resource: WorkloadKind
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  if (resource === 'cronjobs')
    return typeof detail.suspend === 'boolean' ? (
      <span>
        {detail.suspend
          ? zh
            ? '已暂停调度'
            : 'Scheduling suspended'
          : zh
            ? '调度中'
            : 'Scheduling active'}
      </span>
    ) : null
  const current =
    resource === 'daemonsets'
      ? detail.readyNumber
      : resource === 'jobs'
        ? detail.succeeded
        : detail.readyReplicas
  const desired =
    resource === 'daemonsets'
      ? detail.desiredNumber
      : resource === 'jobs'
        ? detail.completions
        : detail.desiredReplicas
  return typeof current === 'number' && typeof desired === 'number' ? (
    <span>
      {zh ? (resource === 'jobs' ? '完成' : '就绪') : resource === 'jobs' ? 'Completed' : 'Ready'}{' '}
      {current} / {desired}
    </span>
  ) : null
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
  const number = (field: string) =>
    typeof detail[field] === 'number' && Number.isFinite(detail[field])
      ? (detail[field] as number)
      : undefined
  const desired = number(resource === 'daemonsets' ? 'desiredNumber' : 'desiredReplicas')
  const ready = number(resource === 'daemonsets' ? 'readyNumber' : 'readyReplicas')
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
  const revisionsDiffer =
    resource === 'statefulsets' &&
    typeof detail.currentRevision === 'string' &&
    Boolean(detail.currentRevision) &&
    typeof detail.updateRevision === 'string' &&
    Boolean(detail.updateRevision) &&
    detail.currentRevision !== detail.updateRevision
  let summary: { type: 'info' | 'success' | 'warning'; title: string } = {
    type: 'info',
    title: zh ? `${title} 就绪数据尚未返回` : `${title} readiness is unavailable`,
  }
  if (resource === 'cronjobs') {
    summary = {
      type: 'info',
      title:
        typeof detail.suspend !== 'boolean'
          ? zh
            ? 'CronJob 调度状态尚未返回'
            : 'CronJob scheduling status is unavailable'
          : detail.suspend
            ? zh
              ? 'CronJob 已暂停后续调度'
              : 'CronJob scheduling is suspended'
            : zh
              ? 'CronJob 调度已启用'
              : 'CronJob scheduling is enabled',
    }
  } else if (resource === 'jobs') {
    const succeeded = number('succeeded')
    const completions = number('completions')
    const active = number('active')
    const failed = number('failed')
    summary =
      succeeded !== undefined &&
      completions !== undefined &&
      completions > 0 &&
      succeeded >= completions
        ? {
            type: 'success',
            title: zh
              ? `Job 已完成 ${succeeded} 个执行`
              : `Job has ${succeeded} successful completions`,
          }
        : active !== undefined && active > 0
          ? {
              type: 'info',
              title: zh ? `Job 当前有 ${active} 个活跃 Pod` : `Job has ${active} active Pods`,
            }
          : failed !== undefined && failed > 0
            ? {
                type: 'warning',
                title: zh
                  ? `Job 已记录 ${failed} 个失败 Pod`
                  : `Job has recorded ${failed} failed Pods`,
              }
            : { type: 'info', title: zh ? 'Job 尚无完成结果' : 'Job has no completion result yet' }
  } else if (desired === 0) {
    summary = { type: 'info', title: zh ? '已缩容至 0 个副本' : 'Scaled to zero replicas' }
  } else if (desired !== undefined && ready !== undefined) {
    summary =
      ready < desired
        ? {
            type: 'warning',
            title: zh ? `${title} 仍有副本未就绪` : `${title} has replicas that are not ready`,
          }
        : revisionsDiffer
          ? {
              type: 'warning',
              title: zh
                ? `${title} 当前副本已就绪，版本仍有差异`
                : `${title} replicas are ready; revisions still differ`,
            }
          : {
              type: 'success',
              title: zh ? `${title} 当前副本已就绪` : `${title} replicas are ready`,
            }
  }
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
          {resource === 'cronjobs' && <ControllerHeaderFacts detail={detail} resource={resource} />}
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
