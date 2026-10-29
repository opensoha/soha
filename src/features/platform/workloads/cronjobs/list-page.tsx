import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementIconButton, ManagementTableToolbar } from '@/components/management-list'
import { BooleanTag } from '@/components/status-tag'
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
import { Popconfirm, Space } from 'antd'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { cronJobMutations } from './mutations'
import { cronJobQueries } from './queries'
import type { CronJob, CronJobTarget } from './types'

export function WorkloadsCronJobsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const cronJobsQuery = useQuery(cronJobQueries.list(toScopeKey(clusterId, namespace)))
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
  const canShowActions = cronJobs.some(
    (item) =>
      (!yamlApplyCapability.disabled && hasAllowedAction(item.allowedActions, 'update')) ||
      (!workloadMutationCapability.disabled &&
        (hasAllowedAction(item.allowedActions, 'suspend') ||
          hasAllowedAction(item.allowedActions, 'delete'))),
  )
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

  const targetFor = (name: string, targetNamespace: string): CronJobTarget => ({
    scope: toScopeKey(clusterId, targetNamespace),
    name,
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
    {
      title: '',
      dataIndex: 'name',
      key: 'actions',
      fixed: 'right',
      align: 'center',
      width: 96,
      render: (name: string, record: CronJob) => {
        const canSuspend = hasAllowedAction(record.allowedActions, 'suspend')
        const canEdit = hasAllowedAction(record.allowedActions, 'update')
        const canDelete = hasAllowedAction(record.allowedActions, 'delete')
        if (!canEdit && !canSuspend && !canDelete) return null

        const target = targetFor(name, record.namespace)
        const actionLabel = record.suspend
          ? localeCode === 'zh_CN'
            ? '恢复'
            : 'Resume'
          : localeCode === 'zh_CN'
            ? '暂停'
            : 'Suspend'
        const suspendPending =
          suspendMutation.isPending &&
          suspendMutation.variables?.name === name &&
          suspendMutation.variables.scope.namespace === record.namespace
        const removePending =
          removeMutation.isPending &&
          removeMutation.variables?.name === name &&
          removeMutation.variables.scope.namespace === record.namespace
        const deleteLabel = localeCode === 'zh_CN' ? '删除' : 'Delete'

        return (
          <Space size={4} className="soha-deployment-action-cell">
            {canEdit ? (
              <ManagementIconButton
                icon={<FormOutlined />}
                aria-label={`${localeCode === 'zh_CN' ? '编辑' : 'Edit'} ${name}`}
                disabled={platformWriteBlocked || yamlApplyCapability.disabled}
                tooltip={capabilityActionTooltip(
                  localeCode === 'zh_CN' ? '编辑' : 'Edit',
                  yamlApplyCapability,
                )}
                onClick={() => setEditTarget(record)}
              />
            ) : null}
            {canSuspend ? (
              <Popconfirm
                okButtonProps={{ disabled: platformWriteBlocked }}
                title={
                  record.suspend
                    ? localeCode === 'zh_CN'
                      ? `恢复 ${name}？`
                      : `Resume ${name}?`
                    : localeCode === 'zh_CN'
                      ? `暂停 ${name}？`
                      : `Suspend ${name}?`
                }
                okText={localeCode === 'zh_CN' ? '确认' : 'OK'}
                cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
                onConfirm={() =>
                  suspendMutation.mutate({ ...target, suspend: !record.suspend }, {})
                }
              >
                <ManagementIconButton
                  icon={record.suspend ? <PlayCircleOutlined /> : <PauseCircleOutlined />}
                  aria-label={actionLabel}
                  loading={suspendPending}
                  tooltip={actionLabel}
                />
              </Popconfirm>
            ) : null}
            {canDelete && workloadMutationCapability.disabled ? (
              <ManagementIconButton
                danger
                disabled
                icon={<DeleteOutlined />}
                aria-label={deleteLabel}
                tooltip={capabilityActionTooltip(deleteLabel, workloadMutationCapability)}
              />
            ) : null}
            {canDelete && !workloadMutationCapability.disabled ? (
              <Popconfirm
                title={localeCode === 'zh_CN' ? `确认删除 ${name}？` : `Delete ${name}?`}
                description={
                  localeCode === 'zh_CN'
                    ? '此操作不可恢复，删除后集群资源立即消失。'
                    : 'This deletes the resource immediately and cannot be undone.'
                }
                okText={deleteLabel}
                cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
                okButtonProps={{
                  disabled: platformWriteBlocked,
                  danger: true,
                  loading: removePending,
                }}
                placement="topRight"
                onConfirm={() => removeMutation.mutate(target, {})}
              >
                <ManagementIconButton
                  disabled={platformWriteBlocked}
                  danger
                  icon={<DeleteOutlined />}
                  aria-label={deleteLabel}
                  loading={removePending}
                  tooltip={deleteLabel}
                />
              </Popconfirm>
            ) : null}
          </Space>
        )
      },
    },
  ]

  return (
    <div className="soha-page">
      {suspendMutation.feedback}
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
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={t('common.refresh', 'Refresh')}
              loading={cronJobsQuery.isFetching}
              onRefresh={() => void cronJobsQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        columns={canShowActions ? columns : columns.filter((column) => column.key !== 'actions')}
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
