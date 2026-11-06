import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { Button, Card, Typography } from 'antd'
import { Link } from 'react-router-dom'
import type { IngressClassIngress } from './types'
import './ingressclass.css'

export function ingressReferencePath(clusterId: string, item: IngressClassIngress) {
  return `/network/ingresses/${encodeURIComponent(item.name)}?${new URLSearchParams({ clusterId, namespace: item.namespace })}`
}

export function IngressClassIngresses({
  items,
  clusterId,
  canRead,
  preview = false,
  tableOnly = false,
  onShowAll,
}: {
  items: readonly IngressClassIngress[]
  clusterId: string
  canRead: boolean
  preview?: boolean
  tableOnly?: boolean
  onShowAll?: () => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const content = (
    <>
      {!canRead ? (
        <ManagementState
          compact
          kind="no-permission"
          description={zh ? '没有查看 Ingresses 的权限' : 'No permission to view Ingresses'}
        />
      ) : (
        <>
          {!preview && (
            <Typography.Paragraph type="secondary">
              {zh
                ? '仅包含本次读取且有权查看的引用。'
                : 'References returned by this read and visible to your role.'}
            </Typography.Paragraph>
          )}
          <AdminTable
            pagination={false}
            rowKey={(r: IngressClassIngress) => `${r.namespace}/${r.name}`}
            enableColumnSelection={false}
            dataSource={preview ? items.slice(0, 3) : [...items]}
            scroll={{ x: preview ? 560 : 850, y: preview ? undefined : 520 }}
            columns={[
              {
                title: 'Ingress',
                dataIndex: 'name',
                width: 220,
                ellipsis: true,
                render: (name: string, item: IngressClassIngress) => (
                  <Link title={name} to={ingressReferencePath(clusterId, item)}>
                    {name}
                  </Link>
                ),
              },
              {
                title: zh ? '命名空间' : 'Namespace',
                dataIndex: 'namespace',
                width: 170,
                render: (v: string) => <MetadataTag tone="blue" label={v} />,
              },
              {
                title: zh ? '域名' : 'Hosts',
                dataIndex: 'hosts',
                width: 240,
                render: (v?: string[]) => (
                  <span className="soha-ingressclass-wrap">
                    {v?.join(', ') || (zh ? '未限定域名' : 'Any host')}
                  </span>
                ),
              },
              ...(!preview
                ? [
                    {
                      title: zh ? '地址' : 'Address',
                      dataIndex: 'address',
                      width: 180,
                      render: (v?: string) => (
                        <span className="soha-ingressclass-wrap">{v || '—'}</span>
                      ),
                    },
                    {
                      title: zh ? '时长' : 'Age',
                      dataIndex: 'ageSeconds',
                      width: 110,
                      render: (v: number) => formatAgeSeconds(v),
                    },
                  ]
                : []),
            ]}
            empty={
              <ManagementState
                compact
                kind="empty"
                description={
                  zh
                    ? '本次未读取到可查看的引用 Ingress'
                    : 'No visible referencing Ingresses returned'
                }
              />
            }
          />
        </>
      )}
    </>
  )
  return tableOnly ? (
    content
  ) : (
    <Card
      className="soha-detail-card"
      title={zh ? '引用 Ingresses' : 'Referencing Ingresses'}
      extra={
        preview ? (
          <Button type="link" onClick={onShowAll}>
            {zh ? '查看全部' : 'View all'}
          </Button>
        ) : undefined
      }
    >
      {content}
    </Card>
  )
}
