import { ManagementDataPage } from '@/components/management-data-page'
import { ManagementState, ManagementTableToolbar } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { readableQueryData } from '@/utils/query-feedback'
import { AppstoreAddOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Space } from 'antd'
import { useNavigate } from 'react-router-dom'
import { workbenchQueries } from '../workbench/queries'
import type { WorkbenchAgentProvider } from '../workbench/types'

export function AgentProvidersPage() {
  const navigate = useNavigate()
  const catalogQuery = useQuery(workbenchQueries.catalog())
  const providers = readableQueryData(catalogQuery)?.data.agentProviders ?? []

  const columns: TableColumnsType<WorkbenchAgentProvider> = [
    { title: 'Provider', dataIndex: 'name', key: 'name' },
    { title: '类型', dataIndex: 'kind', key: 'kind', width: 150 },
    {
      title: '状态',
      key: 'status',
      width: 140,
      render: (_, record) => (
        <StatusTag
          value={record.runtimeStatus?.state || (record.enabled ? 'enabled' : 'disabled')}
        />
      ),
    },
    {
      title: '能力',
      dataIndex: 'capabilities',
      key: 'capabilities',
      render: (values: string[] | undefined) =>
        values?.length ? (
          <Space size={4} wrap>
            {values.map((value) => (
              <MetadataTag key={value} label={value} />
            ))}
          </Space>
        ) : (
          '-'
        ),
    },
    {
      title: '运行',
      key: 'runs',
      width: 150,
      render: (_, record) =>
        record.runtimeStatus
          ? `${record.runtimeStatus.runningRuns ?? 0} 运行 / ${record.runtimeStatus.queuedRuns ?? 0} 排队`
          : '运行状态未读取',
    },
  ]

  return (
    <ManagementDataPage
      table={{
        title: 'Agent Providers',
        columnSettingIconOnly: true,
        columnSettingPlacement: 'header',
        headerExtra: (
          <ManagementTableToolbar>
            <Button
              icon={<ReloadOutlined />}
              loading={catalogQuery.isFetching}
              onClick={() => void catalogQuery.refetch()}
            >
              刷新
            </Button>
            <Button
              type="primary"
              icon={<AppstoreAddOutlined />}
              onClick={() => navigate('/plugins/marketplace')}
            >
              从插件市场安装
            </Button>
          </ManagementTableToolbar>
        ),
        columns,
        dataSource: providers,
        loading: catalogQuery.isLoading,
        error: catalogQuery.error,
        hasReadData: catalogQuery.data !== undefined,
        onRetry: () => void catalogQuery.refetch(),
        rowKey: 'id',
        empty: catalogQuery.isError ? (
          <ManagementState kind="error" title="Provider Catalog 加载失败" />
        ) : (
          <ManagementState
            title="暂无 Agent Provider"
            description="从插件市场安装并启用 Provider 后，待运行时完成同步会显示在这里。"
          />
        ),
      }}
    />
  )
}
