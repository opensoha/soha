import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useResourceListViewport } from '@/features/platform/shared/use-resource-list-viewport'
import { buildWorkloadDetailPath } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { Button, Card, Input, Segmented, Typography, type TableColumnsType } from 'antd'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import {
  formatJobDuration,
  jobDuration,
  jobEndTime,
  jobState,
  jobStateLabel,
} from '../shared/job-execution'
import type { CronJobChildJob } from './types'

export function CronJobJobsTable({
  jobs,
  scope,
  compact = false,
  onShowAll,
}: {
  jobs?: CronJobChildJob[]
  scope: ScopeKey
  compact?: boolean
  onShowAll?: () => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const [search, setSearch] = useState(''),
    [filter, setFilter] = useState('all')
  const rows = compact
    ? jobs?.slice(0, 3)
    : jobs?.filter(
        (j) =>
          j.name.toLowerCase().includes(search.trim().toLowerCase()) &&
          (filter === 'all' || jobState(j) === filter),
      )
  const rowKey = (j: CronJobChildJob) => `${j.namespace}/${j.name}`
  const viewport = useResourceListViewport(
    rows || [],
    rowKey,
    JSON.stringify([scope.clusterId, scope.namespace, search, filter]),
  )
  const date = (v?: string) => (v ? formatDateTime(v) : '—')
  const columns: TableColumnsType<CronJobChildJob> = [
    {
      title: 'Job',
      key: 'name',
      dataIndex: 'name',
      width: compact ? 280 : 300,
      fixed: 'left',
      ellipsis: true,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (name: string, j) =>
        hasPermission(permissions, 'platform.workloads.jobs.view') ? (
          <Link
            title={name}
            to={buildWorkloadDetailPath(
              'jobs',
              name,
              scope.namespace,
              j.namespace,
              scope.clusterId,
            )}
          >
            {name}
          </Link>
        ) : (
          name
        ),
    },
    {
      title: zh ? '执行状态' : 'Execution status',
      key: 'state',
      width: 150,
      render: (_, j) => (
        <StatusTag
          value={
            jobState(j) === 'completed'
              ? 'completed'
              : jobState(j) === 'failed'
                ? 'failed'
                : jobState(j) === 'running'
                  ? 'progressing'
                  : jobState(j) === 'failures-recorded'
                    ? 'warning'
                    : jobState(j)
          }
          label={jobStateLabel(jobState(j), zh)}
        />
      ),
    },
    ...(!compact
      ? [
          {
            title: zh ? '开始' : 'Started',
            key: 'startTime',
            dataIndex: 'startTime',
            width: 170,
            sorter: (a: CronJobChildJob, b: CronJobChildJob) =>
              (Date.parse(a.startTime || '') || 0) - (Date.parse(b.startTime || '') || 0),
            render: date,
          },
          {
            title: zh ? '完成 / 终态' : 'Finished / terminal',
            key: 'endTime',
            width: 170,
            render: (_: unknown, j: CronJobChildJob) => date(jobEndTime(j)),
          },
        ]
      : []),
    {
      title: zh ? '耗时' : 'Elapsed',
      key: 'duration',
      width: 100,
      sorter: (a, b) => (jobDuration(a) ?? -1) - (jobDuration(b) ?? -1),
      render: (_, j) => formatJobDuration(jobDuration(j)),
    },
    ...(!compact
      ? [
          {
            title: zh ? '成功 Pods' : 'Succeeded Pods',
            dataIndex: 'succeeded',
            key: 'succeeded',
            width: 110,
          },
          {
            title: zh ? '失败 Pods' : 'Failed Pods',
            dataIndex: 'failed',
            key: 'failed',
            width: 110,
          },
          {
            title: zh ? '活跃 Pods' : 'Active Pods',
            dataIndex: 'active',
            key: 'active',
            width: 110,
          },
        ]
      : []),
  ]
  return (
    <Card
      className="soha-detail-card soha-cronjob-jobs"
      title={
        <>
          {zh ? '保留的 Jobs' : 'Retained Jobs'}{' '}
          <Typography.Text type="secondary">
            {compact ? (jobs?.length ?? '—') : `${rows?.length ?? '—'} / ${jobs?.length ?? '—'}`}
          </Typography.Text>
        </>
      }
      extra={
        compact && (
          <Button type="link" onClick={onShowAll}>
            {zh ? '查看全部' : 'View all'}
          </Button>
        )
      }
    >
      {!compact && (
        <div className="soha-k8s-detail-toolbar">
          <Input
            aria-label={zh ? '搜索 Job' : 'Search Jobs'}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            allowClear
            placeholder={zh ? '搜索执行名称' : 'Search executions'}
          />
          <Segmented
            aria-label={zh ? '执行筛选' : 'Execution filter'}
            value={filter}
            onChange={(v) => setFilter(String(v))}
            options={[
              { value: 'all', label: zh ? '全部' : 'All' },
              { value: 'running', label: zh ? '执行中' : 'Executing' },
              { value: 'completed', label: zh ? '已完成' : 'Completed' },
              { value: 'failed', label: zh ? '失败' : 'Failed' },
            ]}
          />
        </div>
      )}
      <AdminTable
        rowKey={rowKey}
        columns={columns}
        dataSource={rows || []}
        pagination={false}
        enableColumnSelection={false}
        tableRef={viewport.tableRef}
        onScroll={viewport.onScroll}
        onChange={(_, __, ___, extra) => {
          if (extra.action === 'sort') viewport.resetPosition()
        }}
        virtual={!compact && (rows?.length || 0) > 20}
        scroll={{
          x: compact ? 530 : 1320,
          ...(!compact ? { y: 560 } : {}),
          scrollToFirstRowOnChange: false,
        }}
        empty={
          <ManagementState
            compact
            title={
              jobs === undefined
                ? zh
                  ? '未返回执行记录'
                  : 'Execution records unavailable'
                : jobs.length
                  ? zh
                    ? '没有匹配的 Job'
                    : 'No matching Job'
                  : zh
                    ? '暂无保留的 Job，可能尚未调度或已按保留策略清理'
                    : 'No retained Jobs: not scheduled yet or already cleaned up'
            }
          />
        }
      />
      {!compact && (
        <Typography.Paragraph type="secondary">
          {zh
            ? '仅当前 CronJob UID 所属、仍保留的执行；失败 Pods 数量不代表 Job 终态失败。进入 Job 查看 Pods 和日志。'
            : 'Only retained executions owned by this CronJob UID. Failed Pod counts are not a terminal Job failure. Open a Job for Pods and logs.'}
        </Typography.Paragraph>
      )}
    </Card>
  )
}
