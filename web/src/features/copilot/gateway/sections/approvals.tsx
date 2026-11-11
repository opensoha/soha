import type { ReactNode } from 'react'
import type { TableColumnsType } from 'antd'
import { DatePicker, Input, Select, Space } from 'antd'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
} from '@/components/management-list'
import type {
  AIClient,
  ApprovalFilterState,
  ApprovalRequest,
  GatewayManifest,
  GatewayTimeRangeValue,
} from '../types'
import {
  approvalRequestStrategyOptions,
  approvalStatusOptions,
  gatewayTimeRangeQuery,
  riskLevelOptions,
} from '../types'

const { RangePicker } = DatePicker

export interface GatewayApprovalsSectionProps {
  columns: TableColumnsType<ApprovalRequest>
  approvals: ApprovalRequest[]
  loading: boolean
  filters: ApprovalFilterState
  clients: AIClient[]
  manifest?: GatewayManifest
  expandedRowRender: (record: ApprovalRequest) => ReactNode
  onFiltersChange: (filters: ApprovalFilterState) => void
  onRefresh: () => void
}

export function GatewayApprovalsSection({
  columns,
  approvals,
  loading,
  filters,
  clients,
  manifest,
  expandedRowRender,
  onFiltersChange,
  onRefresh,
}: GatewayApprovalsSectionProps) {
  const clientOptions = clients.map((item) => ({
    label: `${item.name} (${item.id})`,
    value: item.id,
  }))
  const toolOptions = manifest?.tools.map((item) => ({ label: item.name, value: item.name })) ?? []

  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <ManagementQueryPanel
        actions={
          <ManagementRefreshButton aria-label="刷新审批请求" tooltip="刷新" onClick={onRefresh} />
        }
      >
        <ManagementQueryField label="Request ID" width={220}>
          <Input
            placeholder="approvalRequestId"
            value={filters.id}
            onChange={(event) =>
              onFiltersChange({
                ...filters,
                id: event.target.value,
                status: event.target.value ? '' : filters.status,
              })
            }
          />
        </ManagementQueryField>
        <ManagementQueryField label="状态" width={150}>
          <Select
            allowClear
            placeholder="全部状态"
            options={approvalStatusOptions}
            value={filters.status || undefined}
            onChange={(value) => onFiltersChange({ ...filters, status: value ?? '' })}
          />
        </ManagementQueryField>
        <ManagementQueryField label="Actor" width={180}>
          <Input
            placeholder="actorId"
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
        <ManagementQueryField label="Risk" width={140}>
          <Select
            allowClear
            placeholder="全部风险"
            options={riskLevelOptions}
            value={filters.riskLevel || undefined}
            onChange={(value) => onFiltersChange({ ...filters, riskLevel: value ?? '' })}
          />
        </ManagementQueryField>
        <ManagementQueryField label="Strategy" width={190}>
          <Select
            allowClear
            placeholder="全部策略"
            options={approvalRequestStrategyOptions}
            value={filters.strategy || undefined}
            onChange={(value) => onFiltersChange({ ...filters, strategy: value ?? '' })}
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
        dataSource={approvals}
        loading={loading}
        scroll={{ x: 1560 }}
        expandable={{ expandedRowRender }}
      />
    </Space>
  )
}
