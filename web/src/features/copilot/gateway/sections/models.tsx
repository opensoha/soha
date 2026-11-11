import { PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { Button, DatePicker, Descriptions, Input, Select, Space, Tabs } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
  ManagementToolbarSearch,
} from '@/components/management-list'
import type {
  GatewayTabKey,
  GatewayTimeRangeValue,
  LLMCallLog,
  LLMModelRoute,
  LLMUpstream,
  ModelCallFilterState,
} from '../types'
import {
  gatewayTimeRangeQuery,
  relayCacheStatusOptions,
  relayCallStatusOptions,
  relayEndpointOptions,
  relayProviderKindOptions,
  relayUpstreamStatusOptions,
} from '../types'

const { RangePicker } = DatePicker

export interface GatewayRelaySectionProps {
  activeTab: GatewayTabKey
  onTabChange: (tab: GatewayTabKey) => void
  metrics: {
    requests: string
    successRate: string
    failure: string
    ttfb: string
    ttft: string
    duration: string
    tokensPerSecond: string
    cache: string
  }
  rankingColumns: TableColumnsType<{ key: string; count: number }>
  ranking: Array<{ key: string; count: number }>
  metricsLoading: boolean
  recentErrors: LLMCallLog[]
  modelCallColumns: TableColumnsType<LLMCallLog>
  modelCalls: LLMCallLog[]
  modelCallsLoading: boolean
  modelCallsFetching: boolean
  modelCallFilters: ModelCallFilterState
  onModelCallFiltersChange: (filters: ModelCallFilterState) => void
  canRelayView: boolean
  canRelayCreate: boolean
  upstreamColumns: TableColumnsType<LLMUpstream>
  upstreams: LLMUpstream[]
  upstreamsLoading: boolean
  upstreamsFetching: boolean
  upstreamFilter: string
  upstreamProviderFilter: string
  upstreamStatusFilter: string
  onUpstreamFilterChange: (value: string) => void
  onUpstreamProviderFilterChange: (value: string) => void
  onUpstreamStatusFilterChange: (value: string) => void
  onRefreshUpstreams: () => void
  onCreateUpstream: () => void
  modelRouteColumns: TableColumnsType<LLMModelRoute>
  modelRoutes: LLMModelRoute[]
  modelRoutesLoading: boolean
  modelRoutesFetching: boolean
  modelRouteFilter: string
  modelRouteProviderFilter: string
  modelRouteUpstreamFilter: string
  onModelRouteFilterChange: (value: string) => void
  onModelRouteProviderFilterChange: (value: string) => void
  onModelRouteUpstreamFilterChange: (value: string) => void
  onRefreshModelRoutes: () => void
  onCreateModelRoute: () => void
  onRefreshModelCalls: () => void
  expandedErrorRowRender: (record: LLMCallLog) => React.ReactNode
  expandedModelCallRowRender: (record: LLMCallLog) => React.ReactNode
}

