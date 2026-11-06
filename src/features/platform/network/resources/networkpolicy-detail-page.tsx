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
import { PolicySelector, PolicyTypes, PolicyRules, PolicyPods } from './networkpolicy-panels'
import { policyConfiguration } from './networkpolicy-model'
import { networkCoreQueries } from './queries'
import type { NetworkPolicyDetail, NetworkPolicyPod } from './types'

const tabs = ['overview', 'rules', 'pods', 'relationships', 'yaml']
export function NetworkPolicyDetailPage() {
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
    hasPermission(permissions, `platform.network.network-policies.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const options = networkCoreQueries.detail<NetworkPolicyDetail>('networkpolicies', scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const yamlOptions = networkQueries.yaml('networkpolicies', scope, name)
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled:
      Boolean(yamlOptions.enabled) && canRead && active !== 'yaml' && active !== 'relationships',
    refetchInterval: refresh.interval,
  })
  const activeQueries = [
    query,
    ...(yamlQuery.isEnabled || active === 'yaml' ? [{ ...yamlQuery, isEnabled: true }] : []),
  ]
  const returned = canRead ? readableQueryData(query) : undefined
  const mismatch = returned && (returned.name !== name || returned.namespace !== ns)
  const detail = returned && !mismatch ? returned : undefined
  const source = canRead ? readableQueryData(yamlQuery) : undefined
  const configuration = useMemo(() => {
    if (!source || !detail) return undefined
    try {
      return { data: policyConfiguration(source.content, detail) }
    } catch {
      return { error: true }
    }
  }, [source, detail])
  const config = configuration?.data
  const canUpdate =
    canRead && permitted('update') && hasAllowedAction(detail?.allowedActions, 'update')
  const canDelete =
    canRead && permitted('delete') && hasAllowedAction(detail?.allowedActions, 'delete')
  const remove = usePlatformMutation(networkMutations.remove('networkpolicies', client), {
    label: ['删除 NetworkPolicy', 'Delete NetworkPolicy'],
    target: platformResourceTarget,
  })
  const actions = useKubernetesResourceActions<NetworkPolicyDetail>({
    resourceKey: 'networkpolicy-detail',
    resourceLabel: 'NetworkPolicy',
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
                  ? '删除会改变所选 Pods 的网络隔离与放行范围；实际结果还取决于其他策略与 CNI。'
                  : 'Deleting this policy changes selected Pod isolation and allowances; other policies and CNI enforcement also affect the result.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/networkpolicies?${new URLSearchParams({ clusterId: clusterId || '', namespace: ns })}`,
                )
              },
            },
          ]
        : [],
  })
  const feedback = (
    <>
      <ReadFeedback canRead={canRead} query={query} label="NetworkPolicy" />
      {yamlQuery.isEnabled && (
        <ReadFeedback canRead={canRead} query={yamlQuery} label="NetworkPolicy YAML" />
      )}
      {configuration?.error && (
        <Alert
          type="warning"
          showIcon
          title={
            zh
              ? '完整规则未能读取，保留接口摘要；不能据此推断隔离或放行范围。'
              : 'Full rules unavailable; retaining the API summary without inferring isolation or allowances.'
          }
        />
      )}
      {mismatch && (
        <Alert
          type="error"
          showIcon
          title={
            zh
              ? '返回资源与当前 NetworkPolicy 不一致，请重新读取。'
              : 'Returned resource does not match this NetworkPolicy; retry the read.'
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
            description={zh ? 'NetworkPolicy 未找到' : 'NetworkPolicy not found'}
          />
        ) : null}
      </div>
    )
  const canReadPods = hasPermission(permissions, 'platform.pods.view')
  const podLink = (p: NetworkPolicyPod) => {
    const path = canReadPods
      ? networkResourcePath(permissions, {
          clusterId,
          kind: 'Pod',
          name: p.name,
          namespace: p.namespace,
        })
      : null
    return path ? (
      <Link className="soha-gateway-wrap" to={path}>
        {p.name}
      </Link>
    ) : (
      <span className="soha-gateway-wrap">{p.name}</span>
    )
  }
  const overview = (
    <WorkloadOverview
      aside={
        <Card className="soha-detail-card" title={zh ? '策略配置' : 'Policy configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '命名空间' : 'Namespace', children: ns },
              {
                label: zh ? '隔离方向' : 'Policy types',
                children: (
                  <PolicyTypes
                    types={
                      config
                        ? config.directions.filter((d) => d.isolated).map((d) => d.name)
                        : detail.policyTypes
                    }
                  />
                ),
              },
              {
                label: zh ? '已返回 Pods' : 'Returned Pods',
                children: canReadPods
                  ? (detail.matchingPods?.length ?? '—')
                  : zh
                    ? '无查看权限'
                    : 'No view permission',
              },
              {
                label: zh ? '执行状态' : 'Enforcement',
                children: zh ? '未验证 CNI 执行或流量' : 'CNI enforcement and traffic not verified',
              },
            ]}
          />
        </Card>
      }
    >
      <Card
        className="soha-detail-card"
        title={zh ? 'Pod 选择范围' : 'Pod selection'}
        extra={
          <Button type="link" onClick={() => setTab('pods')}>
            {zh ? '查看选中 Pods' : 'Selected Pods'}
          </Button>
        }
      >
        {config ? (
          <PolicySelector
            selector={config.selector}
            all={zh ? '本命名空间全部 Pods' : 'All Pods in this namespace'}
          />
        ) : (
          <span className="soha-gateway-wrap">
            {detail.podSelector ||
              (zh ? '完整选择器未返回，可查看 YAML' : 'Full selector not returned; inspect YAML')}
          </span>
        )}
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? '隔离与规则' : 'Isolation and rules'}
        extra={
          <Button type="link" onClick={() => setTab('rules')}>
            {zh ? '查看规则' : 'View rules'}
          </Button>
        }
      >
        {config ? (
          <WorkloadOverviewConfiguration
            items={config.directions.map((d) => ({
              label: d.name,
              children: !d.isolated
                ? zh
                  ? '此策略不隔离该方向'
                  : 'Not isolated by this policy'
                : !d.rules.length
                  ? zh
                    ? '纳入隔离 · 此策略无放行规则'
                    : 'Isolated · no allow rule in this policy'
                  : zh
                    ? `纳入隔离 · ${d.rules.length} 条放行规则`
                    : `Isolated · ${d.rules.length} allow rules`,
            }))}
          />
        ) : (
          <span>
            {zh
              ? '完整规则未返回，隔离范围待确认'
              : 'Full rules unavailable; isolation scope unconfirmed'}
          </span>
        )}
        <p className="soha-policy-note">
          {zh
            ? '多条策略的放行规则取并集；此处展示配置，不代表已验证流量允许或阻断。'
            : 'Allowances from applicable policies are additive; this configuration does not establish allowed or blocked traffic.'}
        </p>
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
  return (
    <div className="soha-networkpolicy-detail">
      {feedback}
      {actions.confirmation}
      {!actions.isConfirming && remove.feedback}
      <NetworkDetailShell
        key={identity}
        activeTabKey={active}
        onTabChange={setTab}
        kind="networkpolicies"
        label="NetworkPolicy"
        detail={detail}
        target={{ scope, name }}
        preserveYaml
        canUpdateYAML={canUpdate}
        tabOrder={tabs}
        overview={overview}
        header={
          <ResourceDetailHeader
            kind="NetworkPolicy"
            name={detail.name}
            namespace={ns}
            ageSeconds={detail.ageSeconds}
            backTo="/network/gateway-api/networkpolicies"
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  resourceLabel="NetworkPolicy"
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  beforeRefresh={
                    canUpdate ? (
                      <Button
                        disabled={remove.blocked}
                        aria-label={zh ? '编辑 NetworkPolicy' : 'Edit NetworkPolicy'}
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
                    aria-label={zh ? '删除 NetworkPolicy' : 'Delete NetworkPolicy'}
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
            label: zh ? '规则' : 'Rules',
            children: <PolicyRules detail={detail} config={config} />,
          },
          {
            key: 'pods',
            label: zh ? '选中 Pods' : 'Selected Pods',
            children: <PolicyPods detail={detail} link={podLink} canRead={canReadPods} />,
          },
          {
            key: 'relationships',
            label: zh ? '关联资源' : 'Related resources',
            children: (
              <WorkloadRelationships
                key={identity}
                kind="NetworkPolicy"
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
