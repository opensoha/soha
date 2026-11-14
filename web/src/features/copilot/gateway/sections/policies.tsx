import { PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { Button } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { ManagementTableToolbar, ManagementToolbarSearch } from '@/components/management-list'
import type { AccessPolicy } from '../types'

export interface GatewayPoliciesSectionProps {
  columns: TableColumnsType<AccessPolicy>
  policies: AccessPolicy[]
  loading: boolean
  canCreate: boolean
  filter: string
  onFilterChange: (value: string) => void
  onCreate: () => void
}

export function GatewayPoliciesSection({
  columns,
  policies,
  loading,
  canCreate,
  filter,
  onFilterChange,
  onCreate,
}: GatewayPoliciesSectionProps) {
  return (
    <AdminTable
      shellClassName="soha-management-table-shell"
      columnSettingIconOnly
      columnSettingPlacement="header"
      rowKey="id"
      tableSize="small"
      columns={columns}
      dataSource={policies}
      loading={loading}
      scroll={{ x: 1080 }}
      title="Access Policies"
      headerExtra={
        <ManagementTableToolbar>
          <Button
            type="primary"
            size="small"
            icon={<PlusOutlined />}
            disabled={!canCreate}
            onClick={onCreate}
          >
            新增 policy
          </Button>
          <ManagementToolbarSearch
            placeholder="过滤 policy"
            value={filter}
            onChange={onFilterChange}
          />
        </ManagementTableToolbar>
      }
    />
  )
}
