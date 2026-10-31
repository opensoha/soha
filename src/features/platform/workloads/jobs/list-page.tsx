import { WorkloadRefresh, useWorkloadRefresh } from '../shared/workload-refresh'
import {
  useKubernetesResourceActions,
  kubernetesDeleteConfirmation,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementTableToolbar } from '@/components/management-list'
import { hasAllowedAction } from '@/features/auth'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
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
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { jobMutations } from './mutations'
import { jobQueries } from './queries'
import type { Job } from './types'

export function WorkloadsJobsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const listRefresh = useWorkloadRefresh()
  const jobsQuery = useQuery({
    ...jobQueries.list(toScopeKey(clusterId, namespace)),
    refetchInterval: listRefresh.interval,
    refetchIntervalInBackground: false,
  })
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

  const resourceActions = useKubernetesResourceActions({
    resourceKey: 'jobs',
    resourceLabel: 'Job',
    records: jobs,
    getName: (record) => record.name,
    getKey: (record) => `${record.namespace}/${record.name}`,
    actions: (record) => {
      const target = { name: record.name, scope: toScopeKey(clusterId, record.namespace) }
      return [
        ...(hasAllowedAction(record.allowedActions, 'delete')
          ? [
              {
                key: 'delete',
                label: localeCode === 'zh_CN' ? '删除' : 'Delete',
                icon: <DeleteOutlined aria-hidden />,
                danger: true,
                disabled: platformWriteBlocked || workloadMutationCapability.disabled,
                reason: workloadMutationCapability.reason,
                loading:
                  removeMutation.isPending &&
                  removeMutation.variables?.scope.clusterId === clusterId &&
                  removeMutation.variables?.name === record.name &&
                  removeMutation.variables.scope.namespace === record.namespace,
                feedback: removeMutation.feedback,
                prepare: () => removeMutation.reset(),
                confirm: kubernetesDeleteConfirmation(record, localeCode),
                run: () => removeMutation.mutateAsync(target),
              },
            ]
          : []),
      ]
    },
  })

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
    { title: localeCode === 'zh_CN' ? '活跃' : 'Active', dataIndex: 'active', width: 88 },
    { title: localeCode === 'zh_CN' ? '失败' : 'Failed', dataIndex: 'failed', width: 88 },
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
    resourceActions.column,
  ]

  return (
    <div className="soha-page">
      {resourceActions.isConfirming ? null : removeMutation.feedback}

      {resourceActions.confirmation}
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
            <WorkloadRefresh
              compact
              settings={listRefresh}
              queries={[jobsQuery]}
              resourceLabel="Jobs"
              disabled={!clusterId}
              label={t('common.refresh', 'Refresh')}
              onRefresh={() => void jobsQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        columns={
          resourceActions.hasActions
            ? columns
            : columns.filter((column) => column.key !== 'actions')
        }
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
