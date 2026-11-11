import { AdminTable } from '@/components/admin-table'
import { ManagementKeywordField, ManagementState } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { WorkloadOverviewConfiguration } from '../../workloads/shared/workload-overview'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { Button, Card, Select, Typography, type TableColumnsType } from 'antd'
import { useState, type ReactNode } from 'react'
import {
  staleGatewayCondition,
  type GatewayCondition,
  type GatewayListenerConfiguration,
} from './gateway-configuration'
import type { GatewayRouteReference } from './types'
import '../../workloads/shared/workload-conditions.css'
import './gateway.css'

export type GatewayReference = (kind: string, name: string, namespace?: string) => ReactNode

export function GatewayConditionTag({
  condition,
  generation,
  compact = false,
}: {
  condition: GatewayCondition
  generation?: number
  compact?: boolean
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const stale = staleGatewayCondition(condition, generation)
  const positive = ['Accepted', 'Programmed', 'ResolvedRefs', 'Ready'].includes(condition.type)
  const negative = ['Conflicted', 'Detached'].includes(condition.type)
  const known = condition.status === 'True' || condition.status === 'False'
  const success = positive ? condition.status === 'True' : condition.status === 'False'
  const value =
    !stale && known && (positive || negative) ? (success ? 'healthy' : 'error') : 'unknown'
  const label = `${compact ? '' : `${condition.type} · `}${condition.status}${stale ? (zh ? '（旧状态）' : ' (stale)') : ''}`
  return <StatusTag value={value} label={label} />
}

export function GatewayConditions({
  conditions,
  generation,
  title,
  children,
}: {
  conditions: readonly GatewayCondition[]
  generation?: number
  title?: string
  children?: ReactNode
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  return (
    <Card className="soha-detail-card" title={title || (zh ? '网关条件' : 'Gateway conditions')}>
      {children}
      <details className="soha-workload-conditions">
        <summary>
          <span className="soha-workload-conditions-summary">
            {conditions.length ? (
              conditions.map((condition) => (
                <GatewayConditionTag
                  key={condition.type}
                  condition={condition}
                  generation={generation}
                />
              ))
            ) : (
              <Typography.Text type="secondary">
                {zh ? '尚未返回控制器条件' : 'No controller conditions returned'}
              </Typography.Text>
            )}
          </span>
        </summary>
        <AdminTable
          pagination={false}
          enableColumnSelection={false}
          rowKey="type"
          dataSource={[...conditions]}
          scroll={{ x: 1000 }}
          columns={[
            { title: zh ? '条件' : 'Condition', dataIndex: 'type', width: 170 },
            {
              title: zh ? '状态' : 'Status',
              key: 'status',
              width: 150,
              render: (_: unknown, item: GatewayCondition) => (
                <GatewayConditionTag compact condition={item} generation={generation} />
              ),
            },
            { title: zh ? '原因' : 'Reason', dataIndex: 'reason', width: 180 },
            {
              title: zh ? '消息' : 'Message',
              dataIndex: 'message',
              width: 320,
              render: (v?: string) => <span className="soha-gateway-wrap">{v || '—'}</span>,
            },
            {
              title: zh ? '观测代次' : 'Observed generation',
              dataIndex: 'observedGeneration',
              width: 130,
              render: (v?: number) => v ?? '—',
            },
            {
              title: zh ? '最近变化' : 'Last transition',
              dataIndex: 'lastTransitionTime',
              width: 165,
              render: (v?: string) => (v ? formatDateTime(v) : '—'),
            },
          ]}
        />
      </details>
    </Card>
  )
}

export function GatewayAddresses({
  addresses,
  empty,
}: {
  addresses?: readonly string[]
  empty?: string
}) {
  return (
    <div className="soha-gateway-values">
      {addresses?.length ? (
        addresses.map((address) => (
          <Typography.Text key={address} copyable className="soha-gateway-wrap">
            {address}
          </Typography.Text>
        ))
      ) : (
        <Typography.Text type="secondary">{empty || '—'}</Typography.Text>
      )}
    </div>
  )
}

export function GatewayListeners({
  listeners,
  generation,
  reference,
  preview = false,
  onShowAll,
  selectedName,
  onSelectListener,
}: {
  listeners: readonly GatewayListenerConfiguration[]
  selectedName?: string
  onSelectListener?: (name: string) => void
  generation?: number
  reference: GatewayReference
  preview?: boolean
  onShowAll?: () => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const selected =
    selectedName === undefined
      ? listeners[0]
      : listeners.find((listener) => listener.name === selectedName)
  const [keyword, setKeyword] = useState('')
  const filtered = listeners.filter((listener) =>
    [listener.name, listener.protocol, listener.hostname, String(listener.port ?? '')].some(
      (value) => (value || '').toLowerCase().includes(keyword.trim().toLowerCase()),
    ),
  )
  const columns: TableColumnsType<GatewayListenerConfiguration> = [
    {
      title: zh ? '监听器' : 'Listener',
      dataIndex: 'name',
      width: 150,
      render: (name: string) =>
        preview ? (
          name
        ) : (
          <Button
            type="link"
            className="soha-gateway-cell-button"
            aria-pressed={selected?.name === name}
            onClick={() => onSelectListener?.(name)}
          >
            {name}
          </Button>
        ),
    },
    {
      title: zh ? '协议 / 端口' : 'Protocol / port',
      key: 'port',
      width: 160,
      render: (_: unknown, item) => (
        <span className="soha-gateway-inline">
          <MetadataTag
            tone={item.protocol === 'HTTPS' || item.protocol === 'TLS' ? 'blue' : 'default'}
            label={item.protocol || '—'}
          />
          {item.port ?? '—'}
        </span>
      ),
    },
    {
      title: zh ? '域名' : 'Hostname',
      dataIndex: 'hostname',
      width: 220,
      render: (v?: string) => (
        <span className="soha-gateway-wrap">{v || (zh ? '未限定' : 'Any hostname')}</span>
      ),
    },
    {
      title: zh ? '挂载路由' : 'Attached routes',
      dataIndex: 'attachedRoutes',
      width: 110,
      render: (v?: number) => v ?? '—',
    },
    {
      title: zh ? '条件' : 'Conditions',
      key: 'conditions',
      width: 300,
      render: (_: unknown, item) => (
        <div className="soha-gateway-tags">
          {item.conditions.length ? (
            item.conditions.map((c) => (
              <GatewayConditionTag key={c.type} condition={c} generation={generation} />
            ))
          ) : (
            <Typography.Text type="secondary">{zh ? '未返回' : 'Not returned'}</Typography.Text>
          )}
        </div>
      ),
    },
  ]
  return (
    <div className="soha-detail-stack">
      <Card
        className="soha-detail-card"
        title={zh ? '监听器' : 'Listeners'}
        extra={
          preview ? (
            <Button type="link" onClick={() => onShowAll?.()}>
              {zh ? '查看全部' : 'View all'}
            </Button>
          ) : (
            <Typography.Text type="secondary">
              {filtered.length} / {listeners.length}
            </Typography.Text>
          )
        }
      >
        {!preview && (
          <div className="soha-k8s-detail-toolbar">
            <ManagementKeywordField
              label={zh ? '关键词' : 'Keyword'}
              value={keyword}
              onChange={setKeyword}
              placeholder={
                zh
                  ? '搜索监听器 / 协议 / 端口 / 域名'
                  : 'Search listener / protocol / port / hostname'
              }
              inputProps={{ size: 'small' }}
            />
          </div>
        )}
        <AdminTable
          pagination={false}
          localSorting
          enableColumnSelection={false}
          rowKey="name"
          columns={columns}
          dataSource={preview ? [...listeners.slice(0, 3)] : [...filtered]}
          scroll={{ x: 940, y: preview ? undefined : 400 }}
          rowClassName={(item: GatewayListenerConfiguration) =>
            !preview && selected?.name === item.name ? 'soha-gateway-selected-row' : ''
          }
          empty={
            <ManagementState
              compact
              description={
                zh
                  ? listeners.length
                    ? '没有匹配的监听器'
                    : '本次未返回监听器配置'
                  : listeners.length
                    ? 'No matching listeners'
                    : 'No listener configuration returned'
              }
            />
          }
        />
      </Card>
      {!preview && !selected && selectedName !== undefined && (
        <ManagementState
          compact
          kind="not-found"
          description={
            zh
              ? '所选监听器已不在当前结果中，请重新选择。'
              : 'The selected listener is no longer in the current response; select another.'
          }
        />
      )}
      {!preview && selected && (
        <>
          <div className="soha-gateway-listener-grid">
            <Card
              className="soha-detail-card"
              title={`${selected.name} · ${zh ? '监听配置' : 'Listener configuration'}`}
            >
              <WorkloadOverviewConfiguration
                items={[
                  { label: zh ? '协议' : 'Protocol', children: selected.protocol || '—' },
                  { label: zh ? '端口' : 'Port', children: selected.port ?? '—' },
                  {
                    label: zh ? '域名' : 'Hostname',
                    children: selected.hostname || (zh ? '未限定' : 'Any hostname'),
                  },
                  {
                    label: 'TLS',
                    children:
                      selected.tlsMode ||
                      (selected.configurationReturned
                        ? selected.tlsConfigured
                          ? zh
                            ? 'Terminate（默认）'
                            : 'Terminate (default)'
                          : zh
                            ? '未配置'
                            : 'Not configured'
                        : '—'),
                  },
                  {
                    label: zh ? '挂载路由' : 'Attached routes',
                    children:
                      selected.attachedRoutes ?? (zh ? '状态未返回' : 'Status not returned'),
                  },
                ]}
              />
            </Card>
            <Card className="soha-detail-card" title={zh ? '允许路由' : 'Allowed routes'}>
              <WorkloadOverviewConfiguration
                items={[
                  {
                    label: zh ? '类型限制' : 'Allowed kinds',
                    children: selected.allowedRouteKinds?.length ? (
                      <div className="soha-gateway-tags">
                        {selected.allowedRouteKinds.map((v) => (
                          <MetadataTag key={v} label={v} />
                        ))}
                      </div>
                    ) : selected.configurationReturned ? (
                      zh ? (
                        '由监听协议决定（默认）'
                      ) : (
                        'Determined by listener protocol (default)'
                      )
                    ) : (
                      '—'
                    ),
                  },
                  {
                    label: zh ? '命名空间范围' : 'Namespace scope',
                    children:
                      selected.allowedNamespaces || (selected.configurationReturned ? 'Same' : '—'),
                  },
                  ...(selected.namespaceSelector.length
                    ? [
                        {
                          label: zh ? '命名空间选择器' : 'Namespace selector',
                          children: (
                            <div className="soha-gateway-values">
                              {selected.namespaceSelector.map((v) => (
                                <Typography.Text key={v} copyable className="soha-gateway-wrap">
                                  {v}
                                </Typography.Text>
                              ))}
                            </div>
                          ),
                        },
                      ]
                    : []),
                  {
                    label: zh ? '控制器支持类型' : 'Supported kinds',
                    children: selected.supportedKinds?.length ? (
                      <div className="soha-gateway-tags">
                        {selected.supportedKinds.map((v) => (
                          <MetadataTag key={v} label={v} />
                        ))}
                      </div>
                    ) : zh ? (
                      '未返回'
                    ) : (
                      'Not returned'
                    ),
                  },
                ]}
              />
            </Card>
          </div>
          <Card className="soha-detail-card" title={zh ? '证书引用' : 'Certificate references'}>
            <div className="soha-gateway-certificates">
              {selected.certificateRefs.map((ref) => (
                <div key={ref.sourceLabel}>
                  <MetadataTag label={ref.kind} />
                  <span>{ref.namespace}</span>
                  <strong>
                    {ref.group === '' && ref.kind === 'Secret'
                      ? reference('Secret', ref.name, ref.namespace)
                      : ref.name}
                  </strong>
                  {ref.group && <Typography.Text type="secondary">{ref.group}</Typography.Text>}
                </div>
              ))}
            </div>
            <div className="soha-gateway-values">
              {selected.certificateLabels
                .filter(
                  (label) => !selected.certificateRefs.some((ref) => ref.sourceLabel === label),
                )
                .map((label) => (
                  <Typography.Text key={label} className="soha-gateway-wrap">
                    {label}
                  </Typography.Text>
                ))}
            </div>
            {!selected.certificateRefs.length && !selected.certificateLabels.length && (
              <Typography.Text type="secondary">
                {zh ? '未返回可查看的证书引用' : 'No readable certificate references returned'}
              </Typography.Text>
            )}
          </Card>
          <GatewayConditions
            conditions={selected.conditions}
            generation={generation}
            title={zh ? '监听器条件' : 'Listener conditions'}
          />
        </>
      )}
    </div>
  )
}

export function GatewayRoutes({
  routes,
  reference,
  preview = false,
  onShowAll,
}: {
  routes: readonly GatewayRouteReference[]
  reference: GatewayReference
  preview?: boolean
  onShowAll?: () => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const [keyword, setKeyword] = useState(''),
    [kind, setKind] = useState('all')
  const filtered = routes.filter(
    (route) =>
      (kind === 'all' || route.kind === kind) &&
      [route.name, route.namespace, route.kind, ...(route.hostnames || [])].some((v) =>
        (v || '').toLowerCase().includes(keyword.trim().toLowerCase()),
      ),
  )
  return (
    <Card
      className="soha-detail-card"
      title={zh ? '已读路由引用' : 'Returned route references'}
      extra={
        preview ? (
          <Button type="link" onClick={() => onShowAll?.()}>
            {zh ? '查看全部' : 'View all'}
          </Button>
        ) : (
          <Typography.Text type="secondary">
            {filtered.length} / {routes.length}
          </Typography.Text>
        )
      }
    >
      {!preview && (
        <div className="soha-k8s-detail-toolbar">
          <ManagementKeywordField
            label={zh ? '关键词' : 'Keyword'}
            value={keyword}
            onChange={setKeyword}
            placeholder={zh ? '搜索路由 / 命名空间 / 域名' : 'Search route / namespace / hostname'}
            inputProps={{ size: 'small' }}
          />
          <Select
            aria-label={zh ? '路由类型' : 'Route kind'}
            value={kind}
            onChange={setKind}
            className="soha-gateway-route-kind"
            options={[
              { value: 'all', label: zh ? '全部类型' : 'All kinds' },
              { value: 'HTTPRoute', label: 'HTTPRoute' },
              { value: 'GRPCRoute', label: 'GRPCRoute' },
            ]}
          />
        </div>
      )}
      {!preview && (
        <Typography.Paragraph type="secondary">
          {zh
            ? '仅展示本次读取且有权查看的 HTTPRoute / GRPCRoute 引用；挂载数量不等于已接纳数量。'
            : 'Visible ParentRef references returned by this read; attached counts are not accepted counts.'}
        </Typography.Paragraph>
      )}
      <AdminTable
        pagination={false}
        localSorting
        enableColumnSelection={false}
        rowKey={(r: GatewayRouteReference) => `${r.kind}/${r.namespace}/${r.name}`}
        dataSource={preview ? [...routes.slice(0, 3)] : [...filtered]}
        scroll={{ x: 760, y: preview ? undefined : 520 }}
        columns={[
          {
            title: zh ? '类型' : 'Kind',
            dataIndex: 'kind',
            width: 130,
            render: (v: string) => <MetadataTag label={v} />,
          },
          {
            title: zh ? '名称' : 'Name',
            dataIndex: 'name',
            width: 240,
            render: (name: string, item: GatewayRouteReference) =>
              reference(item.kind, name, item.namespace),
          },
          {
            title: zh ? '命名空间' : 'Namespace',
            dataIndex: 'namespace',
            width: 160,
            render: (v?: string) => (v ? <MetadataTag tone="blue" label={v} /> : '—'),
          },
          {
            title: zh ? '域名' : 'Hostnames',
            dataIndex: 'hostnames',
            width: 250,
            render: (v?: string[]) => (
              <span className="soha-gateway-wrap">
                {v?.join(', ') || (zh ? '未限定' : 'Any hostname')}
              </span>
            ),
          },
          {
            title: 'Accepted',
            dataIndex: 'accepted',
            width: 140,
            render: (v?: string) =>
              v ? (
                <GatewayConditionTag compact condition={{ type: 'Accepted', status: v }} />
              ) : (
                <Typography.Text type="secondary">{zh ? '未返回' : 'Not returned'}</Typography.Text>
              ),
          },
        ]}
        empty={
          <ManagementState
            compact
            description={
              zh
                ? routes.length
                  ? '没有匹配的路由'
                  : '本次未返回可查看的路由引用'
                : routes.length
                  ? 'No matching routes'
                  : 'No visible route references returned'
            }
          />
        }
      />
    </Card>
  )
}
