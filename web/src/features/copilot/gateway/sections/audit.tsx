import type { TableColumnsType } from 'antd'
import { DatePicker, Input, Select, Space, Tabs } from 'antd'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementState,
} from '@/components/management-list'
import type {
  AIClient,
  AuditFilterState,
  GatewayAuditLog,
  GatewayManifest,
  GatewayTabKey,
  GatewayTimeRangeValue,
  LLMCallLog,
  LLMUpstream,
  ModelCallFilterState,
} from '../types'
import {
  auditActionOptions,
  auditResultOptions,
  gatewayTimeRangeQuery,
  relayCacheStatusOptions,
  relayCallStatusOptions,
  relayEndpointOptions,
  relayProviderKindOptions,
  riskLevelOptions,
} from '../types'

const { RangePicker } = DatePicker

export interface GatewayCallLogsSectionProps {
  activeTab: GatewayTabKey
  onTabChange: (tab: GatewayTabKey) => void
  columns: TableColumnsType<GatewayAuditLog>
  logs: GatewayAuditLog[]
  loading: boolean
  filters: AuditFilterState
  clients: AIClient[]
  manifest?: GatewayManifest
  expandedRowRender: (record: GatewayAuditLog) => React.ReactNode
  modelCallColumns: TableColumnsType<LLMCallLog>
  modelCalls: LLMCallLog[]
  modelCallsLoading: boolean
  modelCallsFetching: boolean
  modelCallFilters: ModelCallFilterState
  upstreams: LLMUpstream[]
  canRelayView: boolean
  expandedModelCallRowRender: (record: LLMCallLog) => React.ReactNode
  onModelCallFiltersChange: (filters: ModelCallFilterState) => void
  onRefreshModelCalls: () => void
  onFiltersChange: (filters: AuditFilterState) => void
  onRefresh: () => void
}

