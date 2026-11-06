import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
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
import { formatResourceQuantity, parseResourceQuantity } from '../shared/resource-quantity'
import { limitFields, limitGroups, type LimitField, type LimitRow } from './limit-model'
import type { LimitRangeResource } from './types'

function LimitHelp({ label, help }: { label: string; help: string }) {
  const { localeCode } = useI18n()
  return (
    <span className="soha-limitrange-card-title">
      {label}
      <Tooltip title={help} trigger={['hover', 'focus', 'click']}>
        <Button
          type="text"
          size="small"
          icon={<QuestionCircleOutlined />}
          aria-label={label + (localeCode === 'zh_CN' ? '说明' : ' information')}
        />
      </Tooltip>
    </span>
  )
}

export function LimitRangeOverview({ detail }: { detail: LimitRangeResource }) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const groups = limitGroups(detail.rules || [])
  const fieldLabels: Record<LimitField, string> = {
    min: zh ? '最小值 Min' : 'Min',
    max: zh ? '最大值 Max' : 'Max',
    defaultRequest: zh ? '默认请求' : 'Default request',
    default: zh ? '默认限制' : 'Default limit',
    maxLimitRequestRatio: zh ? '最大 Limit / Request' : 'Max limit/request ratio',
  }
  const help = (type: string) =>
    type === 'Container'
      ? zh
        ? '按单个容器约束资源；未声明的请求或限制可使用默认值。比例为无单位的 Limit / Request，破折号表示未设置。'
        : 'Constraints and defaults apply to each container. Limit/request ratios are dimensionless. A dash means not set.'
      : type === 'Pod'
        ? zh
          ? 'Pod 级资源约束，不代表命名空间资源总量或实时用量。破折号表示未设置。'
          : 'Pod-level resource constraints, not namespace totals or live usage. A dash means not set.'
        : type === 'PersistentVolumeClaim'
          ? zh
            ? '每个 PVC 的存储请求上下限，不代表已用空间；这里不会自动创建存储卷。'
            : 'Storage request bounds for each PVC, not used space. No volume is created here.'
          : zh
            ? '展示接口返回的限制字段。破折号表示未设置。'
            : 'Limits returned by the API. A dash means not set.'
  const cell = (row: LimitRow, field: LimitField) => {
    const raw = row.rule[field]?.[row.resource]
    if (raw === undefined) return <span className="soha-limitrange-muted">—</span>
    const value = parseResourceQuantity(raw)
    const display =
      value === null
        ? raw
        : field === 'maxLimitRequestRatio'
          ? new Intl.NumberFormat('en-US', { maximumSignificantDigits: 6 }).format(value) + '×'
          : formatResourceQuantity(row.resource, value, zh)
    return (
      <Tooltip title={raw}>
        <span className="soha-limitrange-number">{display}</span>
      </Tooltip>
    )
  }
  return (
    <WorkloadOverview
      aside={
        <Card
          title={
            <LimitHelp
              label={zh ? '限制配置' : 'Limit configuration'}
              help={
                zh
                  ? '这是准入规则，不是实时用量。新增或修改 LimitRange 不会改写已经运行的 Pod。'
                  : 'These are admission rules, not live usage. Changing a LimitRange does not rewrite running Pods.'
              }
            />
          }
        >
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '作用范围说明' : 'Scope semantics',
                children: zh
                  ? '规则在当前命名空间的资源准入时应用；默认值和限制不是对已有资源的依赖或改写。'
                  : 'Rules apply during resource admission in this namespace. Defaults and limits do not imply dependencies or rewrite existing resources.',
              },
              {
                label: zh ? '命名空间' : 'Namespace',
                children: (
                  <Tooltip title={detail.namespace} trigger={['hover', 'focus', 'click']}>
                    <span className="soha-limitrange-namespace-trigger" tabIndex={0}>
                      <MetadataTag
                        tone="blue"
                        className="soha-limitrange-namespace"
                        label={<span>{detail.namespace}</span>}
                      />
                    </span>
                  </Tooltip>
                ),
              },
              { label: zh ? '规则数' : 'Rules', children: detail.limits },
              {
                label: zh ? '适用类型' : 'Types',
                children:
                  detail.rules == null ? (
                    zh ? (
                      '未提供'
                    ) : (
                      'Not provided'
                    )
                  ) : groups.length ? (
                    <div className="soha-limitrange-types">
                      {groups.map((group) => (
                        <MetadataTag key={group.type} label={group.type} />
                      ))}
                    </div>
                  ) : (
                    '—'
                  ),
              },
              {
                label: zh ? '创建时间' : 'Created',
                children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
              },
            ]}
          />
        </Card>
      }
    >
      {detail.rules == null ? (
        <ManagementState
          kind="empty"
          title={zh ? '未返回限制规则' : 'Limit rules were not returned'}
          description={
            zh
              ? '可在 YAML 页查看当前资源配置。'
              : 'Open YAML to inspect the current configuration.'
          }
        />
      ) : !groups.length ? (
        <ManagementState
          kind="empty"
          title={zh ? '没有限制规则' : 'No limit rules'}
          description={zh ? '当前返回的规则列表为空。' : 'The returned rule list is empty.'}
        />
      ) : (
        groups.map((group) => {
          const fields = limitFields.filter(
            (field) =>
              group.type === 'Container' ||
              field === 'min' ||
              field === 'max' ||
              group.rules.some((rule) => Object.keys(rule[field] || {}).length > 0),
          )
          const columns: TableColumnsType<LimitRow> = [
            ...(group.rules.length > 1
              ? [{ title: zh ? '规则' : 'Rule', dataIndex: 'index', width: 80 }]
              : []),
            {
              title: zh ? '资源' : 'Resource',
              dataIndex: 'resource',
              width: 180,
              render: (value: string) => <TableCellText value={value} />,
            },
            ...fields.map((field) => ({
              title: fieldLabels[field],
              key: field,
              width: field === 'maxLimitRequestRatio' ? 180 : 140,
              render: (_: unknown, row: LimitRow) => cell(row, field),
            })),
          ]
          return (
            <Card
              key={group.type}
              title={<LimitHelp label={group.type} help={help(group.type)} />}
              extra={
                <span>{zh ? group.rules.length + ' 条规则' : group.rules.length + ' rules'}</span>
              }
            >
              <AdminTable
                columns={columns}
                dataSource={group.rows}
                pagination={false}
                rowKey="key"
                enableColumnSelection={false}
                tableSize="small"
                scroll={{
                  x: columns.reduce((sum, column) => sum + Number(column.width || 140), 0),
                }}
                empty={
                  <span>
                    {zh ? '未提供具体资源限制' : 'No resource limit values were provided'}
                  </span>
                }
              />
            </Card>
          )
        })
      )}
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
