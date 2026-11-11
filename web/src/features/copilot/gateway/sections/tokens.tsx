import { PlusOutlined, StopOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { Button, Space, Tabs } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { ManagementTableToolbar, ManagementToolbarSearch } from '@/components/management-list'
import type {
  GatewayTabKey,
  PersonalAccessToken,
  ServiceAccount,
  ServiceAccountToken,
} from '../types'

export interface GatewayTokensSectionProps {
  activeTab: GatewayTabKey
  onTabChange: (tab: GatewayTabKey) => void
  tokenColumns: TableColumnsType<PersonalAccessToken>
  personalTokens: PersonalAccessToken[]
  personalTokensLoading: boolean
  serviceAccountColumns: TableColumnsType<ServiceAccount>
  serviceAccounts: ServiceAccount[]
  serviceAccountsLoading: boolean
  serviceTokenColumns: TableColumnsType<ServiceAccountToken>
  serviceTokens: ServiceAccountToken[]
  serviceTokensLoading: boolean
  canViewAll: boolean
  canCreate: boolean
  canRevoke: boolean
  canInvoke: boolean
  tokenFilter: string
  serviceTokenFilter: string
  onTokenFilterChange: (value: string) => void
  onServiceTokenFilterChange: (value: string) => void
  onCreatePersonalToken: () => void
  onCreateServiceAccount: () => void
  onRevokeServiceToken: () => void
}

export function GatewayTokensSection({
  activeTab,
  onTabChange,
  tokenColumns,
  personalTokens,
  personalTokensLoading,
  serviceAccountColumns,
  serviceAccounts,
  serviceAccountsLoading,
  serviceTokenColumns,
  serviceTokens,
  serviceTokensLoading,
  canViewAll,
  canCreate,
  canRevoke,
  canInvoke,
  tokenFilter,
  serviceTokenFilter,
  onTokenFilterChange,
  onServiceTokenFilterChange,
  onCreatePersonalToken,
  onCreateServiceAccount,
  onRevokeServiceToken,
}: GatewayTokensSectionProps) {
  return (
    <Tabs
      activeKey={activeTab}
      className="soha-resource-tabs"
      onChange={(key) => onTabChange(key as GatewayTabKey)}
      destroyOnHidden
      items={[
        {
          key: 'tokens',
          label: 'Tokens',
          children: (
            <AdminTable
              shellClassName="soha-management-table-shell"
              columnSettingIconOnly
              columnSettingPlacement="header"
              rowKey="id"
              tableSize="small"
              columns={tokenColumns}
              dataSource={personalTokens}
              loading={personalTokensLoading}
              scroll={{ x: 1420 }}
              title={canViewAll ? 'User Login Keys' : 'My Login Keys'}
              headerExtra={
                <ManagementTableToolbar>
                  <Button
                    type="primary"
                    size="small"
                    icon={<PlusOutlined />}
                    disabled={!canInvoke}
                    onClick={onCreatePersonalToken}
                  >
                    生成我的 key
                  </Button>
                  <ManagementToolbarSearch
                    placeholder="过滤 key / owner"
                    value={tokenFilter}
                    onChange={onTokenFilterChange}
                  />
                </ManagementTableToolbar>
              }
            />
          ),
        },
        {
          key: 'service-accounts',
          label: 'Service Accounts',
          children: (
            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
              <AdminTable
                shellClassName="soha-management-table-shell"
                columnSettingIconOnly
                columnSettingPlacement="header"
                rowKey="id"
                tableSize="small"
                columns={serviceAccountColumns}
                dataSource={serviceAccounts}
                loading={serviceAccountsLoading}
                scroll={{ x: 860 }}
                title="Service Accounts"
                headerExtra={
                  <ManagementTableToolbar>
                    <Button
                      type="primary"
                      size="small"
                      icon={<PlusOutlined />}
                      disabled={!canCreate}
                      onClick={onCreateServiceAccount}
                    >
                      新增服务账号
                    </Button>
                  </ManagementTableToolbar>
                }
              />
              <AdminTable
                shellClassName="soha-management-table-shell"
                columnSettingIconOnly
                columnSettingPlacement="header"
                rowKey="id"
                tableSize="small"
                columns={serviceTokenColumns}
                dataSource={serviceTokens}
                loading={serviceTokensLoading}
                scroll={{ x: 1520 }}
                title="Service Tokens"
                headerExtra={
                  <ManagementTableToolbar>
                    <Button
                      size="small"
                      danger
                      icon={<StopOutlined />}
                      disabled={!canRevoke}
                      onClick={onRevokeServiceToken}
                    >
                      吊销服务 token
                    </Button>
                    <ManagementToolbarSearch
                      placeholder="过滤 service token"
                      value={serviceTokenFilter}
                      onChange={onServiceTokenFilterChange}
                    />
                  </ManagementTableToolbar>
                }
              />
            </Space>
          ),
        },
      ]}
    />
  )
}
