import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag } from '@/components/status-tag'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  ResourceDetailHeader,
  useResourceDetailTab,
} from '@/features/platform/shared/resource-detail-header'
import { ResourceGitOpsStatus } from '@/features/platform/shared/resource-gitops-status'
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
import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Spin, Typography } from 'antd'
import { Suspense, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { WorkloadDetailEvents } from '../../workloads/shared/detail-events'
import { useNetworkDetailScope } from '../shared/detail-scope'
import { NetworkDetailShell } from '../shared/detail-shell'
import { networkQueries } from '../shared/queries'
import { networkMutations } from '../shared/mutations'
import { networkResourcePath } from '../shared/resource-links'
import { ingressConfiguration, type ConfiguredIngressRoute } from './configuration'
import { IngressRulesTable, IngressBackends } from './rules-panel'
import { IngressEditModal } from './edit-modal'
import { ingressQueries } from './queries'
import { IngressTLSCard } from './tls-card'
import { secretCertificateQuery } from '../../configuration/secrets'
import { MAX_INGRESS_CERTIFICATE_READS } from './certificate-validity'
import type { Ingress } from './types'
import './styles.css'
const tabs = ['overview', 'rules', 'events', 'relationships', 'yaml']
export function IngressDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN',
    navigate = useNavigate(),
    client = useQueryClient(),
    { name = '' } = useParams()
  const scope = useNetworkDetailScope(),
    { clusterId } = scope,
    detailNamespace = scope.namespace ?? '',
    identity = JSON.stringify([clusterId, detailNamespace, name]),
    [tab, setTab] = useResourceDetailTab(),
    active = tabs.includes(tab) ? tab : 'overview'
  useEffect(() => {
    if (!tabs.includes(tab)) setTab('overview')
  }, [tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data,
    permitted = (key: string) => hasPermission(permissions, key),
    canRead = permitted('platform.network.ingresses.view'),
    canServices = permitted('platform.network.services.view'),
    canPods = permitted('platform.pods.view'),
    canEndpoints = permitted('platform.network.endpoint-slices.view'),
    canEvents = permitted('platform.workloads.overview.view'),
    refresh = useWorkloadRefresh()
  const options = ingressQueries.detail(scope, name),
    query = useQuery({
      ...options,
      enabled: Boolean(options.enabled) && canRead,
      refetchInterval: refresh.interval,
    }),
    returned = canRead ? readableQueryData(query) : undefined,
    ingress =
      returned?.name === name && returned.namespace === detailNamespace ? returned : undefined
  const yamlOptions = networkQueries.yaml('ingresses', scope, name),
    configurationQuery = useQuery({
      ...yamlOptions,
      enabled: Boolean(yamlOptions.enabled) && canRead && ['overview', 'rules'].includes(active),
      refetchInterval: refresh.interval,
    }),
    configurationData = canRead ? readableQueryData(configurationQuery) : undefined
  const configuration = useMemo(() => {
    if (!configurationData) return {}
    try {
      return { data: ingressConfiguration(configurationData.content, name, detailNamespace) }
    } catch (e) {
      return { error: e instanceof Error ? e.message : String(e) }
    }
  }, [configurationData, name, detailNamespace])
  const canCertificates = canRead && permitted('platform.configuration.secrets.view')
  const certificateNames = [
    ...new Set((configuration.data?.tls || []).map((item) => item.secretName).filter(Boolean)),
  ].slice(0, MAX_INGRESS_CERTIFICATE_READS)
  const certificateQueries = useQueries({
    queries: certificateNames.map((name) => ({
      ...secretCertificateQuery(scope, name),
      enabled:
        canCertificates && active === 'overview' && Boolean(scope.clusterId && scope.namespace),
      refetchInterval: refresh.interval,
    })),
  })
  const eventOptions = ingressQueries.events(scope, name),
    events = useQuery({
      ...eventOptions,
      enabled: Boolean(eventOptions.enabled) && canRead && canEvents && active === 'events',
      refetchInterval: refresh.interval,
    })
  const [editing, setEditing] = useState<string>()
  const remove = usePlatformMutation(networkMutations.remove('ingresses', client), {
    label: ['删除 Ingress', 'Delete Ingress'],
    target: platformResourceTarget,
  })
  const canUpdate =
      canRead &&
      permitted('platform.network.ingresses.update') &&
      hasAllowedAction(ingress?.allowedActions, 'update'),
    canDelete =
      canRead &&
      permitted('platform.network.ingresses.delete') &&
      hasAllowedAction(ingress?.allowedActions, 'delete')
  const actions = useKubernetesResourceActions<Ingress>({
    resourceKey: 'ingress-detail',
    resourceLabel: 'Ingress',
    records: ingress ? [ingress] : [],
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
                  ? '删除此 Ingress 会移除入口规则，不会删除后端 Service 或 Pod。'
                  : 'Deleting this Ingress removes its routes; backend Services and Pods remain.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/ingresses?${new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace })}`,
                )
              },
            },
          ]
        : [],
  })
  const feedback = (
    <>
      <ReadFeedback canRead={canRead} query={query} label="Ingress" />
      {['overview', 'rules'].includes(active) && (
        <ReadFeedback
          canRead={configurationQuery.isEnabled}
          query={configurationQuery}
          label="Ingress YAML"
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
  if (!ingress)
    return (
      <div className="soha-page">
        {feedback}
        {query.isLoading ? (
          <Card loading />
        ) : !query.isError && canRead ? (
          <ManagementState
            kind="not-found"
            description={zh ? 'Ingress 未找到' : 'Ingress not found'}
          />
        ) : null}
      </div>
    )
  const reference = (kind: string, resourceName: string, ns?: string) => {
    const path = networkResourcePath(permissions, {
      kind,
      name: resourceName,
      namespace: ns,
      clusterId,
    })
    return path ? <Link to={path}>{resourceName}</Link> : resourceName || '—'
  }
  const config = configuration.data
  const routes: ConfiguredIngressRoute[] | undefined =
    config?.routes ??
    ingress.routes?.map((r, i) => ({
      ...r,
      key: String(i),
      host: r.host || '',
      path: r.path || '',
      pathType: r.pathType || '',
      isDefault: r.pathType === 'Default',
    }))
  const activeQueries = [
    query,
    ...(configurationQuery.isEnabled || active === 'yaml'
      ? [{ ...configurationQuery, isEnabled: true }]
      : []),
    ...(events.isEnabled ? [events] : []),
    ...(canCertificates && active === 'overview' ? certificateQueries : []),
  ]
  const ruleTable = (preview = false) => (
    <IngressRulesTable
      ingress={ingress}
      routes={routes}
      reference={reference}
      canReadServices={canServices}
      preview={preview}
    />
  )
  const overview = (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '入口配置' : 'Ingress configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: 'IngressClass',
                  children:
                    config?.className || ingress.className
                      ? reference('IngressClass', config?.className || ingress.className || '')
                      : zh
                        ? '未显式指定'
                        : 'Not explicitly set',
                },
                ...(config?.legacyClass
                  ? [
                      {
                        label: zh ? '旧版 Class 注解' : 'Legacy class annotation',
                        children: config.legacyClass,
                      },
                    ]
                  : []),
                {
                  label: zh ? '地址' : 'Address',
                  children: ingress.address ? (
                    <Typography.Text copyable>{ingress.address}</Typography.Text>
                  ) : zh ? (
                    '未分配地址'
                  ) : (
                    'Address not assigned'
                  ),
                },
                ...(config?.createdAt
                  ? [{ label: zh ? '创建时间' : 'Created', children: config.createdAt }]
                  : []),
                {
                  label: zh ? '默认后端' : 'Default backend',
                  children: config
                    ? routes?.some((r) => r.isDefault)
                      ? zh
                        ? '已配置'
                        : 'Configured'
                      : zh
                        ? '未配置'
                        : 'Not configured'
                    : '—',
                },
              ]}
            />
          </Card>
          <ResourceGitOpsStatus
            clusterId={clusterId}
            kind="Ingress"
            name={ingress.name}
            namespace={ingress.namespace}
          />
        </>
      }
    >
      {configuration.error && <Alert showIcon type="warning" title={configuration.error} />}
      <Card
        className="soha-detail-card"
        title={zh ? '入口规则' : 'Ingress rules'}
        extra={
          <Button type="link" onClick={() => setTab('rules')}>
            {zh ? '查看全部规则' : 'View all rules'}
            {routes ? ` · ${routes.length}` : ''}
          </Button>
        }
      >
        {ruleTable(true)}
      </Card>
      <IngressTLSCard
        tls={config?.tls}
        canRead={canCertificates}
        queries={new Map(certificateNames.map((name, index) => [name, certificateQueries[index]]))}
        reference={(name) =>
          permitted('platform.configuration.secret-data.view')
            ? reference('Secret', name, ingress.namespace)
            : name
        }
      />
      <WorkloadOverviewMetadata labels={ingress.labels} annotations={ingress.annotations} />
    </WorkloadOverview>
  )
  return (
    <div className="soha-ingress-detail">
      {feedback}
      {actions.confirmation}
      <NetworkDetailShell
        key={identity}
        kind="ingresses"
        label="Ingress"
        detail={ingress}
        target={{ scope, name }}
        activeTabKey={active}
        onTabChange={setTab}
        tabOrder={tabs}
        preserveYaml
        canUpdateYAML={canUpdate}
        overview={overview}
        aiContext={{
          pinnedData: {
            className: ingress.className,
            address: ingress.address,
            hosts: ingress.hosts,
            activeTab: active,
          },
        }}
        header={
          <ResourceDetailHeader
            kind="Ingress"
            name={ingress.name}
            namespace={ingress.namespace}
            ageSeconds={ingress.ageSeconds}
            facts={ingress.className ? <MetadataTag label={ingress.className} /> : undefined}
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  resourceLabel="Ingress"
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  beforeRefresh={
                    canUpdate && (
                      <Button
                        aria-label={zh ? '编辑 Ingress' : 'Edit Ingress'}
                        disabled={remove.blocked}
                        onClick={() => setEditing(identity)}
                      >
                        {zh ? '编辑' : 'Edit'}
                      </Button>
                    )
                  }
                />
                {canDelete && (
                  <Button
                    danger
                    aria-label={zh ? '删除 Ingress' : 'Delete Ingress'}
                    disabled={remove.blocked}
                    onClick={() => actions.selectAction(ingress, 'delete')}
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
              <div className="soha-detail-stack">
                <Card
                  className="soha-detail-card"
                  title={zh ? '规则与默认后端' : 'Rules and default backend'}
                >
                  {ruleTable()}
                </Card>
                <IngressBackends
                  ingress={ingress}
                  reference={reference}
                  canReadServices={canServices}
                  canReadPods={canPods}
                  canReadEndpoints={canEndpoints}
                />
              </div>
            ),
          },
          {
            key: 'events',
            label: zh ? '事件' : 'Events',
            children:
              active === 'events' ? (
                canEvents ? (
                  <>
                    <ReadFeedback
                      canRead={events.isEnabled}
                      query={events}
                      label="Ingress events"
                    />
                    {events.isError && !readableQueryData(events) ? null : (
                      <>
                        <WorkloadDetailEvents
                          key={identity}
                          resourceLabel="Ingress"
                          events={readableQueryData(events) || []}
                          observedAt={events.dataUpdatedAt}
                          loading={events.isLoading}
                          onRefresh={() => void events.refetch()}
                          allowEstimatedTime={false}
                          scopeHint={
                            zh
                              ? '当前命名空间最近 100 条事件中的匹配记录'
                              : 'Matching records from the latest 100 namespace events'
                          }
                        />
                      </>
                    )}
                  </>
                ) : (
                  <Alert
                    showIcon
                    type="info"
                    title={zh ? '无事件查看权限' : 'Event access unavailable'}
                  />
                )
              ) : null,
          },
        ]}
        relatedResources={
          active === 'relationships' ? (
            <Suspense fallback={<Spin />}>
              <WorkloadRelationships
                key={identity}
                kind="Ingress"
                scope={scope}
                name={name}
                active={active === 'relationships'}
              />
            </Suspense>
          ) : null
        }
      />
      {canUpdate && editing === identity && (
        <IngressEditModal
          key={`edit-${identity}`}
          clusterId={clusterId}
          ingress={ingress}
          onClose={() => setEditing(undefined)}
        />
      )}
    </div>
  )
}
