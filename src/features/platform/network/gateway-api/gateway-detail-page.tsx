import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag } from '@/components/status-tag'
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
import { networkResourcePath, permissionsByKind } from '../shared/resource-links'
import { gatewayConfiguration, gatewayListeners } from './gateway-configuration'
import {
  GatewayAddresses,
  GatewayConditions,
  GatewayConditionTag,
  GatewayListeners,
  GatewayRoutes,
} from './gateway-panels'
import { gatewayAPIQueries } from './queries'
import type { GatewayDetail } from './types'
import './gateway.css'

const tabs = ['overview', 'listeners', 'routes', 'relationships', 'yaml']
export function GatewayDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { name = '' } = useParams()
  const navigate = useNavigate(),
    client = useQueryClient()
  const scope = useNetworkDetailScope(),
    { clusterId } = scope,
    detailNamespace = scope.namespace ?? '',
    identity = JSON.stringify([clusterId, detailNamespace, name])
  const [tab, setTab] = useResourceDetailTab(),
    active = tabs.includes(tab) ? tab : 'overview'
  useEffect(() => {
    if (!tabs.includes(tab)) setTab('overview')
  }, [tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.gateways.${action}`)
  const canView = (kind: string) =>
    Boolean(permissionsByKind[kind]) && hasPermission(permissions, permissionsByKind[kind])
  const canRead = permitted('view'),
    canRoutes = canView('HTTPRoute') || canView('GRPCRoute'),
    canSecrets = canView('Secret')
  const refresh = useWorkloadRefresh(),
    options = gatewayAPIQueries.detail<GatewayDetail>('gateways', scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const returned = canRead ? readableQueryData(query) : undefined
  const identityMismatch =
    returned && (returned.name !== name || returned.namespace !== detailNamespace)
  const detail = returned && !identityMismatch ? returned : undefined
  const yamlOptions = networkQueries.yaml('gateways', scope, name)
  const configurationQuery = useQuery({
    ...yamlOptions,
    enabled:
      Boolean(yamlOptions.enabled) && canRead && active !== 'yaml' && active !== 'relationships',
    refetchInterval: refresh.interval,
  })
  const source = canRead ? readableQueryData(configurationQuery) : undefined
  const configuration = useMemo(() => {
    if (!source) return undefined
    try {
      return { data: gatewayConfiguration(source.content, name, detailNamespace) }
    } catch {
      return {
        error: zh
          ? '无法读取当前网关的完整配置，请查看 YAML。'
          : 'Cannot read the full configuration for this Gateway; inspect YAML.',
      }
    }
  }, [source, name, detailNamespace, zh])
  const config = configuration?.data
  const listeners = useMemo(() => {
    if (!detail) return []
    return gatewayListeners(detail, config).map((listener) => ({
      ...listener,
      certificateRefs: canSecrets ? listener.certificateRefs : [],
      certificateLabels: canSecrets ? listener.certificateLabels : [],
    }))
  }, [detail, config, canSecrets])
  const routes = (detail?.routes || [])
    .filter((route) => canView(route.kind))
    .map((route) => ({ ...route, namespace: route.namespace || detailNamespace }))
  const [listenerSelection, setListenerSelection] = useState<{ identity: string; name: string }>()
  const selectedListenerName =
    listenerSelection?.identity === identity ? listenerSelection.name : undefined
  const canUpdate =
    canRead && permitted('update') && hasAllowedAction(detail?.allowedActions, 'update')
  const canDelete =
    canRead && permitted('delete') && hasAllowedAction(detail?.allowedActions, 'delete')
  const remove = usePlatformMutation(networkMutations.remove('gateways', client), {
    label: ['删除 Gateway', 'Delete Gateway'],
    target: platformResourceTarget,
  })
  const actions = useKubernetesResourceActions<GatewayDetail>({
    resourceKey: 'gateway-detail',
    resourceLabel: 'Gateway',
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
                  ? '删除会移除网关配置，引用它的路由可能无法继续转发流量。'
                  : 'Deleting this Gateway removes its configuration; referencing routes may stop forwarding traffic.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/gateway-api/gateways?${new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace })}`,
                )
              },
            },
          ]
        : [],
  })
  const feedback = (
    <>
      <ReadFeedback canRead={canRead} query={query} label="Gateway" />
      {configurationQuery.isEnabled && (
        <ReadFeedback canRead={canRead} query={configurationQuery} label="Gateway YAML" />
      )}
      {configuration?.error && <Alert showIcon type="warning" title={configuration.error} />}
      {identityMismatch && (
        <Alert
          showIcon
          type="error"
          title={
            zh
              ? '返回的 Gateway 与当前资源不一致，请重新读取。'
              : 'The returned Gateway does not match the current resource; retry the read.'
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
        ) : canRead && !query.isError && !identityMismatch ? (
          <ManagementState
            kind="not-found"
            description={zh ? 'Gateway 未找到' : 'Gateway not found'}
          />
        ) : null}
      </div>
    )
  const reference = (kind: string, resourceName: string, ns?: string) => {
    const path =
      kind === 'Secret' && !hasPermission(permissions, 'platform.configuration.secret-data.view')
        ? null
        : networkResourcePath(permissions, { kind, name: resourceName, namespace: ns, clusterId })
    return path ? (
      <Link className="soha-gateway-wrap" title={resourceName} to={path}>
        {resourceName}
      </Link>
    ) : (
      <span className="soha-gateway-wrap">{resourceName || '—'}</span>
    )
  }
  const conditions = config?.conditions ?? detail.conditions ?? []
  const showListeners = (listenerName?: string) => {
    if (listenerName) setListenerSelection({ identity, name: listenerName })
    setTab('listeners')
  }
  const routePanel = (preview = false) =>
    canRoutes ? (
      <GatewayRoutes
        key={identity}
        routes={routes}
        reference={reference}
        preview={preview}
        onShowAll={() => setTab('routes')}
      />
    ) : (
      <Card className="soha-detail-card" title={zh ? '路由引用' : 'Route references'}>
        <ManagementState
          compact
          kind="no-permission"
          description={
            zh
              ? '没有查看 HTTPRoute 或 GRPCRoute 的权限'
              : 'No permission to view HTTPRoutes or GRPCRoutes'
          }
        />
      </Card>
    )
  const overview = (
    <WorkloadOverview
      aside={
        <Card className="soha-detail-card" title={zh ? '网关配置' : 'Gateway configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '命名空间' : 'Namespace',
                children: <MetadataTag tone="blue" label={detail.namespace} />,
              },
              {
                label: 'GatewayClass',
                children: detail.gatewayClass
                  ? reference('GatewayClass', detail.gatewayClass)
                  : zh
                    ? '未返回或无权查看'
                    : 'Not returned or not permitted',
              },
              { label: zh ? '配置代次' : 'Generation', children: config?.generation ?? '—' },
              {
                label: zh ? '请求地址' : 'Requested addresses',
                children: <GatewayAddresses addresses={config?.requestedAddresses} />,
              },
              {
                label: zh ? '创建时间' : 'Created',
                children: config?.createdAt ? formatDateTime(config.createdAt) : '—',
              },
            ]}
          />
        </Card>
      }
    >
      <Card className="soha-detail-card" title={zh ? '访问地址' : 'Access addresses'}>
        <GatewayAddresses
          addresses={detail.addresses}
          empty={zh ? '未返回已分配地址' : 'No assigned address returned'}
        />
      </Card>
      <GatewayListeners
        key={identity}
        listeners={listeners}
        generation={config?.generation}
        reference={reference}
        preview
        onShowAll={showListeners}
      />
      {routePanel(true)}
      <GatewayConditions conditions={conditions} generation={config?.generation} />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
  const activeQueries = [
    query,
    ...(configurationQuery.isEnabled || active === 'yaml'
      ? [{ ...configurationQuery, isEnabled: true }]
      : []),
  ]
  return (
    <div className="soha-gateway-detail">
      {feedback}
      {actions.confirmation}
      {!actions.isConfirming && remove.feedback}
      <NetworkDetailShell
        key={identity}
        detail={detail}
        kind="gateways"
        label="Gateway"
        target={{ scope, name }}
        activeTabKey={active}
        onTabChange={setTab}
        tabOrder={tabs}
        preserveYaml
        canUpdateYAML={canUpdate}
        overview={overview}
        header={
          <ResourceDetailHeader
            kind="Gateway"
            name={detail.name}
            namespace={detail.namespace}
            ageSeconds={detail.ageSeconds}
            facts={
              <>
                {conditions
                  .filter((condition) => ['Accepted', 'Programmed'].includes(condition.type))
                  .map((condition) => (
                    <GatewayConditionTag
                      key={condition.type}
                      condition={condition}
                      generation={config?.generation}
                    />
                  ))}
                <span>
                  {zh ? '监听器' : 'Listeners'} {detail.listenerCount}
                </span>
                {detail.gatewayClass && reference('GatewayClass', detail.gatewayClass)}
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  resourceLabel="Gateway"
                  beforeRefresh={
                    canUpdate ? (
                      <Button
                        aria-label={zh ? '编辑 Gateway' : 'Edit Gateway'}
                        disabled={remove.blocked}
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
                    aria-label={zh ? '删除 Gateway' : 'Delete Gateway'}
                    disabled={remove.blocked}
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
            key: 'listeners',
            label: zh ? '监听器' : 'Listeners',
            children: (
              <GatewayListeners
                key={identity}
                listeners={listeners}
                generation={config?.generation}
                reference={reference}
                selectedName={selectedListenerName}
                onSelectListener={(name) => setListenerSelection({ identity, name })}
              />
            ),
          },
          { key: 'routes', label: zh ? '路由' : 'Routes', children: routePanel() },
          {
            key: 'relationships',
            label: zh ? '关联资源' : 'Related resources',
            children: (
              <WorkloadRelationships
                key={identity}
                kind="Gateway"
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
