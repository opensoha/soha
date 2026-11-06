import { AdminTable } from '@/components/admin-table'
import { MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { ResourceGitOpsStatus } from '@/features/platform/shared/resource-gitops-status'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import { configurationDataSize } from '../shared/data-primitives'
import { useI18n } from '@/i18n'
import { Button, Card } from 'antd'
import { formatDateTime } from '@/utils/time'
import type { ConfigMapDetail } from './types'

export function ConfigMapOverview({
  detail,
  clusterId,
  onData,
}: {
  detail: ConfigMapDetail
  clusterId: string
  onData: () => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const text = Object.entries(detail.data ?? {})
  const binary = Object.entries(detail.binaryData ?? {})
  const rows = [
    ...text.map(([key, value]) => ({
      key,
      type: 'data',
      size: configurationDataSize(value),
      lines: value.split('\n').length,
    })),
    ...binary.map(([key]) => ({ key, type: 'binaryData', size: 'Base64', lines: undefined })),
  ]
  return (
    <WorkloadOverview
      aside={
        <>
          <Card title={zh ? '配置属性' : 'Configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '命名空间' : 'Namespace',
                  children: <MetadataTag tone="blue" label={detail.namespace} />,
                },
                {
                  label: zh ? '不可变' : 'Immutable',
                  children: detail.immutable ? (zh ? '是' : 'Yes') : zh ? '否' : 'No',
                },
                {
                  label: zh ? '创建时间' : 'Created',
                  children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
                },
              ]}
            />
          </Card>
          <ResourceGitOpsStatus
            clusterId={clusterId}
            kind="ConfigMap"
            name={detail.name}
            namespace={detail.namespace}
          />
        </>
      }
    >
      <Card
        title={zh ? '数据键' : 'Data keys'}
        extra={
          <Button type="link" onClick={onData}>
            {zh ? '查看全部数据' : 'View all data'}
          </Button>
        }
      >
        <dl className="soha-k8s-detail-summary soha-configmap-summary">
          <div>
            <dt>{zh ? '全部键' : 'All keys'}</dt>
            <dd>{rows.length}</dd>
          </div>
          <div>
            <dt>{zh ? '文本键' : 'Text keys'}</dt>
            <dd>{text.length}</dd>
          </div>
          <div>
            <dt>{zh ? '二进制键' : 'Binary keys'}</dt>
            <dd>{binary.length}</dd>
          </div>
        </dl>
        <AdminTable
          columns={[
            {
              title: 'Key',
              dataIndex: 'key',
              ellipsis: true,
              render: (value: string) => <TableCellText value={value} />,
            },
            {
              title: zh ? '类型' : 'Type',
              dataIndex: 'type',
              width: 120,
              render: (value: string) => <MetadataTag label={value} />,
            },
            {
              title: zh ? '大小 / 行数' : 'Size / lines',
              dataIndex: 'size',
              width: 150,
              render: (_value: string, record: (typeof rows)[number]) =>
                record.lines === undefined
                  ? 'Base64'
                  : `${record.size} · ${record.lines} ${zh ? '行' : 'lines'}`,
            },
          ]}
          dataSource={rows.slice(0, 6)}
          rowKey="key"
          pagination={false}
          enableColumnSelection={false}
          tableSize="small"
          empty={<span>{zh ? '此 ConfigMap 没有数据键' : 'This ConfigMap has no data keys'}</span>}
        />
        {rows.length > 6 && (
          <Button type="link" onClick={onData}>
            {zh ? `查看全部 ${rows.length} 个键` : `View all ${rows.length} keys`}
          </Button>
        )}
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
