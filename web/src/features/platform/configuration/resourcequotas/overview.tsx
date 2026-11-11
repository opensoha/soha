import { AdminTable } from '@/components/admin-table'
import { MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { QuestionCircleOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { Button, Card, Tooltip } from 'antd'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import type { quotaConfiguration } from './configuration'
import { formatQuotaAmount, quotaQuantity, quotaRows, type QuotaRow } from './quota-model'
import { QuotaUsageBar } from './quota-usage'
import type { ResourceQuotaResource } from './types'

function QuotaCardTitle({ label, help }: { label: string; help: string }) {
  return (
    <span className="soha-quota-card-title">
      {label}
      <Tooltip title={help} trigger={['hover', 'focus', 'click']}>
        <Button type="text" size="small" icon={<QuestionCircleOutlined />} aria-label={help} />
      </Tooltip>
    </span>
  )
}

export function QuotaOverview({
  detail,
  configuration,
}: {
  detail: ResourceQuotaResource
  configuration?: ReturnType<typeof quotaConfiguration>
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const rows = quotaRows(detail, configuration?.hard)
  const hasConfigDifference = rows.some(
    (row) => row.configuredRaw !== undefined && quotaQuantity(row.configuredRaw) !== row.hard,
  )
  const amount = (row: QuotaRow, value: number | null, raw?: string) =>
    value === null ? (
      <span className="soha-quota-muted" title={raw}>
        —
      </span>
    ) : (
      <span className="soha-quota-number" title={raw}>
        {formatQuotaAmount(row.resource, value, zh)}
      </span>
    )
  const columns: TableColumnsType<QuotaRow> = [
    {
      title: zh ? '资源' : 'Resource',
      dataIndex: 'resource',
      width: 220,
      render: (value: string) => <TableCellText value={value} />,
    },
    {
      title: zh ? '已用 Used' : 'Used',
      key: 'used',
      width: 140,
      render: (_, row) => amount(row, row.used, row.usedRaw),
    },
    {
      title: zh ? '生效上限 Hard' : 'Enforced hard',
      key: 'hard',
      width: 150,
      render: (_, row) => amount(row, row.hard, row.hardRaw),
    },
    {
      title: zh ? '剩余' : 'Remaining',
      key: 'remaining',
      width: 150,
      render: (_, row) =>
        row.remaining === null ? (
          '—'
        ) : (
          <span className={row.over ? 'soha-quota-number is-over' : 'soha-quota-number'}>
            {row.over ? (zh ? '超出 ' : 'Over by ') : ''}
            {formatQuotaAmount(row.resource, Math.abs(row.remaining), zh)}
          </span>
        ),
    },
    {
      title: zh ? '占用比例' : 'Usage',
      key: 'percent',
      width: 220,
      render: (_, row) => <QuotaUsageBar row={row} />,
    },
    ...(hasConfigDifference
      ? [
          {
            title: zh ? '配置上限' : 'Configured hard',
            key: 'configured',
            width: 150,
            render: (_: unknown, row: QuotaRow) =>
              amount(row, quotaQuantity(row.configuredRaw), row.configuredRaw),
          },
        ]
      : []),
  ]
  const scopes = configuration?.scopes ?? detail.scopes ?? []
  return (
    <WorkloadOverview
      aside={
        <Card
          title={
            <QuotaCardTitle
              label={zh ? '配额配置' : 'Quota configuration'}
              help={
                zh
                  ? 'Scopes 与范围选择器的全部条件同时满足时，资源才计入此配额。未设置范围条件时，配额适用于整个命名空间。'
                  : 'Resources count toward this quota only when all scopes and scope-selector conditions match. Without scope conditions, the quota applies to the whole namespace.'
              }
            />
          }
        >
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '作用范围说明' : 'Scope semantics',
                children: zh
                  ? '命名空间内满足 Scopes 与 scopeSelector 的资源计入配额；这是作用范围，不表示对象依赖。'
                  : 'Resources in this namespace count toward the quota when Scopes and scopeSelector match. This is policy scope, not an object dependency.',
              },
              {
                label: zh ? '命名空间' : 'Namespace',
                children: (
                  <Tooltip title={detail.namespace} trigger={['hover', 'focus', 'click']}>
                    <span className="soha-quota-namespace-trigger" tabIndex={0}>
                      <MetadataTag
                        tone="blue"
                        className="soha-quota-namespace"
                        label={
                          <span className="soha-quota-namespace-text">{detail.namespace}</span>
                        }
                      />
                    </span>
                  </Tooltip>
                ),
              },
              { label: zh ? '资源项' : 'Resources', children: rows.length },
              {
                label: 'Scopes',
                children: scopes.length ? (
                  <div className="soha-quota-scope-terms">
                    {scopes.map((scope) => (
                      <MetadataTag key={scope} label={scope} />
                    ))}
                  </div>
                ) : configuration ? (
                  zh ? (
                    '未限定'
                  ) : (
                    'Not restricted'
                  )
                ) : zh ? (
                  '未提供完整范围'
                ) : (
                  'Full scope unavailable'
                ),
              },
              ...(configuration
                ? [
                    {
                      label: zh ? '范围选择器' : 'Scope selector',
                      children: configuration.expressions.length ? (
                        <div className="soha-quota-scope-terms">
                          {configuration.expressions.map((term, i) => (
                            <span key={i}>
                              {term.scopeName} {term.operator}
                              {term.values.length ? ' (' + term.values.join(', ') + ')' : ''}
                            </span>
                          ))}
                        </div>
                      ) : zh ? (
                        '未设置'
                      ) : (
                        'Not set'
                      ),
                    },
                  ]
                : []),
              {
                label: zh ? '创建时间' : 'Created',
                children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
              },
            ]}
          />
        </Card>
      }
    >
      <Card
        title={
          <QuotaCardTitle
            label={zh ? '配额用量' : 'Quota usage'}
            help={
              zh
                ? 'Kubernetes 配额统计，不代表 CPU / 内存实时使用量。占比按已生效的 Hard 上限计算。'
                : 'Kubernetes quota accounting, not live CPU or memory usage. Ratios use the enforced Hard limits.'
            }
          />
        }
        extra={<span>{zh ? rows.length + ' 项资源' : rows.length + ' resources'}</span>}
      >
        {hasConfigDifference ? (
          <p className="soha-quota-note">
            {zh
              ? '配置上限与已生效状态不同，请等待控制器同步；当前占比按生效上限计算。'
              : 'Configured limits differ from observed enforcement. Ratios use the enforced hard limits.'}
          </p>
        ) : null}
        <AdminTable
          columns={columns}
          dataSource={rows}
          pagination={false}
          rowKey="resource"
          tableSize="small"
          enableColumnSelection={false}
          scroll={{ x: hasConfigDifference ? 1040 : 900 }}
          empty={<span>{zh ? '尚未返回配额状态' : 'Quota status has not been returned'}</span>}
        />
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
