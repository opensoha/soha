import { AdminTable } from '@/components/admin-table'
import { StatusTag } from '@/components/status-tag'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableProps } from 'antd'
import { Typography } from 'antd'
import '../observability-pages.css'
import { observabilityEventQueries } from './queries'
import type { EventStreamEntry } from './types'

const { Text } = Typography

export function EventsPage() {
  const eventsQuery = useQuery(observabilityEventQueries.list())

  const columns: TableProps<EventStreamEntry>['columns'] = [
    {
      ...tableColumnPresets.datetime,
      title: '发生时间',
      dataIndex: 'occurredAt',
      render: formatDateTime,
    },
    {
      title: '来源',
      dataIndex: 'source',
      width: 180,
      render: (value: string) => value || '-',
    },
    {
      title: '类别',
      dataIndex: 'category',
      width: 160,
      render: (value: string) => value || '-',
    },
    {
      ...tableColumnPresets.status,
      title: '严重度',
      dataIndex: 'severity',
      render: (value?: string) => <StatusTag value={value} />,
    },
    {
      title: '范围',
      dataIndex: 'namespace',
      width: 220,
      render: (value: string, record) =>
        [record.clusterId, value].filter(Boolean).join(' / ') || '-',
    },
    {
      title: '摘要',
      dataIndex: 'summary',
      ellipsis: true,
      render: (value: string) => value || '-',
    },
    {
      title: 'Payload',
      dataIndex: 'payload',
      ellipsis: true,
      render: (value: EventStreamEntry['payload']) => {
        if (!value || Object.keys(value).length === 0) return '-'
        return <Text code>{JSON.stringify(value)}</Text>
      },
    },
  ]

  return (
    <div className="soha-page">
      <h1 className="soha-observability-page-heading">事件流</h1>
      <AdminTable
        enableDensity
        error={eventsQuery.error}
        hasReadData={eventsQuery.data !== undefined}
        refreshing={eventsQuery.isFetching}
        onRefresh={() => void eventsQuery.refetch()}
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={eventsQuery.data ?? []}
        rowKey="id"
        loading={eventsQuery.isLoading}
        pageSize={50}
        scroll={{ x: 'max-content' }}
      />
    </div>
  )
}
