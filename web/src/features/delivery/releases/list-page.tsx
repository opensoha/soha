import { ArrowRightOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { ManagementIconButton } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { DeliveryTable } from '../delivery-table'
import { deliveryQueries } from '../queries'
import type { ReleaseRecord } from '../types'

type ColumnProps<T> = TableColumnsType<T>[number]

export function ReleasesPage() {
  const navigate = useNavigate()

  const releasesQuery = useQuery(deliveryQueries.releases.list())

  const columns: ColumnProps<ReleaseRecord>[] = [
    { title: '应用', dataIndex: 'applicationId' },
    { title: '集群', dataIndex: 'clusterId' },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: '部署目标', dataIndex: 'deploymentName' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (status: string) => <StatusTag value={status} />,
    },
    {
      ...tableColumnPresets.datetime,
      title: '部署时间',
      dataIndex: 'deployedAt',
      render: (value: string, record: ReleaseRecord) => formatDateTime(value || record.createdAt),
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: ReleaseRecord) => (
        <ManagementIconButton
          aria-label="查看发布详情"
          icon={<ArrowRightOutlined />}
          size="small"
          tooltip="查看详情"
          onClick={() => navigate(`/releases/${record.id}`)}
        />
      ),
    },
  ]

  return (
    <div className="soha-page">
      <DeliveryTable
        refreshing={releasesQuery.isFetching}
        onRefresh={() => void releasesQuery.refetch()}
        isError={releasesQuery.isError}
        error={releasesQuery.error}
        hasReadData={releasesQuery.data !== undefined}
        errorDescription="暂时无法读取发布记录。"
        onRetry={() => void releasesQuery.refetch()}
        localSorting
        columns={columns}
        dataSource={releasesQuery.data ?? []}
        rowKey="id"
        loading={releasesQuery.isLoading}
      />
    </div>
  )
}
