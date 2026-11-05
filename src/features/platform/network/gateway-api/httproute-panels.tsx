import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { Button, Card, Select, Typography, type TableColumnsType } from 'antd'
import { Fragment, type ReactNode } from 'react'
import { WorkloadOverviewConfiguration } from '../../workloads/shared/workload-overview'
import { GatewayConditions } from './gateway-panels'
import type {
  HTTPRouteBackend,
  HTTPRouteFilter,
  HTTPRouteParentView,
  HTTPRouteReference,
  HTTPRouteRuleView,
} from './httproute-configuration'
import './httproute.css'

export type HTTPRouteReferenceRenderer = (ref: HTTPRouteReference) => ReactNode
export function HTTPRouteValues({
  values,
  empty = '—',
  compact = false,
}: {
  values?: readonly string[]
  empty?: string
  compact?: boolean
}) {
  return (
    <div className="soha-gateway-values">
      <div className="soha-gateway-tags">
        {values?.length ? (
          (compact ? values.slice(0, 2) : values).map((value, index) => (
            <MetadataTag key={`${value}/${index}`} label={value} />
          ))
        ) : (
          <Typography.Text type="secondary">{empty}</Typography.Text>
        )}
      </div>
      {compact && values && values.length > 2 && (
        <details>
          <summary>+{values.length - 2}</summary>
          <div className="soha-gateway-tags">
            {values.slice(2).map((value, index) => (
              <MetadataTag key={`${value}/${index}`} label={value} />
            ))}
          </div>
        </details>
      )}
    </div>
  )
}
export function HTTPRouteRulePicker({
  rules,
  selectedKey,
  onSelect,
}: {
  rules: readonly HTTPRouteRuleView[]
  selectedKey?: string
  onSelect: (key: string) => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  return (
    <Select
      className="soha-httproute-rule-picker"
      aria-label={zh ? '选择路由规则' : 'Select route rule'}
      value={selectedKey}
      disabled={!rules.length}
      onChange={onSelect}
      placeholder={zh ? '选择规则' : 'Select a rule'}
      options={rules.map((rule) => ({
        value: rule.key,
        label: `${zh ? '规则' : 'Rule'} ${rule.index + 1}${rule.name ? ` · ${rule.name}` : ''}`,
      }))}
    />
  )
}
export function HTTPRouteParents({
  parents,
  statuses,
  generation,
  reference,
}: {
  parents: readonly HTTPRouteReference[]
  statuses: readonly HTTPRouteParentView[]
  generation?: number
  reference: HTTPRouteReferenceRenderer
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  return (
    <div className="soha-detail-stack">
      <Card
        className="soha-detail-card"
        title={zh ? '配置的父级引用' : 'Configured parent references'}
      >
        <AdminTable
          pagination={false}
          enableColumnSelection={false}
          rowKey={(ref: HTTPRouteReference) =>
            JSON.stringify([ref.sourceLabel, ref.sectionName, ref.port])
          }
          dataSource={[...parents]}
          scroll={{ x: 780 }}
          columns={[
            {
              title: zh ? '父级' : 'Parent',
              key: 'parent',
              width: 230,
              render: (_: unknown, ref: HTTPRouteReference) => reference(ref),
            },
            {
              title: zh ? '命名空间' : 'Namespace',
              dataIndex: 'namespace',
              width: 170,
              render: (value: string) => (
                <span className="soha-gateway-tags">
                  <MetadataTag tone="blue" label={value} />
                </span>
              ),
            },
            {
              title: zh ? '类型 / API 组' : 'Kind / group',
              key: 'kind',
              width: 230,
              render: (_: unknown, ref: HTTPRouteReference) => (
                <span className="soha-gateway-wrap">
                  {ref.kind} · {ref.group || 'core'}
                </span>
              ),
            },
            {
              title: 'SectionName',
              dataIndex: 'sectionName',
              width: 160,
              render: (value?: string) => value || '—',
            },
            {
              title: zh ? '端口' : 'Port',
              dataIndex: 'port',
              width: 90,
              render: (value?: number) => value ?? '—',
            },
          ]}
          empty={
            <ManagementState
              compact
              kind="empty"
              description={
                zh ? '本次未返回可查看的父级引用' : 'No visible parent references returned'
              }
            />
          }
        />
      </Card>
      {statuses.length ? (
        statuses.map((item) => (
          <section key={item.key} className="soha-httproute-parent-status">
            <GatewayConditions
              title={zh ? '父级条件' : 'Parent conditions'}
              conditions={item.conditions}
              generation={generation}
            >
              <div className="soha-httproute-parent-identity">
                <strong>{item.ref ? reference(item.ref) : item.label}</strong>
                {item.ref?.sectionName && (
                  <MetadataTag
                    className="soha-httproute-parent-section"
                    label={`SectionName · ${item.ref.sectionName}`}
                  />
                )}
                {item.ref?.port !== undefined && (
                  <span>
                    {zh ? '端口' : 'Port'} {item.ref.port}
                  </span>
                )}
                <Typography.Text className="soha-gateway-wrap" type="secondary">
                  {item.controllerName || '—'}
                </Typography.Text>
              </div>
            </GatewayConditions>
          </section>
        ))
      ) : (
        <Card className="soha-detail-card">
          <ManagementState
            compact
            kind="empty"
            description={zh ? '本次未返回可查看的父级状态' : 'No visible parent status returned'}
          />
        </Card>
      )}
    </div>
  )
}

function FilterList({ filters }: { filters: readonly HTTPRouteFilter[] }) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  return filters.length ? (
    <div className="soha-httproute-filters">
      {filters.map((filter, index) => (
        <div key={`${filter.type}/${index}`}>
          <MetadataTag tone="blue" label={filter.type} />
          {filter.details.map((line, index) => (
            <code key={index}>{line}</code>
          ))}
        </div>
      ))}
    </div>
  ) : (
    <Typography.Text type="secondary">
      {zh ? '未配置过滤器' : 'No filters configured'}
    </Typography.Text>
  )
}

function BackendResources({
  backend,
  canPods,
  canEndpoints,
  reference,
}: {
  backend: HTTPRouteBackend
  canPods: boolean
  canEndpoints: boolean
  reference: HTTPRouteReferenceRenderer
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const missing = zh ? '本次未返回此项数据' : 'No data returned by this read'
  return (
    <div className="soha-detail-stack">
      {backend.filters.length > 0 && <FilterList filters={backend.filters} />}
      <div>
        <h4>{zh ? '已读端点' : 'Returned endpoints'}</h4>
        {!canEndpoints ? (
          <ManagementState
            compact
            kind="no-permission"
            description={zh ? '没有查看端点的权限' : 'No permission to view endpoints'}
          />
        ) : backend.endpoints === undefined ? (
          <Typography.Text type="secondary">{missing}</Typography.Text>
        ) : (
          <AdminTable
            pagination={false}
            enableColumnSelection={false}
            rowKey={(item: { address: string; targetRef?: string }) =>
              JSON.stringify([item.address, item.targetRef])
            }
            dataSource={backend.endpoints}
            scroll={{ x: 650, y: 280 }}
            columns={[
              { title: zh ? '地址' : 'Address', dataIndex: 'address', width: 200 },
              {
                title: zh ? '就绪' : 'Ready',
                dataIndex: 'ready',
                width: 100,
                render: (value?: boolean) => (
                  <StatusTag
                    value={value === undefined ? 'unknown' : value ? 'ready' : 'warning'}
                    label={
                      value === undefined
                        ? zh
                          ? '未知'
                          : 'Unknown'
                        : value
                          ? zh
                            ? '就绪'
                            : 'Ready'
                          : zh
                            ? '未就绪'
                            : 'Not ready'
                    }
                  />
                ),
              },
              {
                title: zh ? '目标引用' : 'Target reference',
                dataIndex: 'targetRef',
                width: 240,
                render: (value?: string) => (
                  <span className="soha-gateway-wrap">{value || '—'}</span>
                ),
              },
              { title: zh ? '节点' : 'Node', dataIndex: 'nodeName', width: 200 },
            ]}
          />
        )}
      </div>
      <div>
        <h4>{zh ? '已读后端 Pods' : 'Returned backend Pods'}</h4>
        {!canPods ? (
          <ManagementState
            compact
            kind="no-permission"
            description={zh ? '没有查看 Pods 的权限' : 'No permission to view Pods'}
          />
        ) : backend.backendPods === undefined ? (
          <Typography.Text type="secondary">{missing}</Typography.Text>
        ) : (
          <AdminTable
            pagination={false}
            enableColumnSelection={false}
            rowKey={(item: { name: string; namespace: string }) =>
              JSON.stringify([item.namespace, item.name])
            }
            dataSource={backend.backendPods}
            scroll={{ x: 660, y: 280 }}
            columns={[
              {
                title: 'Pod',
                dataIndex: 'name',
                width: 260,
                render: (name: string, item: { namespace: string }) =>
                  reference({
                    name,
                    namespace: item.namespace || backend.namespace || '',
                    kind: 'Pod',
                    group: '',
                    sourceLabel: '',
                  }),
              },
              {
                title: zh ? '状态' : 'Status',
                dataIndex: 'phase',
                width: 130,
                render: (value?: string) => <StatusTag value={value} />,
              },
              { title: zh ? '节点' : 'Node', dataIndex: 'nodeName', width: 240 },
              { title: zh ? '重启' : 'Restarts', dataIndex: 'restarts', width: 90 },
            ]}
          />
        )}
      </div>
    </div>
  )
}

export function HTTPRouteRules({
  rules,
  selectedKey,
  onSelect,
  preview = false,
  onShowAll,
  reference,
  canPods,
  canEndpoints,
}: {
  rules: readonly HTTPRouteRuleView[]
  selectedKey?: string
  onSelect: (key: string) => void
  preview?: boolean
  onShowAll?: () => void
  reference: HTTPRouteReferenceRenderer
  canPods: boolean
  canEndpoints: boolean
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const selected =
    selectedKey === undefined ? rules[0] : rules.find((rule) => rule.key === selectedKey)
  if (preview)
    return (
      <Card
        className="soha-detail-card"
        title={zh ? '路由规则' : 'Route rules'}
        extra={
          <Button type="link" onClick={onShowAll}>
            {zh ? '查看全部' : 'View all'}
          </Button>
        }
      >
        <AdminTable
          pagination={false}
          enableColumnSelection={false}
          rowKey="key"
          dataSource={rules.slice(0, 3)}
          scroll={{ x: 650 }}
          columns={[
            {
              title: zh ? '规则' : 'Rule',
              key: 'rule',
              width: 100,
              render: (_: unknown, rule: HTTPRouteRuleView) => (
                <Button
                  type="link"
                  className="soha-gateway-cell-button"
                  onClick={() => {
                    onSelect(rule.key)
                    onShowAll?.()
                  }}
                >
                  {rule.name || rule.index + 1}
                </Button>
              ),
            },
            {
              title: zh ? '匹配' : 'Match',
              key: 'matches',
              width: 260,
              render: (_: unknown, rule: HTTPRouteRuleView) => (
                <span className="soha-gateway-wrap">
                  {rule.matches.map((group) => group.join(' AND ')).join(' OR ') || '—'}
                </span>
              ),
            },
            {
              title: zh ? '后端' : 'Backends',
              key: 'backends',
              width: 250,
              render: (_: unknown, rule: HTTPRouteRuleView) => (
                <HTTPRouteValues
                  values={rule.backends.map(
                    (b) => `${b.namespace}/${b.name}${b.port !== undefined ? `:${b.port}` : ''}`,
                  )}
                />
              ),
            },
          ]}
        />
      </Card>
    )
  const backendColumns: TableColumnsType<HTTPRouteBackend> = [
    {
      title: zh ? '后端' : 'Backend',
      dataIndex: 'name',
      width: 220,
      render: (_: unknown, b) =>
        reference({
          name: b.name,
          namespace: b.namespace || '',
          kind: b.kind || 'Service',
          group: b.group ?? 'unknown',
          sourceLabel: '',
        }),
    },
    { title: zh ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 170 },
    {
      title: zh ? '类型 / API 组' : 'Kind / group',
      key: 'kind',
      width: 230,
      render: (_: unknown, b) => (
        <span className="soha-gateway-wrap">
          {b.kind || 'Service'} ·{' '}
          {(b.group ?? (selected?.native ? '—' : zh ? '未返回' : 'Not returned')) || 'core'}
        </span>
      ),
    },
    { title: zh ? '端口' : 'Port', dataIndex: 'port', width: 90, render: (v?: number) => v ?? '—' },
    {
      title: zh ? '权重' : 'Weight',
      key: 'weight',
      width: 130,
      render: (_: unknown, b) => (
        <span>
          {b.weight ?? '—'}
          {b.weightDefaulted && (
            <Typography.Text type="secondary">{zh ? '（默认）' : ' (default)'}</Typography.Text>
          )}
        </span>
      ),
    },
  ]
  return (
    <div className="soha-detail-stack">
      <Card
        className="soha-detail-card"
        title={zh ? '规则与后端' : 'Rules and backends'}
        extra={
          <HTTPRouteRulePicker
            rules={rules}
            selectedKey={selected?.key ?? selectedKey}
            onSelect={onSelect}
          />
        }
      >
        {!selected ? (
          <ManagementState
            compact
            kind={selectedKey ? 'not-found' : 'empty'}
            description={
              selectedKey
                ? zh
                  ? '所选规则已不在本次结果中，请重新选择。'
                  : 'The selected rule is no longer returned; select another.'
                : zh
                  ? '本次未返回路由规则'
                  : 'No route rules returned'
            }
          />
        ) : (
          <div className="soha-detail-stack">
            {!selected.native && (
              <Typography.Text type="secondary">
                {zh
                  ? '当前显示接口摘要，完整配置尚未读取。'
                  : 'Showing the API summary; full configuration is not available.'}
              </Typography.Text>
            )}
            <div className="soha-httproute-rule-grid">
              <section>
                <h3>{zh ? '请求匹配' : 'Request matches'}</h3>
                <div className="soha-httproute-matches">
                  {selected.matches.length ? (
                    selected.matches.map((parts, index) => (
                      <Fragment key={index}>
                        {index > 0 && <MetadataTag label="OR" />}
                        <div className="soha-httproute-match">
                          {parts.map((part, offset) => (
                            <div key={offset}>
                              {offset > 0 && <span className="soha-httproute-and">AND</span>}
                              <code>{part}</code>
                            </div>
                          ))}
                        </div>
                      </Fragment>
                    ))
                  ) : (
                    <Typography.Text type="secondary">
                      {zh ? '未返回完整匹配配置' : 'Full match configuration not returned'}
                    </Typography.Text>
                  )}
                </div>
              </section>
              <section>
                <h3>{zh ? '过滤器' : 'Filters'}</h3>
                <FilterList filters={selected.filters} />
              </section>
            </div>
            {selected.settings.length > 0 && (
              <WorkloadOverviewConfiguration
                items={selected.settings.map((item) => ({
                  label: item.label,
                  children: <code className="soha-gateway-wrap">{item.value}</code>,
                }))}
              />
            )}
            <section>
              <h3>{zh ? '后端引用' : 'Backend references'}</h3>
              <AdminTable
                pagination={false}
                enableColumnSelection={false}
                rowKey="key"
                dataSource={selected.backends}
                columns={backendColumns}
                scroll={{ x: 900 }}
                expandable={{
                  expandedRowRender: (backend: HTTPRouteBackend) => (
                    <BackendResources
                      backend={backend}
                      canPods={canPods}
                      canEndpoints={canEndpoints}
                      reference={reference}
                    />
                  ),
                  rowExpandable: (backend: HTTPRouteBackend) =>
                    Boolean(
                      backend.filters.length ||
                      backend.backendPods !== undefined ||
                      backend.endpoints !== undefined,
                    ),
                }}
                empty={
                  <ManagementState
                    compact
                    kind="empty"
                    description={
                      selected.hasBackendRefs === false
                        ? zh
                          ? '此规则未配置 backendRefs'
                          : 'No backendRefs configured for this rule'
                        : zh
                          ? '本次未返回可查看的后端引用'
                          : 'No readable backend references returned'
                    }
                  />
                }
              />
            </section>
          </div>
        )}
      </Card>
    </div>
  )
}
