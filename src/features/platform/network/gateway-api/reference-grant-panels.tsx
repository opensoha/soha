import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { Card, Typography } from 'antd'
import { type ReactNode } from 'react'
import type { ReferenceGrantDetail, ReferenceGrantTo } from './types'
import { grantSummaryParts } from './reference-grant-model'
import './gateway.css'
import './reference-grant.css'

export function GrantSummary({
  labels,
  direction,
}: {
  labels?: readonly string[]
  direction: 'from' | 'to'
}) {
  const { localeCode } = useI18n()
  const all = localeCode === 'zh_CN' ? '全部名称' : 'All names'
  if (!labels?.length) return <span>—</span>
  const item = (value: string, index: number) => {
    const parts = grantSummaryParts(value)
    const tag = (label: string, tone: 'default' | 'blue' = 'default') => (
      <MetadataTag
        className="soha-grant-summary-tag"
        tone={tone}
        label={<TableCellText value={label} />}
      />
    )
    return (
      <div className="soha-grant-summary-entry" key={`${value}/${index}`}>
        {parts ? (
          <>
            {direction === 'from' && tag(parts.namespace, 'blue')}
            {tag(parts.kind)}
            {direction === 'to' && tag(parts.name || all)}
            {tag(`API · ${parts.group}`)}
          </>
        ) : (
          tag(value)
        )}
      </div>
    )
  }
  return (
    <div className="soha-grant-summary">
      {labels.slice(0, 2).map(item)}
      {labels.length > 2 && (
        <details>
          <summary>+{labels.length - 2}</summary>
          {labels.slice(2).map(item)}
        </details>
      )}
    </div>
  )
}

export type GrantTargetRenderer = (target: ReferenceGrantTo) => ReactNode

export function GrantRules({
  detail,
  renderTarget,
}: {
  detail: ReferenceGrantDetail
  renderTarget: GrantTargetRenderer
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const empty = (available: boolean) => (
    <ManagementState
      compact
      kind="empty"
      description={
        available
          ? zh
            ? '未返回授权项'
            : 'No entries returned'
          : zh
            ? '结构化规则未返回，可查看 YAML'
            : 'Structured rules unavailable; inspect YAML'
      }
    />
  )
  return (
    <div className="soha-detail-stack">
      <Card className="soha-detail-card" title={zh ? '允许来源 · From' : 'Allowed sources · From'}>
        <AdminTable
          pagination={false}
          enableColumnSelection={false}
          rowKey="key"
          dataSource={(detail.fromRefs || []).map((r, index) => ({
            ...r,
            key: JSON.stringify([r.namespace, r.group, r.kind, index]),
          }))}
          scroll={{ x: 660 }}
          columns={[
            {
              title: zh ? '来源命名空间' : 'Source namespace',
              dataIndex: 'namespace',
              width: 230,
              render: (v: string) => (
                <MetadataTag tone="blue" label={<TableCellText value={v} />} />
              ),
            },
            { title: 'Kind', dataIndex: 'kind', width: 170 },
            {
              title: 'API group',
              dataIndex: 'group',
              width: 260,
              render: (v: string) => <span className="soha-gateway-wrap">{v || 'core'}</span>,
            },
          ]}
          empty={empty(detail.fromRefs !== undefined)}
        />
        {detail.fromRefs === undefined && <GrantSummary labels={detail.from} direction="from" />}
      </Card>
      <Card className="soha-detail-card" title={zh ? '允许目标 · To' : 'Allowed targets · To'}>
        <AdminTable
          pagination={false}
          enableColumnSelection={false}
          rowKey="key"
          dataSource={(detail.toRefs || []).map((r, index) => ({
            ...r,
            key: JSON.stringify([r.group, r.kind, r.name, index]),
          }))}
          scroll={{ x: 660 }}
          columns={[
            {
              title: zh ? '目标名称' : 'Target name',
              key: 'name',
              width: 260,
              render: (_: unknown, r: ReferenceGrantTo) => renderTarget(r),
            },
            { title: 'Kind', dataIndex: 'kind', width: 140 },
            {
              title: 'API group',
              dataIndex: 'group',
              width: 260,
              render: (v: string) => <span className="soha-gateway-wrap">{v || 'core'}</span>,
            },
          ]}
          empty={empty(detail.toRefs !== undefined)}
        />
        <Typography.Paragraph type="secondary" className="soha-grant-note">
          {zh
            ? `目标均位于 ${detail.namespace}；未指定名称时，允许引用该命名空间中相应 Group / Kind 的全部资源。`
            : `All targets are in ${detail.namespace}. An omitted name allows all resources of that Group / Kind in this namespace.`}
        </Typography.Paragraph>
        {detail.toRefs === undefined && <GrantSummary labels={detail.to} direction="to" />}
      </Card>
    </div>
  )
}
