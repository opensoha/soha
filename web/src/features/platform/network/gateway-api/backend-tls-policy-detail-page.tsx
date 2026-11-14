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
import { useEffect, useMemo } from 'react'
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
import { backendTLSConfiguration, type PolicyReference } from './backend-tls-configuration'
import { PolicyReferences, PolicyValidation, PolicyConditions } from './backend-tls-panels'
import { gatewayAPIQueries } from './queries'
import type { BackendTLSPolicyDetail } from './types'

const tabs = ['overview', 'relationships', 'yaml']
export function BackendTLSPolicyDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { name = '' } = useParams()
  const navigate = useNavigate(),
    client = useQueryClient()
  const scope = useNetworkDetailScope(),
    { clusterId } = scope,
    ns = scope.namespace ?? '',
    identity = JSON.stringify([clusterId, ns, name])
  const [tab, setTab] = useResourceDetailTab(),
    active = tabs.includes(tab) ? tab : 'overview'
  useEffect(() => {
    if (!tabs.includes(tab)) setTab('overview')
  }, [tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.backend-tls-policies.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const options = gatewayAPIQueries.detail<BackendTLSPolicyDetail>(
    'backendtlspolicies',
    scope,
    name,
  )
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const returned = canRead ? readableQueryData(query) : undefined
  const mismatch = returned && (returned.name !== name || returned.namespace !== ns),
    detail = returned && !mismatch ? returned : undefined
  const yamlOptions = networkQueries.yaml('backendtlspolicies', scope, name)
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled:
      Boolean(yamlOptions.enabled) && canRead && active !== 'yaml' && active !== 'relationships',
    refetchInterval: refresh.interval,
  })
  const source = canRead ? readableQueryData(yamlQuery) : undefined
  const configuration = useMemo(() => {
    if (!source || !detail) return undefined
    try {
      return { data: backendTLSConfiguration(source.content, detail) }
    } catch {
      return {
        error: zh
          ? '完整 TLS 配置未能读取，当前保留接口摘要；可查看 YAML。'
          : 'Full TLS configuration is unavailable; showing the API summary. Inspect YAML.',
      }
    }
  }, [source, detail, zh])
  const config = configuration?.data
  const canUpdate =
    canRead && permitted('update') && hasAllowedAction(detail?.allowedActions, 'update')
  const canDelete =
    canRead && permitted('delete') && hasAllowedAction(detail?.allowedActions, 'delete')
  const remove = usePlatformMutation(networkMutations.remove('backendtlspolicies', client), {
    label: ['删除 BackendTLSPolicy', 'Delete BackendTLSPolicy'],
    target: platformResourceTarget,
  })
  const actions = useKubernetesResourceActions<BackendTLSPolicyDetail>({
    resourceKey: 'backend-tls-detail',
    resourceLabel: 'BackendTLSPolicy',
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
                  ? '删除会移除此后端 TLS 校验策略，可能改变网关到后端的连接行为。'
                  : 'Deleting this backend TLS policy may change Gateway-to-backend connections.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/gateway-api/backendtlspolicies?${new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace })}`,
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
      <ReadFeedback canRead={canRead} query={query} label="BackendTLSPolicy" />
      {yamlQuery.isEnabled && (
        <ReadFeedback canRead={canRead} query={yamlQuery} label="BackendTLSPolicy YAML" />
      )}
      {configuration?.error && <Alert type="warning" showIcon title={configuration.error} />}
      {mismatch && (
        <Alert
          type="error"
          showIcon
          title={
            zh
              ? '返回资源与当前 BackendTLSPolicy 不一致，请重新读取。'
              : 'Returned resource does not match this BackendTLSPolicy; retry the read.'
          }
        />
      )}
    </>
  )
  if (!clusterId || !ns)
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
            description={zh ? 'BackendTLSPolicy 未找到' : 'BackendTLSPolicy not found'}
          />
        ) : null}
      </div>
    )
  const reference = (ref: PolicyReference) => {
    const known =
      ref.kind === 'Gateway'
        ? ref.group === 'gateway.networking.k8s.io'
        : ['Service', 'ConfigMap', 'Secret'].includes(ref.kind) && ref.group === ''
    const path =
      known &&
      (ref.kind !== 'Secret' ||
        hasPermission(permissions, 'platform.configuration.secret-data.view'))
        ? networkResourcePath(permissions, { ...ref, clusterId })
        : null
    return path ? (
      <Link className="soha-gateway-wrap" title={ref.name} to={path}>
        {ref.name}
      </Link>
    ) : (
      <span className="soha-gateway-wrap">{ref.name}</span>
    )
  }
  const validation = (
    <PolicyValidation detail={detail} config={config} renderReference={reference} />
  )
  const overview = (
    <WorkloadOverview
      aside={
        <Card className="soha-detail-card" title={zh ? '策略配置' : 'Policy configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '命名空间' : 'Namespace', children: ns },
              {
                label: 'Hostname',
                children: <span className="soha-gateway-wrap">{detail.hostname || '—'}</span>,
              },
              {
                label: zh ? 'CA 来源' : 'CA source',
                children:
                  detail.wellKnownCACertificates ||
                  (detail.caCertificateRefs?.length ? (zh ? 'CA 引用' : 'CA references') : '—'),
              },
              ...(config?.generation !== undefined
                ? [{ label: zh ? '配置代次' : 'Generation', children: config.generation }]
                : []),
              ...(config?.createdAt
                ? [
                    {
                      label: zh ? '创建时间' : 'Created',
                      children: formatDateTime(config.createdAt),
                    },
                  ]
                : []),
            ]}
          />
        </Card>
      }
    >
      {validation}
      <PolicyReferences
        title={zh ? '应用目标' : 'Targets'}
        references={config?.targets}
        labels={detail.targetRefs}
        renderReference={reference}
      />
      <PolicyConditions detail={detail} config={config} renderReference={reference} />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
  return (
    <div className="soha-backend-tls-detail">
      {feedback}
      {actions.confirmation}
      {!actions.isConfirming && remove.feedback}
      <NetworkDetailShell
        key={identity}
        activeTabKey={active}
        onTabChange={setTab}
        kind="backendtlspolicies"
        label="BackendTLSPolicy"
        detail={detail}
        target={{ scope, name }}
        preserveYaml
        canUpdateYAML={canUpdate}
        tabOrder={tabs}
        overview={overview}
        header={
          <ResourceDetailHeader
            kind="BackendTLSPolicy"
            name={detail.name}
            namespace={ns}
            ageSeconds={detail.ageSeconds}
            backTo="/network/gateway-api/backendtlspolicies"
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  resourceLabel="BackendTLSPolicy"
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  beforeRefresh={
                    canUpdate ? (
                      <Button
                        disabled={remove.blocked}
                        aria-label={zh ? '编辑 BackendTLSPolicy' : 'Edit BackendTLSPolicy'}
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
                    aria-label={zh ? '删除 BackendTLSPolicy' : 'Delete BackendTLSPolicy'}
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
            key: 'relationships',
            label: zh ? '关联资源' : 'Related resources',
            children: (
              <WorkloadRelationships
                key={identity}
                kind="BackendTLSPolicy"
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
