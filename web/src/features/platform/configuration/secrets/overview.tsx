import { AdminTable } from '@/components/admin-table'
import { MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import type { UseQueryResult } from '@tanstack/react-query'
import { Button, Card } from 'antd'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import { SecretCertificateCard } from './certificate-card'
import type { SecretCertificate, SecretDetail, SecretResource } from './types'

export function SecretOverview({
  metadata,
  detail,
  canReadData,
  onData,
  certificateQuery,
}: {
  metadata: SecretResource
  detail?: SecretDetail
  canReadData: boolean
  onData: () => void
  certificateQuery: UseQueryResult<SecretCertificate, Error>
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const immutable = detail?.immutable ?? metadata.immutable
  const keys = detail?.data === undefined ? undefined : Object.keys(detail.data)
  return (
    <WorkloadOverview
      aside={
        <Card title={zh ? '配置属性' : 'Configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '命名空间' : 'Namespace',
                children: <MetadataTag tone="blue" label={metadata.namespace} />,
              },
              { label: zh ? '类型' : 'Type', children: metadata.type || 'Opaque' },
              {
                label: zh ? '不可变' : 'Immutable',
                children:
                  immutable == null
                    ? zh
                      ? '未提供'
                      : 'Not provided'
                    : immutable
                      ? zh
                        ? '是'
                        : 'Yes'
                      : zh
                        ? '否'
                        : 'No',
              },
              {
                label: zh ? '创建时间' : 'Created',
                children: detail?.createdAt
                  ? formatDateTime(detail.createdAt)
                  : zh
                    ? '未提供'
                    : 'Not provided',
              },
            ]}
          />
        </Card>
      }
    >
      <Card
        title={zh ? '数据键' : 'Data keys'}
        extra={
          canReadData ? (
            <Button type="link" onClick={onData}>
              {zh ? '查看数据' : 'View data'}
            </Button>
          ) : undefined
        }
      >
        <div className="soha-secret-summary">
          <dl className="soha-k8s-detail-summary">
            <div>
              <dt>{zh ? '数据键' : 'Data keys'}</dt>
              <dd>{keys?.length ?? metadata.dataEntries}</dd>
            </div>
          </dl>
          <MetadataTag label={metadata.type || 'Opaque'} />
        </div>
        {keys ? (
          <AdminTable
            columns={[
              {
                title: 'Key',
                dataIndex: 'key',
                render: (value: string) => <TableCellText value={value} />,
              },
            ]}
            dataSource={keys.slice(0, 6).map((key) => ({ key }))}
            rowKey="key"
            pagination={false}
            enableColumnSelection={false}
            tableSize="small"
            empty={<span>{zh ? '此 Secret 没有数据键' : 'This Secret has no data keys'}</span>}
          />
        ) : (
          <p className="soha-secret-summary-note">
            {canReadData
              ? zh
                ? '在数据页按 Key 查看内容，值默认隐藏。'
                : 'Open the Data tab to inspect keys. Values are hidden by default.'
              : zh
                ? '查看数据键及内容需要敏感数据查看权限。'
                : 'Secret data permission is required to inspect keys and values.'}
          </p>
        )}
      </Card>
      {metadata.type === 'kubernetes.io/tls' ? (
        <SecretCertificateCard query={certificateQuery} />
      ) : null}
      {detail && (detail.labels !== undefined || detail.annotations !== undefined) ? (
        <WorkloadOverviewMetadata
          labels={detail.labels}
          annotations={detail.annotations}
          copyable
        />
      ) : null}
    </WorkloadOverview>
  )
}
