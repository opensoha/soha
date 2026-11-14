import '../observability-pages.css'
import type { ObservabilityProviderDefinition } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Alert, Flex, Typography } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import { observabilityProviderQueries } from '../provider-queries'

const { Text } = Typography

const signalLabels: Record<string, string> = {
  logs: 'Logs',
  metrics: 'Metrics',
  traces: 'Traces',
  profiles: 'Profiles',
}

export function providerSignalCounts(providers: ObservabilityProviderDefinition[]) {
  return providers.reduce<Record<string, number>>((counts, provider) => {
    provider.signals.forEach((signal) => {
      counts[signal] = (counts[signal] ?? 0) + 1
    })
    return counts
  }, {})
}

export function ObservabilityProvidersPage() {
  const providersQuery = useQuery(observabilityProviderQueries.providers())
  const columns: TableColumnsType<ObservabilityProviderDefinition> = [
    {
      title: 'Provider',
      dataIndex: 'displayName',
      key: 'displayName',
      render: (_, item) => (
        <Flex vertical gap={2}>
          <Text strong>{item.displayName}</Text>
          <Text code type="secondary">
            {item.providerKey}
          </Text>
        </Flex>
      ),
    },
    {
      title: '信号',
      dataIndex: 'signals',
      key: 'signals',
      render: (signals: string[]) => (
        <Flex gap={4} wrap>
          {signals.map((signal) => (
            <MetadataTag key={signal} label={signalLabels[signal] ?? signal} />
          ))}
        </Flex>
      ),
    },
    {
      title: '能力',
      dataIndex: 'capabilities',
      key: 'capabilities',
      render: (capabilities: string[]) => (
        <Flex gap={4} wrap>
          {capabilities.map((capability) => (
            <MetadataTag key={capability} label={capability} />
          ))}
        </Flex>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (_, item) => (
        <span title={item.statusReason || undefined}>
          <StatusTag value={item.status} />
        </span>
      ),
    },
    {
      title: '运行验证',
      key: 'runtimeStatus',
      width: 240,
      render: (_, item) => (
        <Flex vertical gap={2}>
          <Flex gap={4} wrap>
            <StatusTag value={item.runtimeStatus ?? 'unknown'} />
            {item.configured === undefined ? null : (
              <MetadataTag
                label={item.configured ? '已配置数据源' : '未配置数据源'}
                tone={item.configured ? 'blue' : 'orange'}
              />
            )}
          </Flex>
          {item.runtimeStatusReason ? (
            <Text type="secondary">{item.runtimeStatusReason}</Text>
          ) : null}
          {item.lastValidatedAt ? (
            <Text type="secondary">{formatDateTime(item.lastValidatedAt)}</Text>
          ) : null}
        </Flex>
      ),
    },
    {
      title: '来源',
      key: 'source',
      width: 150,
      render: (_, item) => (
        <MetadataTag label={item.builtIn ? '内置' : '插件'} tone={item.builtIn ? 'blue' : 'cyan'} />
      ),
    },
    {
      title: '协议',
      dataIndex: 'protocolVersion',
      key: 'protocolVersion',
      width: 120,
    },
  ]

  return (
    <div className="soha-page">
      <h1 className="soha-observability-page-heading">Provider</h1>
      <Flex vertical gap={12}>
        <Alert
          showIcon
          type="info"
          title="OTel 管线细分状态暂不可用"
          description="当前只展示 Provider 与已配置数据源的连接验证；instrumentation、receiver、processor、exporter、freshness 仍需 Collector 自监控证据，Collector 进程运行不代表端到端健康。"
        />
        <AdminTable
          enableDensity
          error={providersQuery.error}
          hasReadData={providersQuery.data !== undefined}
          refreshing={providersQuery.isFetching}
          onRefresh={() => void providersQuery.refetch()}
          columnSettingIconOnly
          columnSettingPlacement="header"
          columns={columns}
          dataSource={providersQuery.data ?? []}
          empty={<ManagementState bordered={false} compact description="暂无 Provider" />}
          loading={providersQuery.isLoading}
          pageSize={20}
          rowKey="providerKey"
          shellClassName="soha-management-table-shell"
          scroll={{ x: 'max-content' }}
        />
      </Flex>
    </div>
  )
}
