import { Card } from 'antd'
import type { TableProps } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { Link } from 'react-router-dom'
import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import type { WorkloadCondition } from '@/types'
import { buildNetworkRoutePath, buildGatewayAPIRoutePath } from '../shared/paths'
import { renderNetworkTextList } from '../shared/renderers'
import type {
  Gateway,
  GatewayListener,
  GatewayRouteBackend,
  GatewayRouteParentStatus,
  GatewayRouteReference,
  GatewayRouteRule,
} from './types'

function buildPodPath(name: string, namespace: string) {
  return `/workloads/pods/${encodeURIComponent(name)}?namespace=${encodeURIComponent(namespace)}`
}

function renderConditionSummary(conditions?: WorkloadCondition[]) {
  return conditions?.length
    ? conditions.map((condition, index) => (
        <span key={`${condition.type}/${index}`}>
          {index ? ', ' : ''}
          {condition.type}=<StatusTag value={condition.status} />
        </span>
      ))
    : '-'
}

function DetailTable<T extends object>({
  columns,
  data,
  rowKey,
  title,
}: {
  columns: NonNullable<TableProps<T>['columns']>
  data?: T[]
  rowKey: string | ((record: T) => string)
  title: string
}) {
  if (!data?.length) return null
  return (
    <Card className="soha-detail-card" title={title}>
      <AdminTable
        enableColumnSelection={false}
        scroll={{ x: '100%' }}
        columns={columns}
        dataSource={data}
        pagination={false}
        rowKey={rowKey}
        tableSize="small"
      />
    </Card>
  )
}

