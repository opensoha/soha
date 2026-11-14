import { AdminTable } from '@/components/admin-table'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementIconButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { ReloadOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { Space, Tabs } from 'antd'
import type { ReactNode } from 'react'

export interface ProductionRecord {
  id: string
  name?: string
  kind?: string
  status?: string
  version?: string
  updatedAt?: string
  [key: string]: unknown
}

export interface ProductionTab<T extends ProductionRecord = ProductionRecord> {
  key: string
  label: string
  records: T[]
  loading?: boolean
  error?: Error | null
  hasReadData?: boolean
  columns?: TableColumnsType<T>
  actions?: (record: T) => ReactNode
  emptyDescription: string
}

interface ProductionOperationsPageProps {
  tabs: ProductionTab[]
  actions?: ReactNode
  onRefresh: () => void
  refreshing?: boolean
  notice?: ReactNode
  feedback?: ReactNode
  children?: ReactNode
}

const defaultColumns: TableColumnsType<ProductionRecord> = [
  { title: '名称 / ID', key: 'identity', render: (_, row) => row.name || row.id },
  {
    title: '类型',
    dataIndex: 'kind',
    key: 'kind',
    width: 150,
    render: (value) => String(value || '-'),
  },
  {
    title: '版本',
    dataIndex: 'version',
    key: 'version',
    width: 140,
    render: (value) => String(value || '-'),
  },
  {
    title: '状态',
    dataIndex: 'status',
    key: 'status',
    width: 130,
    render: (value) => <StatusTag value={String(value || 'unknown')} />,
  },
  {
    title: '更新时间',
    dataIndex: 'updatedAt',
    key: 'updatedAt',
    width: 190,
    render: (value) => String(value || '-'),
  },
]

export function ProductionOperationsPage({
  tabs,
  actions,
  onRefresh,
  refreshing,
  notice,
  feedback,
  children,
}: ProductionOperationsPageProps) {
  return (
    <ManagementDataPage
      className="soha-ai-production-capability"
      beforeQuery={
        <>
          {notice}
          {feedback}
        </>
      }
      tableNode={
        <Tabs
          className="soha-resource-tabs"
          destroyOnHidden
          tabBarExtraContent={
            <ManagementTableToolbar>
              <ManagementIconButton
                aria-label="刷新"
                tooltip="刷新"
                icon={<ReloadOutlined />}
                loading={refreshing}
                onClick={onRefresh}
              />
              {actions}
            </ManagementTableToolbar>
          }
          items={tabs.map((tab) => ({
            key: tab.key,
            label: `${tab.label} (${tab.loading || (tab.error && !tab.hasReadData) ? '—' : tab.records.length})`,
            children: (
              <AdminTable
                columns={[
                  ...((tab.columns as TableColumnsType<ProductionRecord> | undefined) ??
                    defaultColumns),
                  ...(tab.actions
                    ? [
                        {
                          title: '操作',
                          key: 'actions',
                          render: (_: unknown, record: ProductionRecord) => tab.actions?.(record),
                        },
                      ]
                    : []),
                ]}
                dataSource={tab.records}
                loading={tab.loading}
                error={tab.error}
                hasReadData={tab.hasReadData}
                onRetry={onRefresh}
                columnSettingIconOnly
                rowKey="id"
                empty={
                  <ManagementState title={`暂无${tab.label}`} description={tab.emptyDescription} />
                }
              />
            ),
          }))}
        />
      }
    >
      {children ? (
        <Space orientation="vertical" size={12}>
          {children}
        </Space>
      ) : null}
    </ManagementDataPage>
  )
}
