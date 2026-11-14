import { AdminTable } from '@/components/admin-table'
import { MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Button, Card, Tooltip, Typography, type TableColumnsType } from 'antd'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import { formatResourceQuantity, parseResourceQuantity } from '../shared/resource-quantity'
import type { RuntimeClassDetail } from './runtime-model'
function Help({ title, help }: { title: string; help: string }) {
  return (
    <span className="soha-runtime-card-title">
      {title}
      <Tooltip title={help} trigger={['hover', 'focus', 'click']}>
        <Button
          size="small"
          type="text"
          icon={<QuestionCircleOutlined />}
          aria-label={title + ' info'}
        />
      </Tooltip>
    </span>
  )
}
export function RuntimeClassOverview({ detail }: { detail: RuntimeClassDetail }) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const overhead = Object.entries(detail.podFixed).map(([resource, raw]) => ({ resource, raw })),
    selector = Object.entries(detail.nodeSelector).map(([key, value]) => ({ key, value }))
  const resourceColumns: TableColumnsType<{ resource: string; raw: string }> = [
    {
      title: zh ? '资源' : 'Resource',
      dataIndex: 'resource',
      width: 240,
      render: (resource: string) => <TableCellText value={resource} />,
    },
    {
      title: zh ? '每个 Pod 额外开销' : 'Overhead per Pod',
      dataIndex: 'raw',
      width: 240,
      render: (raw: string, row) => {
        const value = parseResourceQuantity(raw)
        return (
          <Tooltip title={raw}>
            <span className="soha-runtime-number">
              {value !== null && ['cpu', 'memory'].includes(row.resource)
                ? formatResourceQuantity(row.resource, value, zh)
                : raw}
            </span>
          </Tooltip>
        )
      },
    },
  ]
  const selectorColumns: TableColumnsType<{ key: string; value: string }> = [
    {
      title: 'Key',
      dataIndex: 'key',
      width: 260,
      render: (value: string) => <TableCellText value={value === '' ? '""' : value} />,
    },
    {
      title: 'Value',
      dataIndex: 'value',
      width: 260,
      render: (value: string) => <TableCellText value={value === '' ? '""' : value} />,
    },
  ]
  const toleranceColumns: TableColumnsType<RuntimeClassDetail['tolerations'][number]> = [
    {
      title: 'Key',
      dataIndex: 'key',
      width: 220,
      render: (value: string) =>
        value ? <TableCellText value={value} /> : <span>{zh ? '任意键' : 'Any key'}</span>,
    },
    {
      title: zh ? '运算符' : 'Operator',
      dataIndex: 'operator',
      width: 110,
      render: (value: string) => <MetadataTag label={value} />,
    },
    {
      title: 'Value',
      dataIndex: 'value',
      width: 180,
      render: (value: string, row) => (
        <TableCellText value={value === '' ? (row.operator === 'Exists' ? '—' : '""') : value} />
      ),
    },
    {
      title: 'Effect',
      dataIndex: 'effect',
      width: 170,
      render: (value: string) =>
        value ? <MetadataTag label={value} /> : <span>{zh ? '全部效果' : 'All effects'}</span>,
    },
    {
      title: 'tolerationSeconds',
      dataIndex: 'tolerationSeconds',
      width: 160,
      render: (value: number | undefined, row) =>
        value === undefined
          ? row.effect === 'NoExecute'
            ? zh
              ? '无限期'
              : 'Indefinite'
            : '—'
          : value + ' s',
    },
  ]
  return (
    <WorkloadOverview
      aside={
        <Card title={zh ? '运行时配置' : 'Runtime configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '作用范围' : 'Scope', children: zh ? '集群' : 'Cluster' },
              { label: zh ? 'API 版本' : 'API version', children: 'node.k8s.io/v1' },
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
        title={
          <Help
            title={zh ? '运行时 Handler' : 'Runtime handler'}
            help={
              zh
                ? 'Handler 对应节点 CRI 配置。这里仅展示 RuntimeClass 声明，不代表节点已安装或验证可用。'
                : 'The handler refers to node CRI configuration. This shows the RuntimeClass declaration, not verified node support.'
            }
          />
        }
      >
        <Typography.Text className="soha-runtime-handler" copyable>
          {detail.handler}
        </Typography.Text>
      </Card>
      <Card
        title={
          <Help
            title="Pod overhead"
            help={
              zh
                ? '使用此 RuntimeClass 的每个 Pod 的额外资源开销，供调度与资源核算；不是实时使用量。未配置不补成 0。'
                : 'Additional per-Pod resource overhead used for scheduling and accounting, not live usage. Unconfigured values are not displayed as zero.'
            }
          />
        }
      >
        {overhead.length ? (
          <AdminTable
            columns={resourceColumns}
            dataSource={overhead}
            rowKey="resource"
            pagination={false}
            enableColumnSelection={false}
            tableSize="small"
            scroll={{ x: 480 }}
          />
        ) : (
          <span className="soha-runtime-muted">
            {zh ? '未配置额外开销' : 'No overhead configured'}
          </span>
        )}
      </Card>
      <Card
        title={
          <Help
            title={zh ? '节点选择器' : 'Node selector'}
            help={
              zh
                ? '这些条件会与 Pod 自身的节点选择器合并；不表示已检查哪些节点支持此运行时。'
                : 'These requirements are combined with Pod node selectors. No node runtime support inventory is inferred.'
            }
          />
        }
      >
        {selector.length ? (
          <AdminTable
            columns={selectorColumns}
            dataSource={selector}
            rowKey="key"
            pagination={false}
            enableColumnSelection={false}
            tableSize="small"
            scroll={{ x: 520 }}
          />
        ) : (
          <span className="soha-runtime-muted">
            {zh ? '未配置节点选择器' : 'No node selector configured'}
          </span>
        )}
      </Card>
      <Card
        title={
          <Help
            title={zh ? '污点容忍' : 'Tolerations'}
            help={
              zh
                ? '此处的容忍项会与 Pod 自身配置合并；容忍污点不代表一定调度到该节点。tolerationSeconds 仅对 NoExecute 生效，未设置时无限期。'
                : 'Tolerations are combined with Pod settings and do not guarantee placement. tolerationSeconds applies only to NoExecute and is indefinite when omitted.'
            }
          />
        }
      >
        {detail.tolerations.length ? (
          <AdminTable
            columns={toleranceColumns}
            dataSource={detail.tolerations.map((row, index) => ({ ...row, rowIndex: index }))}
            rowKey="rowIndex"
            pagination={false}
            enableColumnSelection={false}
            tableSize="small"
            scroll={{ x: 840 }}
          />
        ) : (
          <span className="soha-runtime-muted">
            {zh ? '未配置污点容忍' : 'No tolerations configured'}
          </span>
        )}
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