export function GatewayRelaySection(props: GatewayRelaySectionProps) {
  const upstreamOptions = props.upstreams.map((item) => ({
    label: `${item.name} (${item.id})`,
    value: item.id,
  }))

  return (
    <Tabs
      activeKey={props.activeTab}
      className="soha-resource-tabs"
      onChange={(key) => props.onTabChange(key as GatewayTabKey)}
      destroyOnHidden
      items={[
        {
          key: 'relay',
          label: '模型中转',
          children: (
            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
              <Descriptions
                size="small"
                column={4}
                bordered
                items={[
                  { key: 'requests', label: '今日请求', children: props.metrics.requests },
                  { key: 'successRate', label: '成功率', children: props.metrics.successRate },
                  { key: 'failure', label: '失败数', children: props.metrics.failure },
                  { key: 'ttfb', label: '平均 TTFB', children: props.metrics.ttfb },
                  { key: 'ttft', label: '平均 TTFT', children: props.metrics.ttft },
                  { key: 'duration', label: '平均耗时', children: props.metrics.duration },
                  { key: 'tps', label: 'tokens/sec', children: props.metrics.tokensPerSecond },
                  { key: 'cache', label: 'Cache', children: props.metrics.cache },
                ]}
              />
              <div className="grid gap-3 lg:grid-cols-2">
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columnSettingIconOnly
                  columnSettingPlacement="header"
                  rowKey="key"
                  tableSize="small"
                  title="模型排行"
                  columns={props.rankingColumns}
                  dataSource={props.ranking}
                  loading={props.metricsLoading}
                  pagination={false}
                />
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columnSettingIconOnly
                  columnSettingPlacement="header"
                  rowKey="id"
                  tableSize="small"
                  title="最近模型错误"
                  columns={props.modelCallColumns}
                  dataSource={props.recentErrors}
                  loading={props.metricsLoading || props.modelCallsLoading}
                  pagination={false}
                  scroll={{ x: 1180 }}
                  expandable={{ expandedRowRender: props.expandedErrorRowRender }}
                />
              </div>
            </Space>
          ),
        },
        {
          key: 'upstreams',
          label: '上游管理',
          children: (
            <ManagementDataPage
              query={{
                collapsible: false,
                actions: null,
                children: (
                  <>
                    <ManagementQueryField label="Provider" width={190}>
                      <Select
                        allowClear
                        placeholder="全部 Provider"
                        options={relayProviderKindOptions}
                        value={props.upstreamProviderFilter || undefined}
                        onChange={(value) => props.onUpstreamProviderFilterChange(value ?? '')}
                      />
                    </ManagementQueryField>
                    <ManagementQueryField label="状态" width={160}>
                      <Select
                        allowClear
                        placeholder="全部状态"
                        options={relayUpstreamStatusOptions}
                        value={props.upstreamStatusFilter || undefined}
                        onChange={(value) => props.onUpstreamStatusFilterChange(value ?? '')}
                      />
                    </ManagementQueryField>
                  </>
                ),
              }}
              table={{
                columnSettingIconOnly: true,
                rowKey: 'id',
                tableSize: 'small',
                columns: props.upstreamColumns,
                dataSource: props.upstreams,
                loading: props.upstreamsLoading,
                scroll: { x: 1540 },
                toolbar: (
                  <ManagementTableToolbar>
                    <ManagementToolbarSearch
                      placeholder="过滤上游 / 模型"
                      value={props.upstreamFilter}
                      onChange={props.onUpstreamFilterChange}
                    />
                    <ManagementRefreshButton
                      aria-label="刷新上游"
                      tooltip="刷新"
                      loading={props.upstreamsFetching}
                      onClick={props.onRefreshUpstreams}
                    />
                    <Button
                      type="primary"
                      size="small"
                      icon={<PlusOutlined />}
                      disabled={!props.canRelayCreate}
                      onClick={props.onCreateUpstream}
                    >
                      新增上游
                    </Button>
                  </ManagementTableToolbar>
                ),
              }}
            />
          ),
        },
        {
          key: 'model-routes',
          label: '模型路由',
          children: (
            <ManagementDataPage
              query={{
                collapsible: false,
                actions: null,
                children: (
                  <>
                    <ManagementQueryField label="Provider" width={190}>
                      <Select
                        allowClear
                        placeholder="全部 Provider"
                        options={relayProviderKindOptions}
                        value={props.modelRouteProviderFilter || undefined}
                        onChange={(value) => props.onModelRouteProviderFilterChange(value ?? '')}
                      />
                    </ManagementQueryField>
                    <ManagementQueryField label="上游" width={260}>
                      <Select
                        allowClear
                        showSearch={{ optionFilterProp: 'label' }}
                        placeholder="全部上游"
                        options={upstreamOptions}
                        value={props.modelRouteUpstreamFilter || undefined}
                        onChange={(value) => props.onModelRouteUpstreamFilterChange(value ?? '')}
                      />
                    </ManagementQueryField>
                  </>
                ),
              }}
              table={{
                columnSettingIconOnly: true,
                rowKey: 'id',
                tableSize: 'small',
                columns: props.modelRouteColumns,
                dataSource: props.modelRoutes,
                loading: props.modelRoutesLoading,
                scroll: { x: 1380 },
                toolbar: (
                  <ManagementTableToolbar>
                    <ManagementToolbarSearch
                      placeholder="过滤 public/upstream model"
                      value={props.modelRouteFilter}
                      onChange={props.onModelRouteFilterChange}
                    />
                    <ManagementRefreshButton
                      aria-label="刷新模型路由"
                      tooltip="刷新"
                      loading={props.modelRoutesFetching}
                      onClick={props.onRefreshModelRoutes}
                    />
                    <Button
                      type="primary"
                      size="small"
                      icon={<PlusOutlined />}
                      disabled={!props.canRelayCreate}
                      onClick={props.onCreateModelRoute}
                    >
                      新增路由
                    </Button>
                  </ManagementTableToolbar>
                ),
              }}
            />
          ),
        },
        {
          key: 'model-calls',
          label: 'Model Calls',
          children: (
            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
              <ManagementQueryPanel
                actions={
                  <ManagementRefreshButton
                    aria-label="刷新 Model Calls"
                    tooltip="刷新"
                    disabled={!props.canRelayView}
                    loading={props.modelCallsFetching}
                    onClick={props.onRefreshModelCalls}
                  />
                }
              >
                <ManagementQueryField label="Actor" width={180}>
                  <Input
                    placeholder="调用者 ID"
                    value={props.modelCallFilters.actor}
                    onChange={(event) =>
                      props.onModelCallFiltersChange({
                        ...props.modelCallFilters,
                        actor: event.target.value,
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Token" width={180}>
                  <Input
                    placeholder="Token ID"
                    value={props.modelCallFilters.tokenId}
                    onChange={(event) =>
                      props.onModelCallFiltersChange({
                        ...props.modelCallFilters,
                        tokenId: event.target.value,
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Public Model" width={200}>
                  <Input
                    placeholder="模型名称"
                    value={props.modelCallFilters.publicModel}
                    onChange={(event) =>
                      props.onModelCallFiltersChange({
                        ...props.modelCallFilters,
                        publicModel: event.target.value,
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="上游" width={240}>
                  <Select
                    allowClear
                    showSearch
                    placeholder="全部上游"
                    options={upstreamOptions}
                    value={props.modelCallFilters.upstreamId || undefined}
                    onChange={(value) =>
                      props.onModelCallFiltersChange({
                        ...props.modelCallFilters,
                        upstreamId: value ?? '',
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Provider" width={170}>
                  <Select
                    allowClear
                    placeholder="全部 Provider"
                    options={relayProviderKindOptions}
                    value={props.modelCallFilters.providerKind || undefined}
                    onChange={(value) =>
                      props.onModelCallFiltersChange({
                        ...props.modelCallFilters,
                        providerKind: value ?? '',
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Endpoint" width={190}>
                  <Select
                    allowClear
                    placeholder="全部 Endpoint"
                    options={relayEndpointOptions}
                    value={props.modelCallFilters.endpoint || undefined}
                    onChange={(value) =>
                      props.onModelCallFiltersChange({
                        ...props.modelCallFilters,
                        endpoint: value ?? '',
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="状态" width={170}>
                  <Select
                    allowClear
                    placeholder="全部状态"
                    options={relayCallStatusOptions}
                    value={props.modelCallFilters.status || undefined}
                    onChange={(value) =>
                      props.onModelCallFiltersChange({
                        ...props.modelCallFilters,
                        status: value ?? '',
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Cache" width={170}>
                  <Select
                    allowClear
                    placeholder="全部 Cache 状态"
                    options={relayCacheStatusOptions}
                    value={props.modelCallFilters.cacheStatus || undefined}
                    onChange={(value) =>
                      props.onModelCallFiltersChange({
                        ...props.modelCallFilters,
                        cacheStatus: value ?? '',
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="时间范围" width={340}>
                  <RangePicker
                    showTime
                    allowClear
                    style={{ width: '100%' }}
                    placeholder={['开始时间', '结束时间']}
                    onChange={(value) =>
                      props.onModelCallFiltersChange({
                        ...props.modelCallFilters,
                        ...gatewayTimeRangeQuery(value as GatewayTimeRangeValue),
                      })
                    }
                  />
                </ManagementQueryField>
              </ManagementQueryPanel>
              {props.canRelayView ? (
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columnSettingIconOnly
                  columnSettingPlacement="header"
                  rowKey="id"
                  tableSize="small"
                  columns={props.modelCallColumns}
                  dataSource={props.modelCalls}
                  loading={props.modelCallsLoading}
                  scroll={{ x: 1420 }}
                  expandable={{ expandedRowRender: props.expandedModelCallRowRender }}
                />
              ) : (
                <ManagementState
                  bordered={false}
                  compact
                  kind="no-permission"
                  description="当前账号没有查看模型调用日志的权限。"
                />
              )}
            </Space>
          ),
        },
      ]}
    />
  )
}
