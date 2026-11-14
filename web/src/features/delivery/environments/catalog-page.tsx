import { useMemo } from 'react'
import { Space, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { DeliveryTable } from '../delivery-table'
import { deliveryQueries } from '../queries'
import type { DeliveryEnvironment } from '../types'

const { Text } = Typography

export function EnvironmentCatalogPage() {
  const environmentsQuery = useQuery(deliveryQueries.environmentCatalog.list())
  const bindingsQuery = useQuery(deliveryQueries.environments.list())
  const applicationsQuery = useQuery(deliveryQueries.applications.list())
  const usageByEnvironment = useMemo(() => {
    const applicationNames = new Map(
      (readableQueryData(applicationsQuery) ?? []).map((application) => [
        application.id,
        application.name,
      ]),
    )
    const usage = new Map<string, Set<string>>()
    for (const binding of readableQueryData(bindingsQuery) ?? []) {
      const names = usage.get(binding.environmentId) ?? new Set<string>()
      names.add(applicationNames.get(binding.applicationId) || binding.applicationId)
      usage.set(binding.environmentId, names)
    }
    return usage
  }, [applicationsQuery.data, applicationsQuery.error, bindingsQuery.data, bindingsQuery.error])

  const columns: TableColumnsType<DeliveryEnvironment> = [
    {
      title: '环境',
      dataIndex: 'name',
      render: (name: string, item) => (
        <Space orientation="vertical" size={0}>
          <Text strong>{name}</Text>
          <Text type="secondary">{item.key}</Text>
        </Space>
      ),
    },
    {
      title: '阶段',
      dataIndex: 'stageLevel',
      render: (stageLevel: number, item) => item.tier || `L${stageLevel}`,
    },
    {
      title: '类型',
      dataIndex: 'isProduction',
      render: (isProduction: boolean) => (
        <StatusTag
          value={isProduction ? 'warning' : 'default'}
          label={isProduction ? '生产' : '非生产'}
        />
      ),
    },
    {
      title: '审批',
      dataIndex: 'requiresApproval',
      render: (required: boolean) => (required ? '需要审批' : '无需审批'),
    },
    {
      title: '应用使用',
      dataIndex: 'id',
      render: (id: string) => {
        if (bindingsQuery.isLoading || applicationsQuery.isLoading)
          return <Text type="secondary">读取使用关系…</Text>
        if (!readableQueryData(bindingsQuery) || !readableQueryData(applicationsQuery))
          return <Text type="secondary">使用关系不可用</Text>
        const names = [...(usageByEnvironment.get(id) ?? [])]
        return names.length > 0 ? (
          <Space wrap>
            <MetadataTag label={`${names.length} 个应用`} />
            <Text>{names.join('、')}</Text>
          </Space>
        ) : (
          '-'
        )
      },
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      render: (enabled: boolean) => <StatusTag value={enabled ? 'enabled' : 'disabled'} />,
    },
    {
      ...tableColumnPresets.datetime,
      title: '更新时间',
      dataIndex: 'updatedAt',
      render: (value: string) => formatDateTime(value),
    },
  ]

  return (
    <div className="soha-page">
      <ReadFeedback query={bindingsQuery} label="环境绑定关系" />
      <ReadFeedback query={applicationsQuery} label="应用名称" />
      <DeliveryTable
        pagination={false}
        columns={columns}
        dataSource={environmentsQuery.data ?? []}
        rowKey="id"
        loading={environmentsQuery.isLoading}
        isError={environmentsQuery.isError}
        error={environmentsQuery.error}
        hasReadData={environmentsQuery.data !== undefined}
        errorDescription="暂时无法读取环境目录。"
        onRetry={() => void environmentsQuery.refetch()}
        localSorting
        refreshing={environmentsQuery.isFetching}
        onRefresh={() => void environmentsQuery.refetch()}
      />
    </div>
  )
}
