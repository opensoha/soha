import { PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { Button } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { ManagementTableToolbar, ManagementToolbarSearch } from '@/components/management-list'
import type { AIClient } from '../types'

export interface GatewayClientsSectionProps {
  columns: TableColumnsType<AIClient>
  clients: AIClient[]
  loading: boolean
  canCreate: boolean
  filter: string
  onFilterChange: (value: string) => void
  onCreate: () => void
}

export function GatewayClientsSection({
  columns,
  clients,
  loading,
  canCreate,
  filter,
  onFilterChange,
  onCreate,
}: GatewayClientsSectionProps) {
  return (
    <AdminTable
      shellClassName="soha-management-table-shell"
      columnSettingIconOnly
      columnSettingPlacement="header"
      rowKey="id"
      tableSize="small"
      columns={columns}
      dataSource={clients}
      loading={loading}
      scroll={{ x: 920 }}
      title="AI Clients"
      headerExtra={
        <ManagementTableToolbar>
          <Button
            type="primary"
            size="small"
            icon={<PlusOutlined />}
            disabled={!canCreate}
            onClick={onCreate}
          >
            新增 client
          </Button>
          <ManagementToolbarSearch
            placeholder="过滤 client"
            value={filter}
            onChange={onFilterChange}
          />
        </ManagementTableToolbar>
      }
    />
  )
}
