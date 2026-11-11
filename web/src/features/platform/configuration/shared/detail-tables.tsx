import type { ReactNode } from 'react'
import { Card, Table } from 'antd'
import { StatusTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import type { WorkloadCondition } from '@/types'
import { formatDateTime } from '@/utils/time'
import type { TableColumnsType } from 'antd'
export { AdmissionWebhooks } from './webhooks'

export function ConfigurationConditions({ conditions }: { conditions?: WorkloadCondition[] }) {
  const { localeCode } = useI18n()
  const columns: TableColumnsType<WorkloadCondition> = [
    { title: localeCode === 'zh_CN' ? '条件' : 'Condition', dataIndex: 'type', width: 160 },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      width: 100,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '原因' : 'Reason',
      dataIndex: 'reason',
      width: 180,
      render: (value?: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '消息' : 'Message',
      dataIndex: 'message',
      ellipsis: { showTitle: false },
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '最近变化' : 'Last Transition',
      dataIndex: 'lastTransitionTime',
      width: 180,
      render: (value?: string) => (value ? formatDateTime(value) : '-'),
    },
  ]
  return (
    <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '条件' : 'Conditions'}>
      <Table
        className="soha-platform-table"
        columns={columns}
        dataSource={conditions ?? []}
        pagination={false}
        rowKey="type"
        size="small"
        tableLayout="fixed"
      />
    </Card>
  )
}

export function renderResourceValues(value?: Record<string, string>): ReactNode {
  const entries = Object.entries(value ?? {})
  return entries.length > 0 ? entries.map(([key, item]) => `${key}: ${item}`).join(', ') : '-'
}
