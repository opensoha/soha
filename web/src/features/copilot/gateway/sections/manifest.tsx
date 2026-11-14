import { PlayCircleOutlined } from '@ant-design/icons'
import { useState } from 'react'
import type { TableColumnsType } from 'antd'
import { Descriptions, Input, Select, Space } from 'antd'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementState,
} from '@/components/management-list'
import { GatewayToolInvokeDrawer } from '../tool-invoke-drawer'
import type { AIClient, GatewayManifest, GatewayTool } from '../types'
import { compactList } from '../presentation'

export interface GatewayManifestSectionProps {
  manifest?: GatewayManifest
  loading: boolean
  clients: AIClient[]
  filters: { aiClientId: string; skillId: string; source: string }
  toolColumns: TableColumnsType<GatewayTool>
  canInvoke: boolean
  onFiltersChange: (filters: { aiClientId: string; skillId: string; source: string }) => void
  onRefresh: () => void
}

export function GatewayManifestSection({
  manifest,
  loading,
  clients,
  filters,
  toolColumns,
  canInvoke,
  onFiltersChange,
  onRefresh,
}: GatewayManifestSectionProps) {
  const clientOptions = clients.map((item) => ({
    label: `${item.name} (${item.id})`,
    value: item.id,
  }))
  const skillOptions =
    manifest?.skills?.map((item) => ({ label: `${item.name} (${item.id})`, value: item.id })) ?? []
  const [selectedTool, setSelectedTool] = useState<GatewayTool>()
  const columns: TableColumnsType<GatewayTool> = canInvoke
    ? [
        ...toolColumns,
        {
          title: '',
          key: 'invoke',
          fixed: 'right',
          width: 64,
          render: (_, tool) => (
            <ManagementIconButton
              aria-label={`调用 ${tool.name}`}
              tooltip="调用"
              icon={<PlayCircleOutlined />}
              onClick={() => setSelectedTool(tool)}
            />
          ),
        },
      ]
    : toolColumns

  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <ManagementQueryPanel
        collapsible={false}
        actions={
          <ManagementRefreshButton aria-label="刷新 Manifest" tooltip="刷新" onClick={onRefresh} />
        }
      >
        <ManagementQueryField label="AI Client" width={260}>
          <Select
            allowClear
            placeholder="全部 Client"
            options={clientOptions}
            value={filters.aiClientId || undefined}
            onChange={(value) => onFiltersChange({ ...filters, aiClientId: value ?? '' })}
          />
        </ManagementQueryField>
        <ManagementQueryField label="Skill" width={260}>
          <Select
            allowClear
            placeholder="全部 Skill"
            options={skillOptions}
            value={filters.skillId || undefined}
            onChange={(value) => onFiltersChange({ ...filters, skillId: value ?? '' })}
          />
        </ManagementQueryField>
        <ManagementQueryField label="Source" width={180}>
          <Input
            placeholder="来源"
            value={filters.source}
            onChange={(event) => onFiltersChange({ ...filters, source: event.target.value })}
          />
        </ManagementQueryField>
      </ManagementQueryPanel>
      {manifest ? (
        <>
          <Descriptions
            size="small"
            column={4}
            bordered
            items={[
              {
                key: 'principal',
                label: 'Subject',
                children: manifest.principal?.userName || manifest.principal?.userId || '-',
              },
              {
                key: 'roles',
                label: 'Roles',
                children: compactList(manifest.principal?.roles, 2),
              },
              {
                key: 'permissions',
                label: 'Permissions',
                children: manifest.permissionKeys.length,
              },
              { key: 'denied', label: 'Denied', children: manifest.summary.deniedCount },
            ]}
          />
          <AdminTable
            shellClassName="soha-management-table-shell"
            columnSettingIconOnly
            columnSettingPlacement="header"
            rowKey="name"
            tableSize="small"
            columns={columns}
            dataSource={manifest.tools}
            loading={loading}
            pagination={{ pageSize: 8 }}
            scroll={{ x: 960 }}
          />
        </>
      ) : (
        <ManagementState
          bordered={false}
          compact
          title="暂无 Manifest"
          description="选择 AI client、skill 或 source 后查看可调用工具清单。"
        />
      )}
      <GatewayToolInvokeDrawer
        tool={selectedTool}
        aiClientId={filters.aiClientId || undefined}
        skillId={filters.skillId || undefined}
        onClose={() => setSelectedTool(undefined)}
      />
    </Space>
  )
}
