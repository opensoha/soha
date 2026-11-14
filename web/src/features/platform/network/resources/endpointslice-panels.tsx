import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { Card, Input, Select, Typography } from 'antd'
import { useState, type ReactNode } from 'react'
import type { EndpointSliceDetail, EndpointSliceEndpoint } from './types'
import '../gateway-api/gateway.css'
import './endpointslice.css'

export function SlicePorts({ ports }: { ports?: readonly string[] }) {
  return (
    <div className="soha-slice-ports">
      {ports?.length ? (
        ports.map((port, i) => (
          <MetadataTag
            key={`${port}/${i}`}
            className="soha-slice-port"
            label={<TableCellText value={port} />}
          />
        ))
      ) : (
        <span>—</span>
      )}
    </div>
  )
}
export type SliceLink = (
  kind: 'Service' | 'Pod' | 'Node',
  name: string,
  namespace?: string,
) => ReactNode
export type SliceTarget = (endpoint: EndpointSliceEndpoint) => ReactNode

export function SliceEndpoints({
  detail,
  link,
  target,
}: {
  detail: EndpointSliceDetail
  link: SliceLink
  target: SliceTarget
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const [keyword, setKeyword] = useState(''),
    [filter, setFilter] = useState('all')
  const rows = (detail.endpoints || []).map((e, i) => ({
    ...e,
    key: JSON.stringify([e.address, e.targetRef, e.nodeName, i]),
  }))
  const visible = rows.filter(
    (e) =>
      (!keyword.trim() ||
        [e.address, e.targetRef, e.nodeName, e.zone].some((v) =>
          v?.toLowerCase().includes(keyword.trim().toLowerCase()),
        )) &&
      (filter === 'all' ||
        (filter === 'ready'
          ? e.ready === true
          : filter === 'not-ready'
            ? e.ready === false
            : filter === 'terminating'
              ? e.terminating === true
              : e.ready === undefined)),
  )
  const condition = (v?: boolean) =>
    v === undefined ? (
      <span>{zh ? '未提供' : 'Not reported'}</span>
    ) : (
      <BooleanTag value={v} trueLabel={zh ? '是' : 'Yes'} falseLabel={zh ? '否' : 'No'} />
    )
  return (
    <Card
      className="soha-detail-card"
      title={zh ? '端点地址' : 'Endpoint addresses'}
      extra={detail.endpoints ? `${visible.length} / ${rows.length}` : undefined}
    >
      <div className="soha-slice-endpoint-controls">
        <Input
          allowClear
          className="soha-slice-search"
          aria-label={
            zh ? '搜索端点地址、目标、节点或区域' : 'Search address, target, node or zone'
          }
          placeholder={
            zh ? '搜索地址 / 目标 / 节点 / 区域' : 'Search address / target / node / zone'
          }
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
        />
        <Select
          className="soha-slice-state-filter"
          aria-label={zh ? '端点状态筛选' : 'Endpoint state filter'}
          value={filter}
          onChange={(v) => setFilter(String(v))}
          options={[
            { value: 'all', label: zh ? '全部' : 'All' },
            { value: 'ready', label: 'Ready' },
            { value: 'not-ready', label: zh ? '未就绪' : 'Not ready' },
            { value: 'terminating', label: zh ? '终止中' : 'Terminating' },
            { value: 'unknown', label: zh ? '未提供 Ready' : 'Ready not reported' },
          ]}
        />
      </div>
      <AdminTable
        pagination={false}
        enableColumnSelection={false}
        localSorting
        rowKey="key"
        dataSource={visible}
        scroll={{ x: 1050, y: 520 }}
        columns={[
          {
            title: zh ? '地址' : 'Address',
            dataIndex: 'address',
            width: 230,
            render: (v: string) => (
              <Typography.Text className="soha-gateway-wrap" copyable>
                {v}
              </Typography.Text>
            ),
          },
          { title: 'Ready', dataIndex: 'ready', width: 100, render: condition },
          { title: 'Serving', dataIndex: 'serving', width: 100, render: condition },
          { title: 'Terminating', dataIndex: 'terminating', width: 110, render: condition },
          {
            title: zh ? '目标引用' : 'Target reference',
            key: 'targetRef',
            width: 230,
            render: (_: unknown, e: EndpointSliceEndpoint) => target(e),
          },
          {
            title: zh ? '节点' : 'Node',
            dataIndex: 'nodeName',
            width: 200,
            render: (v?: string) => (v ? link('Node', v) : '—'),
          },
          {
            title: zh ? '区域' : 'Zone',
            dataIndex: 'zone',
            width: 150,
            render: (v?: string) => <span className="soha-gateway-wrap">{v || '—'}</span>,
          },
        ]}
        empty={
          <ManagementState
            compact
            kind="empty"
            description={
              detail.endpoints === undefined
                ? zh
                  ? '端点记录未返回'
                  : 'Endpoint records not returned'
                : rows.length
                  ? zh
                    ? '没有匹配的地址记录'
                    : 'No matching address records'
                  : zh
                    ? '本次返回的地址记录为空'
                    : 'No address records returned'
            }
          />
        }
      />
      <Typography.Paragraph type="secondary" className="soha-slice-note">
        {zh
          ? '每行是一条地址记录；未提供的条件保留未知，不自动视为 False。Ready、Serving 与 Terminating 分别展示，不据此推断请求一定可达。'
          : 'Each row is an address record. Omitted conditions remain unreported rather than false. These conditions do not establish request reachability.'}
      </Typography.Paragraph>
    </Card>
  )
}
