import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementIconButton, ManagementTableToolbar } from '@/components/management-list'
import { hasAllowedAction } from '@/features/auth'
import {
  capabilityActionTooltip,
  useClusterCapability,
} from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import {
  buildWorkloadDetailPath,
  includesSearch,
  normalizeSearchKeyword,
} from '@/features/platform/workloads-model'
import {
  renderWorkloadNameLink,
  useWorkloadListAIContext,
  useWorkloadTableDensity,
  WorkloadQueryPanel,
  WorkloadRefreshButton,
  workloadRowAIContext,
  WorkloadSearchInput,
  WorkloadTableEmpty,
} from '@/features/platform/workloads/shared/list-controls'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatAgeSeconds } from '@/utils/time'
import { DeleteOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Popconfirm } from 'antd'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { jobMutations } from './mutations'
import { jobQueries } from './queries'
import type { Job, JobTarget } from './types'

export function WorkloadsJobsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const jobsQuery = useQuery(jobQueries.list(toScopeKey(clusterId, namespace)))
  const visibleJobsQueryData = readableQueryData(jobsQuery)
  const removeMutation = usePlatformMutation(jobMutations.remove(queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = removeMutation.blocked
  const [searchKeyword, setSearchKeyword] = useState('')
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)
  const workloadMutationCapability = useClusterCapability('workload.mutations', localeCode)

  const jobs = visibleJobsQueryData ?? []
  const canShowActions =
    !workloadMutationCapability.disabled &&
    jobs.some((item) => hasAllowedAction(item.allowedActions, 'delete'))
  const filteredJobs = useMemo(
    () =>
      jobs.filter((item) =>
        includesSearch(
          [item.name, item.namespace, item.completionMode],
          normalizeSearchKeyword(searchKeyword),
        ),
      ),
    [jobs, searchKeyword],
  )

  useWorkloadListAIContext({
    clusterId,
    itemCount: jobs.length,
    kind: 'jobs',
    label: 'Jobs',
    namespace,
    searchKeyword,
  })

  const remove = (target: JobTarget) => removeMutation.mutate(target, {})

  const columns: TableColumnsType<Job> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      ellipsis: { showTitle: false },
      render: (name: string, record: Job) =>
        renderWorkloadNameLink(name, () =>
          navigate(buildWorkloadDetailPath('jobs', name, namespace, record.namespace, clusterId)),
        ),
    },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace', width: 160 },
    {
      title: localeCode === 'zh_CN' ? '完成数' : 'Completions',
      dataIndex: 'completions',
      width: 120,
    },
    {
      title: localeCode === 'zh_CN' ? '成功' : 'Succeeded',
      dataIndex: 'succeeded',
      width: 104,
    },
    { title: localeCode === 'zh_CN' ? '失败' : 'Failed', dataIndex: 'failed', width: 88 },
    { title: localeCode === 'zh_CN' ? '活跃' : 'Active', dataIndex: 'active', width: 88 },
    {
      title: localeCode === 'zh_CN' ? '模式' : 'Mode',
      dataIndex: 'completionMode',
      width: 140,
      render: (value: string) => value || '-',
    },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
    {
      title: '',
      dataIndex: 'name',
      key: 'actions',
      fixed: 'right',
      align: 'center',
      width: 52,
      render: (name: string, record: Job) => {
        if (!hasAllowedAction(record.allowedActions, 'delete')) return null
        const target = { scope: toScopeKey(clusterId, record.namespace), name }
        const pending =
          removeMutation.isPending &&
          removeMutation.variables?.name === name &&
          removeMutation.variables.scope.namespace === record.namespace
        const label = localeCode === 'zh_CN' ? '删除' : 'Delete'
        if (workloadMutationCapability.disabled) {
          return (
            <ManagementIconButton
              danger
              disabled
              icon={<DeleteOutlined />}
              aria-label={label}
              tooltip={capabilityActionTooltip(label, workloadMutationCapability)}
            />
          )
        }
        return (
          <Popconfirm
            title={localeCode === 'zh_CN' ? `确认删除 ${name}？` : `Delete ${name}?`}
            description={
              localeCode === 'zh_CN'
                ? '此操作不可恢复，删除后集群资源立即消失。'
                : 'This deletes the resource immediately and cannot be undone.'
            }
            okText={label}
            cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
            okButtonProps={{ disabled: platformWriteBlocked, danger: true, loading: pending }}
            placement="topRight"
            onConfirm={() => remove(target)}
          >
            <ManagementIconButton
              disabled={platformWriteBlocked}
              danger
              icon={<DeleteOutlined />}
              aria-label={label}
              loading={pending}
              tooltip={label}
            />
          </Popconfirm>
        )
      },
    },
  ]

  return (
    <div className="soha-page">
      {removeMutation.feedback}

      <WorkloadQueryPanel
        hasActiveFilters={Boolean(searchKeyword.trim())}
        localeCode={localeCode}
        onReset={() => setSearchKeyword('')}
      >
        <WorkloadSearchInput
          label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
          value={searchKeyword}
          onChange={setSearchKeyword}
          placeholder={
            localeCode === 'zh_CN' ? '搜索 Job / Namespace / Mode' : 'Search job / namespace / mode'
          }
        />
      </WorkloadQueryPanel>
      <KubernetesResourceTable
        resourceKey={'jobs'}
        resourceLabel={'Jobs'}
        allRows={jobs}
        filterKey={searchKeyword}
        error={jobsQuery.isEnabled === false ? null : jobsQuery.error}
        hasReadData={jobsQuery.data !== undefined}
        onRetry={jobsQuery.isEnabled === false ? undefined : () => jobsQuery.refetch()}
        className="soha-jobs-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={
          <ManagementTableToolbar>
            <WorkloadCreateEntry kind="Job" />
            {densityButton}
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={t('common.refresh', 'Refresh')}
              loading={jobsQuery.isFetching}
              onRefresh={() => void jobsQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        columns={canShowActions ? columns : columns.filter((column) => column.key !== 'actions')}
        dataSource={filteredJobs}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        onRow={(record: Job) => workloadRowAIContext('jobs', record, clusterId)}
        loading={jobsQuery.isLoading}
        localSorting
        empty={
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredJobs.length}
            localeCode={localeCode}
            resourceLabel="Jobs"
            totalCount={jobs.length}
          />
        }
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
    </div>
  )
}
