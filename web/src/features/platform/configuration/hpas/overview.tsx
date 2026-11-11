import { AdminTable } from '@/components/admin-table'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import type { WorkloadCondition } from '@/types'
import { formatDateTime } from '@/utils/time'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Button, Card, Tooltip, type TableColumnsType } from 'antd'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewFacts,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import { parseResourceQuantity, formatResourceQuantity } from '../shared/resource-quantity'
import {
  count,
  hpaConditionTone,
  object,
  text,
  type HPAConfiguration,
  type HPAMetricRow,
} from './hpa-model'
import { HPATargetLink } from './target-link'
import type { HorizontalPodAutoscalerResource } from './types'
function HelpTitle({ title, help }: { title: string; help: string }) {
  return (
    <span className="soha-hpa-card-title">
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
function metricAmount(row: HPAMetricRow, value: string | undefined, zh: boolean) {
  if (value === undefined || value === '') return '—'
  if (row.raw) return value
  if (row.mode === 'Utilization') return value + '%'
  const name = row.name.split('/').at(-1) || ''
  if (['Resource', 'ContainerResource'].includes(row.type)) {
    const amount = parseResourceQuantity(value)
    if (amount !== null) return formatResourceQuantity(name, amount, zh)
  }
  return value
}
function Conditions({ conditions }: { conditions: WorkloadCondition[] }) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const columns: TableColumnsType<WorkloadCondition> = [
    { title: zh ? '条件' : 'Condition', dataIndex: 'type', width: 170 },
    {
      title: zh ? '状态' : 'Status',
      width: 95,
      render: (_, row) => <StatusTag value={hpaConditionTone(row)} label={row.status} />,
    },
    { title: zh ? '原因' : 'Reason', dataIndex: 'reason', width: 210 },
    { title: zh ? '消息' : 'Message', dataIndex: 'message' },
    {
      title: zh ? '最近变化' : 'Last transition',
      dataIndex: 'lastTransitionTime',
      width: 170,
      render: (value?: string) => (value ? formatDateTime(value) : '—'),
    },
  ]
  return (
    <Card title={zh ? '扩缩条件' : 'Scaling conditions'}>
      <details className="soha-hpa-conditions">
        <summary>
          <span>
            {conditions.length
              ? conditions.map((condition) => (
                  <span key={condition.type}>
                    {condition.type}{' '}
                    <StatusTag value={hpaConditionTone(condition)} label={condition.status} />
                  </span>
                ))
              : zh
                ? '未返回扩缩条件'
                : 'No scaling conditions returned'}
          </span>
        </summary>
        <AdminTable
          columns={columns}
          dataSource={conditions}
          rowKey="type"
          pagination={false}
          enableColumnSelection={false}
          scroll={{ x: 960 }}
          tableSize="small"
        />
      </details>
    </Card>
  )
}
function Behavior({ configuration }: { configuration: HPAConfiguration }) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  return (
    <Card
      title={
        <HelpTitle
          title={zh ? '扩缩策略' : 'Scaling behavior'}
          help={
            zh
              ? '展示显式配置；未配置项使用控制器默认策略。Disabled 表示该方向禁止扩缩。'
              : 'Explicit configuration only; unspecified fields use controller defaults. Disabled prevents scaling in that direction.'
          }
        />
      }
    >
      {!configuration.behavior ? (
        <span className="soha-hpa-muted">
          {zh
            ? '未显式配置，使用控制器默认策略'
            : 'Not explicitly configured; controller defaults apply'}
        </span>
      ) : (
        <div className="soha-hpa-behavior">
          {(['scaleUp', 'scaleDown'] as const).map((direction) => {
            const rules = object(configuration.behavior?.[direction]),
              policies = Array.isArray(rules.policies) ? rules.policies : []
            return (
              <section key={direction}>
                <h4>
                  {direction === 'scaleUp'
                    ? zh
                      ? '扩容'
                      : 'Scale up'
                    : zh
                      ? '缩容'
                      : 'Scale down'}
                </h4>
                <WorkloadOverviewConfiguration
                  items={[
                    {
                      label: zh ? '稳定窗口' : 'Stabilization',
                      children:
                        count(rules.stabilizationWindowSeconds) === undefined
                          ? '—'
                          : String(rules.stabilizationWindowSeconds) + (zh ? ' 秒' : ' s'),
                    },
                    {
                      label: zh ? '策略选择' : 'Selection',
                      children: text(rules.selectPolicy) || '—',
                    },
                    ...(rules.tolerance === undefined
                      ? []
                      : [
                          {
                            label: zh ? '容差' : 'Tolerance',
                            children: text(rules.tolerance) || '—',
                          },
                        ]),
                  ]}
                />
                <ul>
                  {policies.map((raw, index) => {
                    const policy = object(raw)
                    return (
                      <li key={index}>
                        {text(policy.type) || '—'} · {text(policy.value) || '—'}
                        {policy.type === 'Percent' ? '%' : ''} / {text(policy.periodSeconds) || '—'}{' '}
                        s
                      </li>
                    )
                  })}
                </ul>
              </section>
            )
          })}
        </div>
      )}
    </Card>
  )
}
export function HPAOverview({
  detail,
  configuration,
  clusterId,
}: {
  detail: HorizontalPodAutoscalerResource
  configuration?: HPAConfiguration
  clusterId: string
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const conditions = configuration?.conditions ?? detail.conditions ?? []
  const current = configuration ? configuration.currentReplicas : detail.currentReplicas,
    desired = configuration ? configuration.desiredReplicas : detail.desiredReplicas
  const min = configuration?.minReplicas ?? detail.minReplicas,
    max = configuration?.maxReplicas ?? detail.maxReplicas
  const metrics: HPAMetricRow[] =
    configuration?.metrics ??
    (detail.metrics || []).map((metric, index) => ({
      id: String(index),
      name: metric.name || '—',
      type: metric.type,
      mode: 'raw',
      target: metric.target,
      current: metric.current,
      raw: true,
    }))
  const columns: TableColumnsType<HPAMetricRow> = [
    {
      title: zh ? '指标' : 'Metric',
      dataIndex: 'name',
      width: 210,
      render: (name: string, row) => (
        <div>
          <TableCellText value={name} />
          {row.selector ? (
            <small className="soha-hpa-selector">
              <TableCellText value={row.selector} />
            </small>
          ) : null}
        </div>
      ),
    },
    {
      title: zh ? '来源' : 'Source',
      dataIndex: 'type',
      width: 150,
      render: (type: string) => <MetadataTag label={type} />,
    },
    {
      title: zh ? '目标类型' : 'Target type',
      dataIndex: 'mode',
      width: 160,
      render: (mode: string) =>
        ({
          Utilization: zh ? '请求利用率' : 'Request utilization',
          AverageValue: zh ? '平均值' : 'Average value',
          Value: zh ? '总值' : 'Value',
          raw: zh ? '接口原始值' : 'API raw value',
        })[mode] ||
        mode ||
        '—',
    },
    {
      title: zh ? '当前' : 'Current',
      width: 140,
      render: (_, row) => (
        <span className="soha-hpa-number">{metricAmount(row, row.current, zh)}</span>
      ),
    },
    {
      title: zh ? '目标' : 'Target',
      width: 140,
      render: (_, row) => (
        <span className="soha-hpa-number">{metricAmount(row, row.target, zh)}</span>
      ),
    },
  ]
  return (
    <WorkloadOverview
      aside={
        <Card title={zh ? '扩缩配置' : 'Autoscaling configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '命名空间' : 'Namespace',
                children: (
                  <Tooltip title={detail.namespace} trigger={['hover', 'focus', 'click']}>
                    <span tabIndex={0} className="soha-hpa-namespace-trigger">
                      <MetadataTag
                        tone="blue"
                        className="soha-hpa-namespace"
                        label={<span>{detail.namespace}</span>}
                      />
                    </span>
                  </Tooltip>
                ),
              },
              {
                label: zh ? '扩缩目标' : 'Scale target',
                children: configuration ? (
                  <HPATargetLink
                    target={configuration.target}
                    namespace={detail.namespace}
                    clusterId={clusterId}
                  />
                ) : (
                  <TableCellText value={detail.targetRef} />
                ),
              },
              {
                label: zh ? 'API 版本' : 'API version',
                children: configuration?.target.apiVersion || '—',
              },
              {
                label: zh ? '副本范围' : 'Replica range',
                children: (
                  <>
                    {min ?? '—'} – {max ?? '—'}
                    {configuration?.minDefaulted ? (
                      <small className="soha-hpa-muted">
                        {zh ? '（最小值默认）' : ' (default minimum)'}
                      </small>
                    ) : null}
                  </>
                ),
              },
              {
                label: zh ? '最近扩缩' : 'Last scale',
                children: configuration?.lastScaleTime
                  ? formatDateTime(configuration.lastScaleTime)
                  : '—',
              },
              { label: zh ? '配置代次' : 'Generation', children: configuration?.generation ?? '—' },
              {
                label: zh ? '已观测代次' : 'Observed generation',
                children: configuration?.observedGeneration ?? '—',
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
      <Card title={zh ? '副本与扩缩' : 'Replicas and scaling'}>
        <WorkloadOverviewFacts
          facts={[
            { key: 'current', label: zh ? '当前副本' : 'Current replicas', value: current },
            { key: 'desired', label: zh ? '期望副本' : 'Desired replicas', value: desired },
            { key: 'min', label: zh ? '最小副本' : 'Min replicas', value: min },
            { key: 'max', label: zh ? '最大副本' : 'Max replicas', value: max },
          ]}
        />
      </Card>
      <Card
        title={
          <HelpTitle
            title={zh ? '扩缩指标' : 'Scaling metrics'}
            help={
              zh
                ? '当前值来自 HPA 控制器状态，不是 Prometheus 趋势。利用率相对资源请求；破折号表示未返回采样，不代表 0。'
                : 'Current values come from HPA controller status, not Prometheus history. Utilization is relative to resource requests. A dash means no sample returned, not zero.'
            }
          />
        }
        extra={metrics.length}
      >
        <AdminTable
          columns={columns}
          dataSource={metrics}
          rowKey="id"
          enableColumnSelection={false}
          pagination={false}
          tableSize="small"
          scroll={{ x: 800 }}
          empty={zh ? '未返回扩缩指标' : 'No scaling metrics returned'}
        />
      </Card>
      {configuration ? <Behavior configuration={configuration} /> : null}
      <Conditions conditions={conditions} />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
