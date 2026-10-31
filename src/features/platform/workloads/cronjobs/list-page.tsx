import { WorkloadRefresh, useWorkloadRefresh } from '../shared/workload-refresh'
import {
  useKubernetesResourceActions,
  kubernetesDeleteConfirmation,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementTableToolbar } from '@/components/management-list'
import { BooleanTag } from '@/components/status-tag'
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
import { WorkloadQuickEditModal } from '@/features/platform/workloads/shared/workload-quick-edit-modal'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatAgeSeconds, formatRelativeTime } from '@/utils/time'
import {
  DeleteOutlined,
  FormOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { cronJobMutations } from './mutations'
import { cronJobQueries } from './queries'
import type { CronJob } from './types'

export function WorkloadsCronJobsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const listRefresh = useWorkloadRefresh()
  const cronJobsQuery = useQuery({
    ...cronJobQueries.list(toScopeKey(clusterId, namespace)),
    refetchInterval: listRefresh.interval,
    refetchIntervalInBackground: false,
  })
  const visibleCronJobsQueryData = readableQueryData(cronJobsQuery)
  const suspendMutation = usePlatformMutation(cronJobMutations.suspend(queryClient), {
    label: ['调整定时任务', 'Change CronJob'],
    target: platformResourceTarget,
  })
  const removeMutation = usePlatformMutation(cronJobMutations.remove(queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = suspendMutation.blocked || removeMutation.blocked
  const [searchKeyword, setSearchKeyword] = useState('')
  const [editTarget, setEditTarget] = useState<CronJob | null>(null)
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)
  const workloadMutationCapability = useClusterCapability('workload.mutations', localeCode)
  const yamlApplyCapability = useClusterCapability('resource.yaml.apply', localeCode)

  const cronJobs = visibleCronJobsQueryData ?? []
  const filteredCronJobs = useMemo(
    () =>
      cronJobs.filter((item) =>
        includesSearch(
          [item.name, item.namespace, item.schedule],
          normalizeSearchKeyword(searchKeyword),
        ),
      ),
    [cronJobs, searchKeyword],
  )

  useWorkloadListAIContext({
    clusterId,
    itemCount: cronJobs.length,
    kind: 'cronjobs',
    label: 'CronJobs',
    namespace,
    searchKeyword,
  })

  useEffect(() => setEditTarget(null), [clusterId, namespace])
  const resourceActions = useKubernetesResourceActions({
    resourceKey: 'cronjobs',
    resourceLabel: 'CronJob',
    records: cronJobs,
    getName: (record) => record.name,
    getKey: (record) => `${record.namespace}/${record.name}`,
    actions: (record) => {
      const target = { name: record.name, scope: toScopeKey(clusterId, record.namespace) }
      return [
        ...(hasAllowedAction(record.allowedActions, 'update')
          ? [
              {
                key: 'update',
                label: localeCode === 'zh_CN' ? '编辑' : 'Edit',
                icon: <FormOutlined aria-hidden />,
                disabled: platformWriteBlocked || yamlApplyCapability.disabled,
                reason: yamlApplyCapability.reason,
                run: () => setEditTarget(record),
              },
            ]
          : []),
        ...(hasAllowedAction(record.allowedActions, 'suspend')
          ? [
              {
                key: record.suspend ? 'resume' : 'suspend',
                label: record.suspend
                  ? localeCode === 'zh_CN'
                    ? '恢复'
                    : 'Resume'
                  : localeCode === 'zh_CN'
                    ? '暂停'
                    : 'Suspend',
                icon: record.suspend ? (
                  <PlayCircleOutlined aria-hidden />
                ) : (
                  <PauseCircleOutlined aria-hidden />
                ),
                disabled: platformWriteBlocked || workloadMutationCapability.disabled,
                reason: workloadMutationCapability.reason,
                loading:
                  suspendMutation.isPending &&
                  suspendMutation.variables?.scope.clusterId === clusterId &&
                  suspendMutation.variables?.name === record.name &&
                  suspendMutation.variables.scope.namespace === record.namespace,
                feedback: suspendMutation.feedback,
                prepare: () => suspendMutation.reset(),
                confirm: {
                  title: record.suspend
                    ? localeCode === 'zh_CN'
                      ? `恢复 ${record.name}？`
                      : `Resume ${record.name}?`
                    : localeCode === 'zh_CN'
                      ? `暂停 ${record.name}？`
                      : `Suspend ${record.name}?`,
                  description: `${record.namespace} / ${record.name}`,
                  okText: localeCode === 'zh_CN' ? '确认' : 'OK',
                },
                run: () => suspendMutation.mutateAsync({ ...target, suspend: !record.suspend }),
              },
            ]
          : []),
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

  const columns: TableColumnsType<CronJob> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      ellipsis: { showTitle: false },
      render: (name: string, record: CronJob) =>
        renderWorkloadNameLink(name, () =>
          navigate(
            buildWorkloadDetailPath('cronjobs', name, namespace, record.namespace, clusterId),
          ),
        ),
    },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace', width: 160 },
    {
      title: localeCode === 'zh_CN' ? '调度计划' : 'Schedule',
      dataIndex: 'schedule',
      width: 180,
    },
    {
      ...tableColumnPresets.status,
      title: localeCode === 'zh_CN' ? '暂停' : 'Suspend',
      dataIndex: 'suspend',
      width: 128,
      render: (suspend: boolean) => (
        <BooleanTag
          value={suspend}
          trueLabel={localeCode === 'zh_CN' ? '是' : 'Yes'}
          falseLabel={localeCode === 'zh_CN' ? '否' : 'No'}
          trueColor="orange"
          falseColor="green"
        />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '活跃' : 'Active',
      dataIndex: 'activeJobs',
      width: 88,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '上次调度' : 'Last Schedule',
      dataIndex: 'lastScheduleTime',
      width: 140,
      render: (value: string) => (value ? formatRelativeTime(value) : '-'),
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
      {resourceActions.isConfirming ? null : suspendMutation.feedback}
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
            localeCode === 'zh_CN'
              ? '搜索 CronJob / Namespace / Schedule'
              : 'Search cron job / namespace / schedule'
          }
        />
      </WorkloadQueryPanel>
      <KubernetesResourceTable
        resourceKey={'cronjobs'}
        resourceLabel={'CronJobs'}
        allRows={cronJobs}
        filterKey={searchKeyword}
        error={cronJobsQuery.isEnabled === false ? null : cronJobsQuery.error}
        hasReadData={cronJobsQuery.data !== undefined}
        onRetry={cronJobsQuery.isEnabled === false ? undefined : () => cronJobsQuery.refetch()}
        className="soha-cronjobs-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={
          <ManagementTableToolbar>
            <WorkloadCreateEntry kind="CronJob" />
            {densityButton}
            <WorkloadRefresh
              compact
              settings={listRefresh}
              queries={[cronJobsQuery]}
              resourceLabel="CronJobs"
              disabled={!clusterId}
              label={t('common.refresh', 'Refresh')}
              onRefresh={() => void cronJobsQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        columns={
          resourceActions.hasActions
            ? columns
            : columns.filter((column) => column.key !== 'actions')
        }
        dataSource={filteredCronJobs}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        onRow={(record: CronJob) => workloadRowAIContext('cronjobs', record, clusterId)}
        loading={cronJobsQuery.isLoading}
        localSorting
        empty={
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredCronJobs.length}
            localeCode={localeCode}
            resourceLabel="CronJobs"
            totalCount={cronJobs.length}
          />
        }
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
      {editTarget ? (
        <WorkloadQuickEditModal
          kind="cronjobs"
          name={editTarget.name}
          namespace={editTarget.namespace}
          onClose={() => setEditTarget(null)}
        />
      ) : null}
    </div>
  )
}
