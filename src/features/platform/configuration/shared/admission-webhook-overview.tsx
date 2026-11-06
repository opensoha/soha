import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { Card, Typography, type TableColumnsType } from 'antd'
import { useSearchParams } from 'react-router-dom'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import {
  webhookTarget,
  type AdmissionWebhookDefinition,
  type AdmissionWebhookDetail,
} from './admission-webhook-model'

export function AdmissionWebhookOverview({ detail }: { detail: AdmissionWebhookDetail }) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const [, setParams] = useSearchParams()
  const open = (name?: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        next.set('tab', 'webhooks')
        if (name) next.set('webhook', name)
        return next
      },
      { replace: true },
    )
  const columns: TableColumnsType<AdmissionWebhookDefinition> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 280,
      render: (name: string) => <TableCellLink label={name} onClick={() => open(name)} />,
    },
    {
      title: zh ? '回调目标' : 'Callback target',
      key: 'target',
      width: 280,
      render: (_, item) => <TableCellText value={webhookTarget(item)} />,
    },
    {
      title: 'FailurePolicy',
      dataIndex: 'failurePolicy',
      width: 130,
      render: (value?: string) => <MetadataTag label={value || (zh ? '未声明' : 'Not declared')} />,
    },
    {
      title: zh ? '超时' : 'Timeout',
      dataIndex: 'timeoutSeconds',
      width: 110,
      render: (value?: number) => (value == null ? '—' : value + ' s'),
    },
  ]
  return (
    <WorkloadOverview
      aside={
        <Card title={zh ? '准入配置' : 'Admission configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '作用域' : 'Scope', children: zh ? '集群级' : 'Cluster' },
              { label: 'Webhooks', children: detail.webhooks.length },
              { label: 'Service', children: detail.webhooks.filter((item) => item.service).length },
              { label: 'URL', children: detail.webhooks.filter((item) => item.url).length },
              { label: 'API', children: 'admissionregistration.k8s.io/v1' },
              {
                label: zh ? '创建时间' : 'Created',
                children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
              },
              ...(detail.uid
                ? [
                    {
                      label: 'UID',
                      children: <Typography.Text copyable>{detail.uid}</Typography.Text>,
                    },
                  ]
                : []),
            ]}
          />
        </Card>
      }
    >
      <Card
        title={zh ? 'Webhook 摘要' : 'Webhook summary'}
        extra={
          <Typography.Link onClick={() => open()}>
            {zh ? '查看全部配置' : 'View configuration'}
          </Typography.Link>
        }
      >
        <AdminTable
          columns={columns}
          dataSource={detail.webhooks}
          rowKey="name"
          pagination={false}
          tableSize="small"
          enableColumnSelection={false}
          scroll={{ x: 800 }}
          tableLayout="fixed"
          empty={
            <ManagementState
              kind="empty"
              compact
              bordered={false}
              description={zh ? '未配置 Webhook' : 'No webhooks configured'}
            />
          }
        />
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
