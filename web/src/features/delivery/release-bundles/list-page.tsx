import { useMemo } from 'react'
import { Alert, Space, Tag, Typography } from 'antd'
import { ArrowRightOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { ManagementIconButton } from '@/components/management-list'
import { OverviewMetricCard, type OverviewMetricItem } from '@/components/overview-visuals'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { DeliveryTable } from '@/features/delivery/delivery-table'
import {
  summarizeReleaseBundleArtifact,
  summarizeReleaseBundleStatus,
} from '@/features/delivery/delivery-status'
import { deliveryQueries } from '@/features/delivery/queries'
import type { ReleaseBundle } from '@/features/delivery/types'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { summarizeDeliveryGovernance } from '../workbench/governance'

const { Text } = Typography

export function ReleaseBundlesPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const focusedReleaseBundleId = searchParams.get('releaseBundleId')?.trim() ?? ''
  const bundlesQuery = useQuery(deliveryQueries.releaseBundles.list())
  const bundles = bundlesQuery.data ?? []
  const focusedBundle = focusedReleaseBundleId
    ? bundles.find((item) => item.id === focusedReleaseBundleId)
    : undefined
  const bundleSummary = useMemo(() => summarizeReleaseBundleStatus(bundles), [bundles])
  const tasksQuery = useQuery(deliveryQueries.executionTasks.list())
  const summaryMetrics: OverviewMetricItem[] = [
    {
      key: 'candidates',
      label: '候选版本',
      value: bundleSummary.total,
      helper: `${bundleSummary.ready} 个可验证 / 可推广`,
    },
    {
      key: 'blocked',
      label: '阻塞版本',
      value: bundleSummary.blocked,
      helper: '构建或发布失败',
      tone: bundleSummary.blocked > 0 ? 'danger' : 'success',
    },
    {
      key: 'artifacts',
      label: '交付物',
      value: bundleSummary.artifacts,
      helper: '镜像 / 包 / digest',
      tone: 'success',
    },
    {
      key: 'missing',
      label: '缺少交付物',
      value: bundleSummary.missingArtifacts,
      helper: '需要回填 artifact',
      tone: bundleSummary.missingArtifacts > 0 ? 'warning' : 'success',
    },
  ]

  return (
    <div className="soha-page">
      {focusedReleaseBundleId ? (
        <Alert
          showIcon
          title={focusedBundle ? `已定位版本包 ${focusedBundle.id}` : '版本包定位'}
          description={`releaseBundleId=${focusedReleaseBundleId}`}
          type={focusedBundle || bundlesQuery.isLoading ? 'info' : 'warning'}
        />
      ) : null}
      <div className="soha-overview-metric-grid">
        {summaryMetrics.map(({ key, ...item }) => (
          <OverviewMetricCard key={key} {...item} loading={bundlesQuery.isLoading} />
        ))}
      </div>
      <DeliveryTable
        rowKey="id"
        refreshing={bundlesQuery.isFetching}
        onRefresh={() => void bundlesQuery.refetch()}
        isError={bundlesQuery.isError}
        error={bundlesQuery.error}
        hasReadData={bundlesQuery.data !== undefined}
        errorDescription="暂时无法读取版本包记录。"
        onRetry={() => void bundlesQuery.refetch()}
        localSorting
        loading={bundlesQuery.isLoading}
        dataSource={bundles}
        columns={[
          {
            title: '版本',
            dataIndex: 'version',
            render: (value: string, record: ReleaseBundle) => (
              <Space orientation="vertical" size={0}>
                <Space size={6} wrap>
                  <Text strong>{value}</Text>
                  {record.id === focusedReleaseBundleId ? <Tag color="blue">已定位</Tag> : null}
                </Space>
                <Text type="secondary">{record.id}</Text>
              </Space>
            ),
          },
          { title: '应用', dataIndex: 'applicationId' },
          {
            title: '环境绑定',
            dataIndex: 'applicationEnvironmentId',
            render: (value: string) => value || '-',
          },
          { title: '来源', dataIndex: 'sourceType' },
          {
            title: '交付物',
            dataIndex: 'artifactRef',
            render: (_: unknown, record: ReleaseBundle) => summarizeReleaseBundleArtifact(record),
          },
          { title: '摘要', dataIndex: 'artifactDigest', render: (value: string) => value || '-' },
          {
            title: '状态',
            dataIndex: 'status',
            render: (value: string) => <StatusTag value={value} />,
          },
          {
            title: '治理门禁',
            dataIndex: 'id',
            render: (_: string, record: ReleaseBundle) => {
              const governance = summarizeDeliveryGovernance(record, tasksQuery.data ?? [])
              return (
                <Space size={4} wrap>
                  <StatusTag value={governance.label} />
                  {governance.approvalStatus ? (
                    <MetadataTag label={`审批:${governance.approvalStatus}`} tone="gold" />
                  ) : null}
                  {governance.rollbackTaskCount ? (
                    <MetadataTag label={`回滚:${governance.rollbackTaskCount}`} tone="blue" />
                  ) : null}
                </Space>
              )
            },
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
            render: (_: string, record: ReleaseBundle) => (
              <ManagementIconButton
                aria-label="查看版本包详情"
                icon={<ArrowRightOutlined />}
                size="small"
                tooltip="查看详情"
                onClick={() => navigate(`/delivery/release-bundles/${record.id}`)}
              />
            ),
          },
        ]}
      />
    </div>
  )
}
