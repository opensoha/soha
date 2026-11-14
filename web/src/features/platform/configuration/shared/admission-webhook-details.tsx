import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Button, Card, Select, Tooltip, Typography, type TableColumnsType } from 'antd'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { WorkloadOverviewConfiguration } from '../../workloads/shared/workload-overview'
import {
  webhookTarget,
  type AdmissionWebhookDefinition,
  type AdmissionWebhookDetail,
  type WebhookSelector,
} from './admission-webhook-model'

function HelpTitle({ title, help }: { title: string; help: string }) {
  return (
    <span className="soha-admission-help-title">
      {title}
      <Tooltip title={help} trigger={['hover', 'focus', 'click']}>
        <Button
          type="text"
          size="small"
          icon={<QuestionCircleOutlined />}
          aria-label={(title || 'CA bundle') + ' info'}
        />
      </Tooltip>
    </span>
  )
}
function SelectorDetails({
  title,
  selector,
  zh,
}: {
  title: string
  selector?: WebhookSelector
  zh: boolean
}) {
  const rows = [
    ...Object.entries(selector?.matchLabels || {}).map(([key, value]) => ({
      key,
      operator: '=',
      value: value === '' ? '""' : value,
    })),
    ...(selector?.matchExpressions || []).map((item) => ({
      key: item.key,
      operator: item.operator,
      value: item.values.length ? item.values.map((v) => (v === '' ? '""' : v)).join(', ') : '—',
    })),
  ].map((item, index) => ({ ...item, index }))
  const columns: TableColumnsType<(typeof rows)[number]> = [
    {
      title: 'Key',
      dataIndex: 'key',
      width: 240,
      render: (value: string) => <TableCellText value={value} />,
    },
    { title: zh ? '运算符' : 'Operator', dataIndex: 'operator', width: 140 },
    {
      title: 'Values',
      dataIndex: 'value',
      width: 260,
      render: (value: string) => <TableCellText value={value} />,
    },
  ]
  return (
    <Card title={title}>
      {rows.length ? (
        <AdminTable
          columns={columns}
          dataSource={rows}
          rowKey="index"
          pagination={false}
          tableSize="small"
          enableColumnSelection={false}
          scroll={{ x: 600 }}
          tableLayout="fixed"
        />
      ) : (
        <span className="soha-admission-muted">
          {selector
            ? zh
              ? '空选择器：不按此标签条件过滤'
              : 'Empty selector: no filtering by this label selector'
            : zh
              ? '未设置此选择器'
              : 'Selector not configured'}
        </span>
      )}
    </Card>
  )
}
export function AdmissionWebhookDetails({ detail }: { detail: AdmissionWebhookDetail }) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const [params, setParams] = useSearchParams(),
    requested = params.get('webhook')
  const selected = requested
    ? detail.webhooks.find((item) => item.name === requested)
    : detail.webhooks[0]
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const { clusterId } = usePlatformScopeStore(),
    navigate = useNavigate()
  const choose = (name: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        next.set('webhook', name)
        return next
      },
      { replace: true },
    )
  const service = selected?.service
  const policy = (value?: string) => value || (zh ? '未声明' : 'Not declared')
  const ruleColumns: TableColumnsType<AdmissionWebhookDefinition['rules'][number]> = [
    {
      title: zh ? '操作' : 'Operations',
      dataIndex: 'operations',
      width: 190,
      render: (items: string[]) => <TableCellText value={items.join(', ')} />,
    },
    {
      title: 'API groups',
      dataIndex: 'apiGroups',
      width: 210,
      render: (items: string[]) => (
        <TableCellText value={items.map((value) => value || 'core').join(', ')} />
      ),
    },
    {
      title: 'API versions',
      dataIndex: 'apiVersions',
      width: 140,
      render: (items: string[]) => <TableCellText value={items.join(', ')} />,
    },
    {
      title: zh ? '资源' : 'Resources',
      dataIndex: 'resources',
      width: 240,
      render: (items: string[]) => <TableCellText value={items.join(', ')} />,
    },
    {
      title: zh ? '作用域' : 'Scope',
      dataIndex: 'scope',
      width: 130,
      render: (value?: string) => policy(value),
    },
  ]
  return (
    <div className="soha-admission-webhooks">
      <div className="soha-admission-chooser">
        <label htmlFor="admission-webhook-select">Webhook</label>
        <Select
          id="admission-webhook-select"
          showSearch={{ optionFilterProp: 'label' }}
          value={selected?.name}
          onChange={choose}
          placeholder={zh ? '搜索并选择 Webhook' : 'Search and select webhook'}
          options={detail.webhooks.map((item) => ({
            label: item.name + ' · ' + webhookTarget(item),
            value: item.name,
          }))}
        />
        <span className="soha-admission-muted">{detail.webhooks.length} Webhooks</span>
      </div>
      {!selected ? (
        <ManagementState
          kind={requested ? 'not-found' : 'empty'}
          description={
            requested
              ? zh
                ? '当前配置中未找到所选 Webhook，请重新选择'
                : 'Selected webhook is no longer in this configuration; select another'
              : zh
                ? '未配置 Webhook'
                : 'No webhooks configured'
          }
        />
      ) : (
        <>
          <Card title={selected.name}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '目标类型' : 'Target type',
                  children: <MetadataTag label={service ? 'Service' : 'URL'} />,
                },
                ...(service
                  ? [
                      {
                        label: 'Service',
                        children: hasPermission(permissions, 'platform.network.services.view') ? (
                          <TableCellLink
                            label={service.namespace + '/' + service.name}
                            onClick={() =>
                              navigate(
                                '/network/services/' +
                                  encodeURIComponent(service.name) +
                                  '?' +
                                  new URLSearchParams({
                                    clusterId: clusterId || '',
                                    namespace: service.namespace,
                                  }),
                              )
                            }
                          />
                        ) : (
                          service.namespace + '/' + service.name
                        ),
                      },
                      {
                        label: zh ? '路径' : 'Path',
                        children: service.path === undefined ? '—' : service.path || '""',
                      },
                      { label: zh ? '端口' : 'Port', children: service.port ?? '—' },
                    ]
                  : [
                      {
                        label: 'URL',
                        children: <Typography.Text copyable>{selected.url}</Typography.Text>,
                      },
                    ]),
                {
                  label: 'CA bundle',
                  children: (
                    <span className="soha-admission-ca">
                      <MetadataTag
                        label={
                          selected.caBundleConfigured
                            ? zh
                              ? '已配置'
                              : 'Configured'
                            : zh
                              ? '未配置'
                              : 'Not configured'
                        }
                      />
                      <HelpTitle
                        title=""
                        help={
                          zh
                            ? '仅表示配置了 CA 内容，不表示证书已验证、未过期或回调可达；本页不会探测回调。'
                            : 'Configured CA content does not prove certificate validity, expiry or callback reachability. This page does not probe callbacks.'
                        }
                      />
                    </span>
                  ),
                },
              ]}
            />
          </Card>
          <Card title={zh ? '调用策略' : 'Invocation policies'}>
            <WorkloadOverviewConfiguration
              items={[
                { label: 'FailurePolicy', children: policy(selected.failurePolicy) },
                { label: 'MatchPolicy', children: policy(selected.matchPolicy) },
                { label: 'SideEffects', children: policy(selected.sideEffects) },
                {
                  label: zh ? '超时' : 'Timeout',
                  children: selected.timeoutSeconds == null ? '—' : selected.timeoutSeconds + ' s',
                },
                {
                  label: 'Review versions',
                  children: selected.admissionReviewVersions.join(', ') || '—',
                },
                ...(detail.configurationKind === 'MutatingWebhookConfiguration'
                  ? [{ label: 'ReinvocationPolicy', children: policy(selected.reinvocationPolicy) }]
                  : []),
              ]}
            />
          </Card>
          <Card title={zh ? 'API 匹配规则' : 'API matching rules'}>
            <AdminTable
              columns={ruleColumns}
              dataSource={selected.rules.map((item, index) => ({ ...item, index }))}
              rowKey="index"
              pagination={false}
              tableSize="small"
              enableColumnSelection={false}
              tableLayout="fixed"
              scroll={{ x: 900 }}
              empty={
                <ManagementState
                  kind="empty"
                  compact
                  bordered={false}
                  description={zh ? '未配置 API 匹配规则' : 'No API matching rules configured'}
                />
              }
            />
          </Card>
          <SelectorDetails
            title="Namespace selector"
            selector={selected.namespaceSelector}
            zh={zh}
          />
          <SelectorDetails title="Object selector" selector={selected.objectSelector} zh={zh} />
          <Card
            title={
              <HelpTitle
                title={zh ? 'CEL 匹配条件' : 'CEL match conditions'}
                help={
                  zh
                    ? '保留 API 返回的条件表达式，本页不执行 CEL 或模拟准入请求。'
                    : 'Expressions are displayed as returned. This page does not evaluate CEL or simulate admission requests.'
                }
              />
            }
          >
            {selected.matchConditions.length ? (
              <div className="soha-admission-conditions">
                {selected.matchConditions.map((item) => (
                  <section key={item.name}>
                    <strong>{item.name}</strong>
                    <Typography.Paragraph copyable className="soha-admission-expression">
                      {item.expression}
                    </Typography.Paragraph>
                  </section>
                ))}
              </div>
            ) : (
              <span className="soha-admission-muted">
                {zh ? '未配置附加 CEL 条件' : 'No additional CEL conditions configured'}
              </span>
            )}
          </Card>
        </>
      )}
    </div>
  )
}