export function GatewaysSection({ gateways }: { gateways?: Gateway[] }) {
  const { localeCode } = useI18n()
  return (
    <DetailTable
      title={localeCode === 'zh_CN' ? '关联 Gateways' : 'Related Gateways'}
      data={gateways}
      rowKey={(item) => `${item.namespace}/${item.name}`}
      columns={[
        {
          title: 'Gateway',
          dataIndex: 'name',
          render: (value: string, item: Gateway) => (
            <Link to={buildGatewayAPIRoutePath('gateways', value, item.namespace)}>{value}</Link>
          ),
        },
        { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
        {
          title: localeCode === 'zh_CN' ? '地址' : 'Addresses',
          dataIndex: 'addresses',
          render: (value?: string[]) => renderNetworkTextList(value),
        },
        { title: localeCode === 'zh_CN' ? '监听器' : 'Listeners', dataIndex: 'listenerCount' },
      ]}
    />
  )
}

export function GatewayListenersSection({ listeners }: { listeners?: GatewayListener[] }) {
  const { localeCode } = useI18n()
  return (
    <DetailTable
      title={localeCode === 'zh_CN' ? '监听器' : 'Listeners'}
      data={listeners}
      rowKey="name"
      columns={[
        { title: localeCode === 'zh_CN' ? '名称' : 'Name', dataIndex: 'name' },
        { title: localeCode === 'zh_CN' ? '协议' : 'Protocol', dataIndex: 'protocol' },
        { title: localeCode === 'zh_CN' ? '端口' : 'Port', dataIndex: 'port' },
        { title: 'Hostname', dataIndex: 'hostname', render: (value?: string) => value || '-' },
        { title: 'TLS', dataIndex: 'tlsMode', render: (value?: string) => value || '-' },
        {
          title: localeCode === 'zh_CN' ? '证书' : 'Certificates',
          dataIndex: 'certificateRefs',
          render: (value?: string[]) => renderNetworkTextList(value),
        },
        {
          title: localeCode === 'zh_CN' ? '允许路由' : 'Allowed routes',
          dataIndex: 'allowedRouteKinds',
          render: (value?: string[]) => renderNetworkTextList(value),
        },
        { title: localeCode === 'zh_CN' ? '已挂载' : 'Attached', dataIndex: 'attachedRoutes' },
        {
          title: localeCode === 'zh_CN' ? '条件' : 'Conditions',
          dataIndex: 'conditions',
          render: renderConditionSummary,
        },
      ]}
    />
  )
}

export function GatewayRoutesSection({ routes }: { routes?: GatewayRouteReference[] }) {
  const { localeCode } = useI18n()
  return (
    <DetailTable
      title={localeCode === 'zh_CN' ? '关联路由' : 'Related routes'}
      data={routes}
      rowKey={(item) => `${item.kind}/${item.namespace}/${item.name}`}
      columns={[
        { title: localeCode === 'zh_CN' ? '类型' : 'Kind', dataIndex: 'kind' },
        {
          title: localeCode === 'zh_CN' ? '名称' : 'Name',
          dataIndex: 'name',
          render: (value: string, item: GatewayRouteReference) => {
            const normalizedKind = item.kind.toLowerCase()
            if (normalizedKind !== 'httproute' && normalizedKind !== 'grpcroute') return value
            const kind = normalizedKind === 'grpcroute' ? 'grpcroutes' : 'httproutes'
            return <Link to={buildGatewayAPIRoutePath(kind, value, item.namespace)}>{value}</Link>
          },
        },
        { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
        {
          title: 'Hostnames',
          dataIndex: 'hostnames',
          render: (value?: string[]) => renderNetworkTextList(value),
        },
        {
          title: localeCode === 'zh_CN' ? '已接受' : 'Accepted',
          dataIndex: 'accepted',
          render: (value?: string) => (value ? <StatusTag value={value} /> : '-'),
        },
      ]}
    />
  )
}

interface RuleBackendRow extends GatewayRouteBackend {
  readonly key: string
  readonly rule: number
  readonly matches?: string[]
  readonly filters?: string[]
}

export function RouteRulesSection({ rules }: { rules?: GatewayRouteRule[] }) {
  const { localeCode } = useI18n()
  const rows: RuleBackendRow[] = (rules ?? []).flatMap((rule, ruleIndex) =>
    (rule.backends?.length ? rule.backends : [{ name: '-' }]).map((backend, backendIndex) => ({
      ...backend,
      key: `${ruleIndex}/${backendIndex}`,
      rule: ruleIndex + 1,
      matches: rule.matches,
      filters: rule.filters,
    })),
  )
  return (
    <DetailTable
      title={localeCode === 'zh_CN' ? '路由规则与后端' : 'Rules and backends'}
      data={rows}
      rowKey="key"
      columns={[
        { title: localeCode === 'zh_CN' ? '规则' : 'Rule', dataIndex: 'rule', width: 72 },
        {
          title: localeCode === 'zh_CN' ? '匹配' : 'Matches',
          dataIndex: 'matches',
          render: (value?: string[]) => renderNetworkTextList(value),
        },
        {
          title: localeCode === 'zh_CN' ? '过滤器' : 'Filters',
          dataIndex: 'filters',
          render: (value?: string[]) => renderNetworkTextList(value),
        },
        {
          title: localeCode === 'zh_CN' ? '后端' : 'Backend',
          dataIndex: 'name',
          render: (value: string, item: RuleBackendRow) =>
            value !== '-' && (!item.kind || item.kind === 'Service') ? (
              <Link to={buildNetworkRoutePath('services', value, item.namespace || '')}>
                {value}
              </Link>
            ) : (
              value
            ),
        },
        {
          title: localeCode === 'zh_CN' ? '端口' : 'Port',
          dataIndex: 'port',
          render: (value?: number) => value || '-',
        },
        {
          title: localeCode === 'zh_CN' ? '端点' : 'Endpoints',
          render: (_: unknown, item: RuleBackendRow) => item.endpoints?.length ?? 0,
        },
        {
          title: 'Pods',
          render: (_: unknown, item: RuleBackendRow) =>
            item.backendPods?.length
              ? item.backendPods.map((pod, index) => (
                  <span key={`${pod.namespace}/${pod.name}`}>
                    {index ? ', ' : ''}
                    <Link to={buildPodPath(pod.name, pod.namespace)}>{pod.name}</Link>
                  </span>
                ))
              : '-',
        },
      ]}
    />
  )
}

export function ConditionsSection({ conditions }: { conditions?: WorkloadCondition[] }) {
  const { localeCode } = useI18n()
  return (
    <DetailTable
      title={localeCode === 'zh_CN' ? '条件' : 'Conditions'}
      data={conditions}
      rowKey={(item) => `${item.type}/${item.lastTransitionTime}`}
      columns={[
        { title: localeCode === 'zh_CN' ? '类型' : 'Type', dataIndex: 'type' },
        {
          title: localeCode === 'zh_CN' ? '状态' : 'Status',
          dataIndex: 'status',
          render: (value: string) => <StatusTag value={value} />,
        },
        {
          title: localeCode === 'zh_CN' ? '原因' : 'Reason',
          dataIndex: 'reason',
          render: (value?: string) => value || '-',
        },
        {
          title: localeCode === 'zh_CN' ? '消息' : 'Message',
          dataIndex: 'message',
          render: (value?: string) => value || '-',
        },
        {
          title: localeCode === 'zh_CN' ? '最近变化' : 'Last transition',
          dataIndex: 'lastTransitionTime',
          render: (value?: string) => value || '-',
        },
      ]}
    />
  )
}

export function ParentStatusesSection({ statuses }: { statuses?: GatewayRouteParentStatus[] }) {
  const { localeCode } = useI18n()
  return (
    <DetailTable
      title={localeCode === 'zh_CN' ? '父级状态' : 'Parent status'}
      data={statuses}
      rowKey={(item) => `${item.parentRef}/${item.controllerName}`}
      columns={[
        { title: localeCode === 'zh_CN' ? '父级引用' : 'Parent', dataIndex: 'parentRef' },
        {
          title: localeCode === 'zh_CN' ? '控制器' : 'Controller',
          dataIndex: 'controllerName',
          render: (value?: string) => value || '-',
        },
        {
          title: localeCode === 'zh_CN' ? '条件' : 'Conditions',
          dataIndex: 'conditions',
          render: renderConditionSummary,
        },
      ]}
    />
  )
}