export function GatewayCallLogsSection({
  columns,
  activeTab,
  onTabChange,
  logs,
  loading,
  filters,
  clients,
  manifest,
  expandedRowRender,
  modelCallColumns,
  modelCalls,
  modelCallsLoading,
  modelCallsFetching,
  modelCallFilters,
  upstreams,
  canRelayView,
  expandedModelCallRowRender,
  onModelCallFiltersChange,
  onRefreshModelCalls,
  onFiltersChange,
  onRefresh,
}: GatewayCallLogsSectionProps) {
  const clientOptions = clients.map((item) => ({
    label: `${item.name} (${item.id})`,
    value: item.id,
  }))
  const toolOptions = manifest?.tools.map((item) => ({ label: item.name, value: item.name })) ?? []
  const upstreamOptions = upstreams.map((item) => ({
    label: `${item.name} (${item.id})`,
    value: item.id,
  }))

  return (
    <Tabs
      activeKey={activeTab}
      className="soha-resource-tabs"
      onChange={(key) => onTabChange(key as GatewayTabKey)}
      destroyOnHidden
      items={[
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
                    disabled={!canRelayView}
                    loading={modelCallsFetching}
                    onClick={onRefreshModelCalls}
                  />
                }
              >
                <ManagementQueryField label="Actor" width={180}>
                  <Input
                    placeholder="调用者 ID"
                    value={modelCallFilters.actor}
                    onChange={(event) =>
                      onModelCallFiltersChange({
                        ...modelCallFilters,
                        actor: event.target.value,
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Token" width={180}>
                  <Input
                    placeholder="Token ID"
                    value={modelCallFilters.tokenId}
                    onChange={(event) =>
                      onModelCallFiltersChange({
                        ...modelCallFilters,
                        tokenId: event.target.value,
                      })
                    }
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Public Model" width={200}>
                  <Input
                    placeholder="模型名称"
                    value={modelCallFilters.publicModel}
                    onChange={(event) =>
                      onModelCallFiltersChange({
                        ...modelCallFilters,
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
                    value={modelCallFilters.upstreamId || undefined}
                    onChange={(value) =>
                      onModelCallFiltersChange({
                        ...modelCallFilters,
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
                    value={modelCallFilters.providerKind || undefined}
                    onChange={(value) =>
                      onModelCallFiltersChange({
                        ...modelCallFilters,
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
                    value={modelCallFilters.endpoint || undefined}
                    onChange={(value) =>
                      onModelCallFiltersChange({
                        ...modelCallFilters,
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
                    value={modelCallFilters.status || undefined}
                    onChange={(value) =>
                      onModelCallFiltersChange({
                        ...modelCallFilters,
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
                    value={modelCallFilters.cacheStatus || undefined}
                    onChange={(value) =>
                      onModelCallFiltersChange({
                        ...modelCallFilters,
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
                      onModelCallFiltersChange({
                        ...modelCallFilters,
                        ...gatewayTimeRangeQuery(value as GatewayTimeRangeValue),
                      })
                    }
                  />
                </ManagementQueryField>
              </ManagementQueryPanel>
              {canRelayView ? (
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columnSettingIconOnly
                  columnSettingPlacement="header"
                  rowKey="id"
                  tableSize="small"
                  columns={modelCallColumns}
                  dataSource={modelCalls}
                  loading={modelCallsLoading}
                  scroll={{ x: 1420 }}
                  expandable={{ expandedRowRender: expandedModelCallRowRender }}
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
        {
          key: 'audit',
          label: 'Tool Calls',
          children: (
            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
              <ManagementQueryPanel
                actions={
                  <ManagementRefreshButton
                    aria-label="刷新 Tool Calls"
                    tooltip="刷新"
                    onClick={onRefresh}
                  />
                }
              >
                <ManagementQueryField label="Actor" width={190}>
                  <Input
                    placeholder="调用者 ID"
                    value={filters.actor}
                    onChange={(event) => onFiltersChange({ ...filters, actor: event.target.value })}
                  />
                </ManagementQueryField>
                <ManagementQueryField label="AI Client" width={220}>
                  <Select
                    allowClear
                    placeholder="全部 Client"
                    options={clientOptions}
                    value={filters.aiClientId || undefined}
                    onChange={(value) => onFiltersChange({ ...filters, aiClientId: value ?? '' })}
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Tool" width={260}>
                  <Select
                    allowClear
                    placeholder="全部 Tool"
                    options={toolOptions}
                    value={filters.toolName || undefined}
                    onChange={(value) => onFiltersChange({ ...filters, toolName: value ?? '' })}
                  />
                </ManagementQueryField>
                <ManagementQueryField label="动作" width={180}>
                  <Select
                    allowClear
                    placeholder="全部动作"
                    options={auditActionOptions}
                    value={filters.action || undefined}
                    onChange={(value) => onFiltersChange({ ...filters, action: value ?? '' })}
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Risk" width={140}>
                  <Select
                    allowClear
                    placeholder="全部风险"
                    options={riskLevelOptions}
                    value={filters.riskLevel || undefined}
                    onChange={(value) => onFiltersChange({ ...filters, riskLevel: value ?? '' })}
                  />
                </ManagementQueryField>
                <ManagementQueryField label="Result" width={190}>
                  <Select
                    allowClear
                    placeholder="全部结果"
                    options={auditResultOptions}
                    value={filters.result || undefined}
                    onChange={(value) => onFiltersChange({ ...filters, result: value ?? '' })}
                  />
                </ManagementQueryField>
                <ManagementQueryField label="时间范围" width={340}>
                  <RangePicker
                    showTime
                    allowClear
                    style={{ width: '100%' }}
                    placeholder={['开始时间', '结束时间']}
                    onChange={(value) =>
                      onFiltersChange({
                        ...filters,
                        ...gatewayTimeRangeQuery(value as GatewayTimeRangeValue),
                      })
                    }
                  />
                </ManagementQueryField>
              </ManagementQueryPanel>
              <AdminTable
                shellClassName="soha-management-table-shell"
                columnSettingIconOnly
                columnSettingPlacement="header"
                rowKey="id"
                tableSize="small"
                columns={columns}
                dataSource={logs}
                loading={loading}
                scroll={{ x: 1220 }}
                expandable={{ expandedRowRender }}
              />
            </Space>
          ),
        },
      ]}
    />
  )
}
