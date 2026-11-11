import { AdminTable } from '@/components/admin-table'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import type { WorkloadCondition } from '@/types'
import { formatDateTime } from '@/utils/time'
import { Card, Typography, type TableColumnsType } from 'antd'
import './workload-conditions.css'

function ConditionStatus({ value }: { value: string }) {
  return ['True', 'False'].includes(value) ? (
    <BooleanTag value={value === 'True'} />
  ) : (
    <StatusTag value={value} />
  )
}

export function WorkloadConditions({
  conditions = [],
  title,
  showTransitionTime = false,
}: {
  conditions?: WorkloadCondition[]
  title?: string
  showTransitionTime?: boolean
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const columns: TableColumnsType<WorkloadCondition> = [
    { title: zh ? '条件' : 'Condition', dataIndex: 'type', width: 230 },
    {
      title: zh ? '状态' : 'Status',
      dataIndex: 'status',
      width: 100,
      render: (value: string) => <ConditionStatus value={value} />,
    },
    { title: zh ? '原因' : 'Reason', dataIndex: 'reason', width: 180 },
    { title: zh ? '消息' : 'Message', dataIndex: 'message' },
    ...(showTransitionTime
      ? [
          {
            title: zh ? '最近变化' : 'Last transition',
            dataIndex: 'lastTransitionTime',
            width: 160,
            render: (value?: string) => (value ? formatDateTime(value) : '—'),
          },
        ]
      : []),
  ]
  return (
    <Card className="soha-detail-card" title={title ?? (zh ? '运行条件' : 'Conditions')}>
      <details className="soha-workload-conditions">
        <summary>
          <span className="soha-workload-conditions-summary">
            {conditions.map((condition) => (
              <span key={condition.type}>
                {condition.type} <ConditionStatus value={condition.status} />
              </span>
            ))}
            {!conditions.length && (
              <Typography.Text type="secondary">
                {zh ? '未返回运行条件' : 'No conditions returned'}
              </Typography.Text>
            )}
          </span>
        </summary>
        <AdminTable
          pagination={false}
          enableColumnSelection={false}
          rowKey="type"
          dataSource={conditions}
          columns={columns}
          scroll={{ x: showTransitionTime ? 960 : 800 }}
        />
      </details>
    </Card>
  )
}
