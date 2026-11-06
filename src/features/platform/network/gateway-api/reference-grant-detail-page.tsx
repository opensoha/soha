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
import { useEffect } from 'react'
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
import { GrantRules } from './reference-grant-panels'
import { gatewayAPIQueries } from './queries'
import type { ReferenceGrantDetail, ReferenceGrantTo } from './types'

const tabs = ['overview', 'relationships', 'yaml']
export function ReferenceGrantDetailPage() {
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
    hasPermission(permissions, `platform.network.reference-grants.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const options = gatewayAPIQueries.detail<ReferenceGrantDetail>('referencegrants', scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  // Observe the shell-owned YAML read for refresh feedback without another eager read.
  const yamlQuery = useQuery({
    ...networkQueries.yaml('referencegrants', scope, name),
    enabled: false,
  })
  const activeQueries = [query, ...(active === 'yaml' ? [{ ...yamlQuery, isEnabled: true }] : [])]
  const returned = canRead ? readableQueryData(query) : undefined
  const mismatch = returned && (returned.name !== name || returned.namespace !== ns)
  const detail = returned && !mismatch ? returned : undefined
  const canUpdate =
    canRead && permitted('update') && hasAllowedAction(detail?.allowedActions, 'update')
  const canDelete =
    canRead && permitted('delete') && hasAllowedAction(detail?.allowedActions, 'delete')
  const remove = usePlatformMutation(networkMutations.remove('referencegrants', client), {
    label: ['删除 ReferenceGrant', 'Delete ReferenceGrant'],
    target: platformResourceTarget,
  })
  const actions = useKubernetesResourceActions<ReferenceGrantDetail>({
    resourceKey: 'reference-grant-detail',
    resourceLabel: 'ReferenceGrant',
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
                  ? '删除会撤销此跨命名空间引用授权，可能使依赖它的路由或证书引用失效。'
                  : 'Deleting this cross-namespace grant may invalidate dependent route or certificate references.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/gateway-api/referencegrants?${new URLSearchParams({ clusterId: clusterId || '', namespace: ns })}`,
                )
              },
            },
          ]
        : [],
  })
  const feedback = (
    <>
      <ReadFeedback canRead={canRead} query={query} label="ReferenceGrant" />
      {mismatch && (
        <Alert
          type="error"
          showIcon
          title={
            zh
              ? '返回资源与当前 ReferenceGrant 不一致，请重新读取。'
              : 'Returned resource does not match this ReferenceGrant; retry the read.'
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
            description={zh ? 'ReferenceGrant 未找到' : 'ReferenceGrant not found'}
          />
        ) : null}
      </div>
    )
  const renderTarget = (r: ReferenceGrantTo) => {
    if (!r.name) return <span>{zh ? '全部名称' : 'All names'}</span>
    const known = r.group === '' && ['Service', 'Secret'].includes(r.kind)
    const path =
      known &&
      (r.kind !== 'Secret' || hasPermission(permissions, 'platform.configuration.secret-data.view'))
        ? networkResourcePath(permissions, { clusterId, namespace: ns, kind: r.kind, name: r.name })
        : null
    return path ? (
      <Link className="soha-gateway-wrap" to={path}>
        {r.name}
      </Link>
    ) : (
      <span className="soha-gateway-wrap">{r.name}</span>
    )
  }
  const overview = (
    <WorkloadOverview
      aside={
        <Card className="soha-detail-card" title={zh ? '授权配置' : 'Grant configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '目标命名空间' : 'Target namespace',
                children: <span className="soha-gateway-wrap">{ns}</span>,
              },
              {
                label: zh ? '来源项' : 'Sources',
                children: detail.fromRefs?.length ?? detail.from?.length ?? '—',
              },
              {
                label: zh ? '目标项' : 'Targets',
                children: detail.toRefs?.length ?? detail.to?.length ?? '—',
              },
              {
                label: zh ? '作用范围' : 'Scope',
                children: zh ? '来源任一项 → 目标任一项' : 'Any source entry → any target entry',
              },
            ]}
          />
        </Card>
      }
    >
      <Card
        className="soha-detail-card"
        title={zh ? '跨命名空间引用授权' : 'Cross-namespace reference grant'}
      >
        <span>
          {zh
            ? 'From 和 To 各项为 OR，任一匹配来源可引用任一匹配目标。仅允许跨命名空间引用，不授予 Soha 用户权限或放通网络流量。'
            : 'From and To entries are OR-combined: any matching source may reference any matching target. This permits cross-namespace references, not Soha user access or network traffic.'}
        </span>
      </Card>
      <GrantRules detail={detail} renderTarget={renderTarget} />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
  return (
    <div className="soha-reference-grant-detail">
      {feedback}
      {actions.confirmation}
      {!actions.isConfirming && remove.feedback}
      <NetworkDetailShell
        key={identity}
        activeTabKey={active}
        onTabChange={setTab}
        kind="referencegrants"
        label="ReferenceGrant"
        detail={detail}
        target={{ scope, name }}
        preserveYaml
        canUpdateYAML={canUpdate}
        tabOrder={tabs}
        overview={overview}
        header={
          <ResourceDetailHeader
            kind="ReferenceGrant"
            name={detail.name}
            namespace={ns}
            ageSeconds={detail.ageSeconds}
            backTo="/network/gateway-api/referencegrants"
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  resourceLabel="ReferenceGrant"
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  beforeRefresh={
                    canUpdate ? (
                      <Button
                        disabled={remove.blocked}
                        aria-label={zh ? '编辑 ReferenceGrant' : 'Edit ReferenceGrant'}
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
                    aria-label={zh ? '删除 ReferenceGrant' : 'Delete ReferenceGrant'}
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
                kind="ReferenceGrant"
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
