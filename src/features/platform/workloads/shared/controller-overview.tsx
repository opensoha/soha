import { useState } from 'react'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import type { TableColumnsType } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { AdminTable } from '@/components/admin-table'
import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import { Button, Card, Descriptions, Input, Space } from 'antd'
import { Link } from 'react-router-dom'
import { buildWorkloadDetailPath } from '@/features/platform/workloads-model'
import { useResourceDetailTab } from '@/features/platform/shared/resource-detail-header'
import { useWorkloadDetailScope } from './detail-scope'
import type { WorkloadMeta } from './detail-shell'
import type { WorkloadKind } from './types'
import type { Pod } from '@/types'
import { WorkloadPodsCard } from './workload-relations'

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
  const fields =
    resource === 'daemonsets'
      ? [
          ['desiredNumber', '期望', 'Desired'],
          ['currentNumber', '当前', 'Current'],
          ['readyNumber', '就绪', 'Ready'],
          ['updatedNumber', '已更新', 'Updated'],
          ['availableNumber', '可用', 'Available'],
        ]
      : resource === 'jobs'
        ? [
            ['completions', '完成数', 'Completions'],
            ['succeeded', '成功', 'Succeeded'],
            ['failed', '失败', 'Failed'],
            ['active', '活跃', 'Active'],
          ]
        : resource === 'cronjobs'
          ? [['activeJobs', '活跃 Jobs', 'Active Jobs']]
          : [
              ['desiredReplicas', '期望', 'Desired'],
              ['currentReplicas', '当前', 'Current'],
              ['readyReplicas', '就绪', 'Ready'],
              ...(resource === 'statefulsets' ? [] : [['availableReplicas', '可用', 'Available']]),
            ]
  const extra =
    resource === 'statefulsets'
      ? [
          ['serviceName', 'Service', 'Service'],
          ['updateStrategy', '更新策略', 'Update strategy'],
          ['currentRevision', 'Current Revision', 'Current revision'],
          ['updateRevision', 'Update Revision', 'Update revision'],
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
      : []
  const conditions = Array.isArray(detail.conditions) ? detail.conditions : []
  return (
    <div className="soha-detail-stack">
      <Card
        className="soha-detail-card soha-controller-overview"
        title={zh ? `${title} ${resource === 'cronjobs' ? '调度' : '状态'}` : `${title} status`}
      >
        <dl className="soha-k8s-detail-summary">
          {fields.map(([field, cn, en]) => (
            <div key={field}>
              <dt>{zh ? cn : en}</dt>
              <dd>{value(field)}</dd>
            </div>
          ))}
        </dl>
        <Descriptions
          column={{ xs: 1, sm: 2 }}
          size="small"
          items={[
            ...extra.map(([field, cn, en]) => ({
              key: field,
              label: zh ? cn : en,
              span: field.endsWith('Revision') ? ('filled' as const) : 1,
              children:
                field.endsWith('Time') && typeof detail[field] === 'string'
                  ? formatDateTime(detail[field] as string)
                  : value(field),
            })),
            ...(resource === 'cronjobs'
              ? [
                  {
                    key: 'suspend',
                    label: zh ? '暂停' : 'Suspend',
                    children: <BooleanTag value={detail.suspend === true} />,
                  },
                ]
              : []),
            ...(selector.length
              ? [
                  {
                    key: 'selector',
                    label: 'Selector',
                    span: 'filled' as const,
                    children: (
                      <Space className="soha-controller-selector" wrap>
                        {selector.map(([key, val]) => (
                          <MetadataTag key={key} label={`${key}=${val}`} />
                        ))}
                      </Space>
                    ),
                  },
                ]
              : []),
          ]}
        />
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
    </div>
  )
}
