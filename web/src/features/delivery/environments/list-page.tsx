import { useMemo } from 'react'
import { ArrowRightOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { ManagementIconButton } from '@/components/management-list'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { DeliveryTable } from '../delivery-table'
import { deliveryQueries } from '../queries'
import { summarizeReleaseTargets } from '../release-targets'
import type { ApplicationEnvironment, WorkflowTemplate } from '../types'

type ColumnProps<T> = TableColumnsType<T>[number]

function applicationEnvironmentLabel(
  binding: Pick<ApplicationEnvironment, 'environmentKey' | 'environmentId'>,
) {
  return binding.environmentKey || binding.environmentId || '-'
}

export function ApplicationEnvironmentsPage() {
  const navigate = useNavigate()
  const bindingsQuery = useQuery(deliveryQueries.environments.list())
  const appsQuery = useQuery(deliveryQueries.applications.list())
  const appNameMap = useMemo(
    () => Object.fromEntries((appsQuery.data ?? []).map((item) => [item.id, item.name])),
    [appsQuery.data],
  )

  const columns: ColumnProps<ApplicationEnvironment>[] = [
    {
      title: '应用',
      dataIndex: 'applicationId',
      render: (value: string) => appNameMap[value] || value,
    },
    {
      title: '环境',
      dataIndex: 'environmentId',
      render: (_: string, record: ApplicationEnvironment) => applicationEnvironmentLabel(record),
    },
    { title: '策略', dataIndex: 'strategyProfileId', render: (value: string) => value || '-' },
    {
      title: '构建来源',
      dataIndex: 'buildPolicy',
      render: (value: ApplicationEnvironment['buildPolicy']) => value?.sourceId || '-',
    },
    {
      title: '动作',
      dataIndex: 'releasePolicy',
      render: (value: ApplicationEnvironment['releasePolicy']) => value?.actionKind || 'deploy',
    },
    {
      title: '发布流程模板',
      dataIndex: 'workflowTemplate',
      render: (_: WorkflowTemplate, record: ApplicationEnvironment) =>
        record.workflowTemplate?.name || record.workflowTemplateId || '-',
    },
    {
      title: '目标数',
      dataIndex: 'targets',
      render: (targets: ApplicationEnvironment['targets']) => (
        <span title={summarizeReleaseTargets(targets)}>
          {targets?.length ?? 0} · {summarizeReleaseTargets(targets)}
        </span>
      ),
    },
    {
      ...tableColumnPresets.datetime,
      title: '更新时间',
      dataIndex: 'updatedAt',
      render: (value: string) => formatDateTime(value),
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: ApplicationEnvironment) => (
        <ManagementIconButton
          aria-label="进入应用环境"
          icon={<ArrowRightOutlined />}
          size="small"
          tooltip="进入应用环境"
          onClick={() =>
            navigate(
              `/applications/${encodeURIComponent(record.applicationId)}?tab=services&applicationEnvironmentId=${encodeURIComponent(record.id)}`,
            )
          }
        />
      ),
    },
  ]

  return (
    <div className="soha-page">
      <DeliveryTable
        refreshing={bindingsQuery.isFetching}
        onRefresh={() => void bindingsQuery.refetch()}
        isError={bindingsQuery.isError}
        error={bindingsQuery.error}
        hasReadData={bindingsQuery.data !== undefined}
        errorDescription="暂时无法读取应用环境。"
        onRetry={() => void bindingsQuery.refetch()}
        localSorting
        columns={columns}
        dataSource={bindingsQuery.data ?? []}
        rowKey="id"
        loading={bindingsQuery.isLoading}
      />
    </div>
  )
}
