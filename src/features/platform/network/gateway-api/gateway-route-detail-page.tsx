import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  ResourceDetailHeader,
  useResourceDetailTab,
} from '@/features/platform/shared/resource-detail-header'
import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { useNetworkDetailScope } from '../shared/detail-scope'
import { NetworkDetailShell } from '../shared/detail-shell'
import { networkMutations } from '../shared/mutations'
import { networkQueries } from '../shared/queries'
import { networkResourcePath } from '../shared/resource-links'
import { GatewayConditions } from './gateway-panels'
import {
  httpRouteConfiguration,
  httpRouteModel,
  type HTTPRouteReference,
} from './httproute-configuration'
import { HTTPRouteParents, HTTPRouteRules, HTTPRouteValues } from './httproute-panels'
import { gatewayAPIQueries } from './queries'
import type { HTTPRouteDetail, GRPCRouteDetail } from './types'
type GatewayRouteDetail = HTTPRouteDetail | GRPCRouteDetail

const tabs = ['overview', 'rules', 'relationships', 'yaml']
export function GatewayRouteDetailPage({ kind }: { kind: 'httproutes' | 'grpcroutes' }) {
  const label = kind === 'grpcroutes' ? 'GRPCRoute' : 'HTTPRoute'
  const permissionSegment = kind === 'grpcroutes' ? 'grpc-routes' : 'http-routes'
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { name = '' } = useParams()
  const navigate = useNavigate(),
    client = useQueryClient()
  const scope = useNetworkDetailScope(),
    { clusterId } = scope,
    detailNamespace = scope.namespace ?? '',
    identity = JSON.stringify([kind, clusterId, detailNamespace, name])
  const [tab, setTab] = useResourceDetailTab(),
    active = tabs.includes(tab) ? tab : 'overview'
  useEffect(() => {
    if (!tabs.includes(tab)) setTab('overview')
  }, [tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.${permissionSegment}.${action}`)
  const canRead = permitted('view'),
    canGateways = hasPermission(permissions, 'platform.network.gateways.view'),
    canServices = hasPermission(permissions, 'platform.network.services.view'),
    canPods = hasPermission(permissions, 'platform.pods.view'),
    canEndpoints = hasPermission(permissions, 'platform.network.endpoint-slices.view')
  const refresh = useWorkloadRefresh(),
    options = gatewayAPIQueries.detail<GatewayRouteDetail>(kind, scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const returned = canRead ? readableQueryData(query) : undefined
  const mismatch = returned && (returned.name !== name || returned.namespace !== detailNamespace)
  const detail = returned && !mismatch ? returned : undefined
  const yamlOptions = networkQueries.yaml(kind, scope, name)
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled:
      Boolean(yamlOptions.enabled) && canRead && active !== 'yaml' && active !== 'relationships',
    refetchInterval: refresh.interval,
  })
  const source = canRead ? readableQueryData(yamlQuery) : undefined
  const configuration = useMemo(() => {
    if (!source) return undefined
    try {
      return { data: httpRouteConfiguration(source.content, name, detailNamespace, label) }
    } catch {
      return {
        error: zh
          ? '无法读取完整路由配置，当前保留接口摘要；可查看 YAML。'
          : 'Full route configuration is unavailable; showing the API summary. Inspect YAML.',
      }
    }
  }, [source, name, detailNamespace, label, zh])
  const model = useMemo(
    () => (detail ? httpRouteModel(detail, configuration?.data) : undefined),
    [detail, configuration],
  )
  const parents = canGateways ? model?.parents || [] : [],
    statuses = canGateways ? model?.statuses || [] : []
  const rules = (model?.rules || []).map((rule) => ({
    ...rule,
    backends: rule.backends
      .filter((backend) => (backend.kind || 'Service') !== 'Service' || canServices)
      .map((backend) => ({
        ...backend,
        endpoints: canEndpoints ? backend.endpoints : undefined,
        backendPods: canPods ? backend.backendPods : undefined,
      })),
  }))
  const [selection, setSelection] = useState<{ identity: string; key: string }>()
  const selectedKey = selection?.identity === identity ? selection.key : undefined
  const selectRule = (key: string) => setSelection({ identity, key })
  const canUpdate =
    canRead && permitted('update') && hasAllowedAction(detail?.allowedActions, 'update')
  const canDelete =
    canRead && permitted('delete') && hasAllowedAction(detail?.allowedActions, 'delete')
  const remove = usePlatformMutation(networkMutations.remove(kind, client), {
    label: [`删除 ${label}`, `Delete ${label}`],
    target: platformResourceTarget,
  })
  const actions = useKubernetesResourceActions<GatewayRouteDetail>({
    resourceKey: `${kind}-detail`,
    resourceLabel: label,
    records: detail ? [detail] : [],
    getName: (r) => r.name,
    getKey: (r) => `${r.namespace}/${r.name}`,
    scopeKey: identity,
    actions: (record) =>
      canDelete
        ? [
            {
              key: 'delete',
              label: zh ? '删除' : 'Delete',
              danger: true,
              disabled: remove.blocked,
              confirm: kubernetesDeleteConfirmation(
                record,
                localeCode,
                zh
                  ? '删除会移除此路由配置，匹配的请求可能无法继续转发。'
                  : 'Deleting this route removes its configuration; matching requests may stop forwarding.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/gateway-api/${kind}?${new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace })}`,
                )
              },
            },
          ]
        : [],
  })
  const activeQueries = [
    query,
    ...(yamlQuery.isEnabled || active === 'yaml' ? [{ ...yamlQuery, isEnabled: true }] : []),
  ]
  const feedback = (
    <>
      <ReadFeedback canRead={canRead} query={query} label={label} />
      {yamlQuery.isEnabled && (
        <ReadFeedback canRead={canRead} query={yamlQuery} label={`${label} YAML`} />
      )}
      {configuration?.error && <Alert type="warning" showIcon title={configuration.error} />}
      {mismatch && (
        <Alert
          type="error"
          showIcon
          title={
            zh
              ? `返回的 ${label} 与当前资源不一致，请重新读取。`
              : `The returned ${label} does not match this resource; retry the read.`
          }
        />
      )}
    </>
  )
  if (!clusterId || !detailNamespace)
    return (
      <ManagementState
        kind="select-scope"
        description={zh ? '请选择集群和命名空间' : 'Select a cluster and namespace'}
      />
    )
  if (!detail)
    return (
      <div className="soha-page">
        {feedback}
        {query.isLoading ? (
          <Card loading className="soha-detail-card" />
        ) : canRead && !query.isError && !mismatch ? (
          <ManagementState
            kind="not-found"
            description={zh ? `${label} 未找到` : `${label} not found`}
          />
        ) : null}
      </div>
    )
  const reference = (ref: HTTPRouteReference) => {
    const knownGroup =
      ref.kind === 'Gateway'
        ? ref.group === 'gateway.networking.k8s.io'
        : ['Service', 'Pod'].includes(ref.kind) && ref.group === ''
    const path = knownGroup ? networkResourcePath(permissions, { ...ref, clusterId }) : null
    return path ? (
      <Link className="soha-gateway-wrap" title={ref.name} to={path}>
        {ref.name}
      </Link>
    ) : (
      <span className="soha-gateway-wrap">{ref.name}</span>
    )
  }
  const parentAccess = (
    <ManagementState
      compact
      kind="no-permission"
      description={zh ? '没有查看父级 Gateway 的权限' : 'No permission to view parent Gateways'}
    />
  )
  const showRules = () => setTab('rules')
  const overview = (
    <WorkloadOverview
      aside={
        <Card className="soha-detail-card" title={zh ? '路由配置' : 'Route configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '命名空间' : 'Namespace', children: detail.namespace },
              { label: zh ? '规则' : 'Rules', children: rules.length },
              {
                label: zh ? '已读父级引用' : 'Returned parent references',
                children: canGateways ? parents.length : '—',
              },
              ...(configuration?.data?.generation !== undefined
                ? [
                    {
                      label: zh ? '配置代次' : 'Generation',
                      children: configuration.data.generation,
                    },
                  ]
                : []),
              ...(configuration?.data?.createdAt
                ? [
                    {
                      label: zh ? '创建时间' : 'Created',
                      children: formatDateTime(configuration.data.createdAt),
                    },
                  ]
                : []),
            ]}
          />
        </Card>
      }
    >
      <Card className="soha-detail-card" title={zh ? '匹配域名' : 'Hostnames'}>
        <HTTPRouteValues values={detail.hostnames} empty={zh ? '未限定域名' : 'Any hostname'} />
      </Card>
      <HTTPRouteRules
        preview
        rules={rules}
        selectedKey={selectedKey}
        onSelect={selectRule}
        onShowAll={showRules}
        reference={reference}
        canPods={canPods}
        canEndpoints={canEndpoints}
      />
      {canGateways ? (
        <HTTPRouteParents
          parents={parents}
          statuses={statuses}
          generation={configuration?.data?.generation}
          reference={reference}
        />
      ) : (
        parentAccess
      )}
      {Boolean(detail.conditions?.length) && (
        <GatewayConditions
          title={zh ? '资源条件' : 'Resource conditions'}
          conditions={detail.conditions || []}
          generation={configuration?.data?.generation}
        />
      )}
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
  return (
    <div className="soha-httproute-detail">
      {feedback}
      {actions.confirmation}
      {!actions.isConfirming && remove.feedback}
      <NetworkDetailShell
        key={identity}
        activeTabKey={active}
        onTabChange={setTab}
        kind={kind}
        label={label}
        detail={detail}
        target={{ scope, name }}
        preserveYaml
        canUpdateYAML={canUpdate}
        tabOrder={tabs}
        overview={overview}
        header={
          <ResourceDetailHeader
            kind={label}
            name={detail.name}
            namespace={detail.namespace}
            ageSeconds={detail.ageSeconds}
            facts={
              <span>
                {zh ? '规则' : 'Rules'} {rules.length}
              </span>
            }
            backTo={`/network/gateway-api/${kind}`}
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  resourceLabel={label}
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  beforeRefresh={
                    canUpdate ? (
                      <Button
                        disabled={remove.blocked}
                        aria-label={zh ? `编辑 ${label}` : `Edit ${label}`}
                        onClick={() => setTab('yaml')}
                      >
                        {zh ? '编辑' : 'Edit'}
                      </Button>
                    ) : undefined
                  }
                />
                {canDelete && (
                  <Button
                    danger
                    disabled={remove.blocked}
                    aria-label={zh ? `删除 ${label}` : `Delete ${label}`}
                    onClick={() => actions.selectAction(detail, 'delete')}
                  >
                    {zh ? '删除' : 'Delete'}
                  </Button>
                )}
              </>
            }
          />
        }
        extraTabs={[
          {
            key: 'rules',
            label: zh ? '规则与后端' : 'Rules and backends',
            children: (
              <HTTPRouteRules
                key={identity}
                rules={rules}
                selectedKey={selectedKey}
                onSelect={selectRule}
                reference={reference}
                canPods={canPods}
                canEndpoints={canEndpoints}
              />
            ),
          },
          {
            key: 'relationships',
            label: zh ? '关联资源' : 'Related resources',
            children: (
              <WorkloadRelationships
                key={identity}
                kind={label}
                scope={scope}
                name={name}
                active={active === 'relationships'}
              />
            ),
          },
        ]}
      />
    </div>
  )
}
