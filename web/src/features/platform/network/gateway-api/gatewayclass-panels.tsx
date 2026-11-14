import { AdminTable } from '@/components/admin-table'
import { ManagementKeywordField, ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { Button, Card, Typography, type TableColumnsType } from 'antd'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { buildGatewayAPIRoutePath } from '../shared/paths'
import { GatewayAddresses } from './gateway-panels'
import type { Gateway } from './types'
import './gatewayclass.css'

const gatewayPath = (clusterId: string, item: Gateway, tab?: string) =>
  buildGatewayAPIRoutePath('gateways', item.name, item.namespace, { clusterId, tab })

export function GatewayClassGateways({
  items,
  clusterId,
  canRead,
  preview = false,
  tableOnly = false,
  onShowAll,
}: {
  items: readonly Gateway[]
  clusterId: string
  canRead: boolean
  preview?: boolean
  tableOnly?: boolean
  onShowAll?: () => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const [keyword, setKeyword] = useState('')
  const needle = keyword.trim().toLowerCase()
  const filtered = preview
    ? items.slice(0, 3)
    : items.filter((item) =>
        [item.name, item.namespace, ...(item.addresses || [])].some((v) =>
          v.toLowerCase().includes(needle),
        ),
      )
  const columns: TableColumnsType<Gateway> = [
    {
      title: 'Gateway',
      dataIndex: 'name',
      width: 240,
      ellipsis: true,
      render: (name: string, item) => (
        <Link title={name} to={gatewayPath(clusterId, item)}>
          {name}
        </Link>
      ),
    },
    {
      title: zh ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 180,
      render: (value: string) => (
        <span className="soha-gateway-tags">
          <MetadataTag tone="blue" label={value} />
        </span>
      ),
    },
    {
      title: zh ? '已分配地址' : 'Assigned addresses',
      dataIndex: 'addresses',
      width: 230,
      render: (addresses?: string[]) => (
        <>
          <GatewayAddresses addresses={addresses?.slice(0, 2)} />
          {addresses && addresses.length > 2 && (
            <details className="soha-gatewayclass-more-addresses">
              <summary>
                {zh
                  ? `另 ${addresses.length - 2} 个地址`
                  : `${addresses.length - 2} more addresses`}
              </summary>
              <GatewayAddresses addresses={addresses.slice(2)} />
            </details>
          )}
        </>
      ),
    },
    {
      title: zh ? '监听器' : 'Listeners',
      dataIndex: 'listenerCount',
      width: 100,
      render: (value: number, item) => (
        <Link to={gatewayPath(clusterId, item, 'listeners')}>{value}</Link>
      ),
    },
    ...(!preview
      ? [
          {
            title: zh ? '时长' : 'Age',
            dataIndex: 'ageSeconds',
            width: 110,
            render: (value: number) => formatAgeSeconds(value),
          },
        ]
      : []),
  ]
  const content = !canRead ? (
    <ManagementState
      compact
      kind="no-permission"
      description={zh ? '没有查看 Gateways 的权限' : 'No permission to view Gateways'}
    />
  ) : (
    <>
      {!preview && (
        <div className="soha-gatewayclass-query">
          <ManagementKeywordField
            label={zh ? '关键词' : 'Keyword'}
            value={keyword}
            onChange={setKeyword}
            placeholder={
              zh ? '搜索 Gateway / 命名空间 / 地址' : 'Search Gateway / namespace / address'
            }
          />
          <Typography.Text type="secondary">
            {filtered.length} / {items.length}
          </Typography.Text>
        </div>
      )}
      <AdminTable
        pagination={false}
        enableColumnSelection={false}
        rowKey={(item) => JSON.stringify([item.namespace, item.name])}
        dataSource={[...filtered]}
        columns={columns}
        scroll={{ x: preview ? 750 : 860, y: preview ? undefined : 520 }}
        empty={
          <ManagementState
            compact
            kind="empty"
            description={
              needle
                ? zh
                  ? '没有匹配的 Gateway'
                  : 'No matching Gateways'
                : zh
                  ? '本次未读取到可查看的引用 Gateway'
                  : 'No visible referencing Gateways returned'
            }
          />
        }
      />
      {!preview && (
        <Typography.Paragraph type="secondary" className="soha-gatewayclass-coverage">
          {zh
            ? '仅展示本次返回且有权查看的配置引用。'
            : 'Configured references returned by this read and visible to your role.'}
        </Typography.Paragraph>
      )}
    </>
  )
  return tableOnly ? (
    content
  ) : (
    <Card
      className="soha-detail-card"
      title={zh ? '引用 Gateways' : 'Referencing Gateways'}
      extra={
        preview && canRead ? (
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
