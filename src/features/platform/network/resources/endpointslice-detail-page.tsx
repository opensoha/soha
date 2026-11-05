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
import { SliceEndpoints, SlicePorts } from './endpointslice-panels'
import { sliceConfiguration, sliceEndpointKey } from './endpointslice-model'
import { networkCoreQueries } from './queries'
import type { EndpointSliceDetail, EndpointSliceEndpoint } from './types'

const tabs = ['overview', 'endpoints', 'relationships', 'yaml']
export function EndpointSliceDetailPage() {
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
    hasPermission(permissions, `platform.network.endpoint-slices.${action}`)
  const canRead = permitted('view'),
    refresh = useWorkloadRefresh()
  const options = networkCoreQueries.detail<EndpointSliceDetail>('endpointslices', scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const yamlOptions = networkQueries.yaml('endpointslices', scope, name)
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
      return { data: sliceConfiguration(source.content, detail) }
    } catch {
      return { error: true }
    }
  }, [source, detail])
  const config = configuration?.data
  const canUpdate =
    canRead && permitted('update') && hasAllowedAction(detail?.allowedActions, 'update')
  const canDelete =
    canRead && permitted('delete') && hasAllowedAction(detail?.allowedActions, 'delete')
  const remove = usePlatformMutation(networkMutations.remove('endpointslices', client), {
    label: ['删除 EndpointSlice', 'Delete EndpointSlice'],
    target: platformResourceTarget,
  })
  const actions = useKubernetesResourceActions<EndpointSliceDetail>({
    resourceKey: 'endpointslice-detail',
    resourceLabel: 'EndpointSlice',
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
                  ? '删除会移除此端点切片，可能影响 Service 的后端发现；由控制器管理的切片可能被重建。'
                  : 'Deleting this slice may affect Service backend discovery. A managing controller may recreate it.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/endpointslices?${new URLSearchParams({ clusterId: clusterId || '', namespace: ns })}`,
                )
              },
            },
          ]
        : [],
  })
  const feedback = (
    <>
      <ReadFeedback canRead={canRead} query={query} label="EndpointSlice" />
      {yamlQuery.isEnabled && (
        <ReadFeedback canRead={canRead} query={yamlQuery} label="EndpointSlice YAML" />
      )}
      {configuration?.error && (
        <Alert
          type="warning"
          showIcon
          title={
            zh
              ? '完整端点配置未能读取，保留接口摘要；目标命名空间未确认时不猜测跳转。'
              : 'Full endpoint configuration unavailable; retaining the API summary without guessing target namespaces.'
          }
        />
      )}
      {mismatch && (
        <Alert
          type="error"
          showIcon
          title={
            zh
              ? '返回资源与当前 EndpointSlice 不一致，请重新读取。'
              : 'Returned resource does not match this EndpointSlice; retry the read.'
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
            description={zh ? 'EndpointSlice 未找到' : 'EndpointSlice not found'}
          />
        ) : null}
      </div>
    )
  const link = (kind: 'Service' | 'Pod' | 'Node', name: string, namespace?: string) => {
    const path = networkResourcePath(permissions, { clusterId, kind, name, namespace })
    return path ? (
      <Link className="soha-gateway-wrap" to={path}>
        {name}
      </Link>
    ) : (
      <span className="soha-gateway-wrap">{name}</span>
    )
  }
  const target = (e: EndpointSliceEndpoint) => {
    const targetNS = config?.targetNamespaces[sliceEndpointKey(e)]
    return e.targetRef?.startsWith('Pod/') && targetNS ? (
      link('Pod', e.targetRef.slice(4), targetNS)
    ) : (
      <span className="soha-gateway-wrap">{e.targetRef || '—'}</span>
    )
  }
  const records = detail.endpoints
  const overview = (
    <WorkloadOverview
      aside={
        <Card className="soha-detail-card" title={zh ? '发现配置' : 'Discovery configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '命名空间' : 'Namespace', children: ns },
              {
                label: 'Service',
                children: detail.serviceName
                  ? link('Service', detail.serviceName, ns)
                  : zh
                    ? '本次未返回关联'
                    : 'No association returned',
              },
              { label: zh ? '地址族' : 'Address type', children: detail.addressType || '—' },
              {
                label: zh ? '管理来源' : 'Managed by',
                children: (
                  <span className="soha-gateway-wrap">
                    {detail.labels?.['endpointslice.kubernetes.io/managed-by'] ||
                      (zh ? '未提供' : 'Not reported')}
                  </span>
                ),
              },
              { label: zh ? '端口' : 'Ports', children: <SlicePorts ports={detail.ports} /> },
            ]}
          />
        </Card>
      }
    >
      <Card
        className="soha-detail-card"
        title={zh ? '端点概况' : 'Endpoint summary'}
        extra={
          <Button type="link" onClick={() => setTab('endpoints')}>
            {zh ? '查看端点' : 'View endpoints'}
          </Button>
        }
      >
        <WorkloadOverviewConfiguration
          items={[
            {
              label: zh ? '原生端点数' : 'Native endpoints',
              children: config?.endpointCount ?? '—',
            },
            { label: zh ? '地址记录' : 'Address records', children: records?.length ?? '—' },
            {
              label: 'Ready / False',
              children: records
                ? `${records.filter((e) => e.ready === true).length} / ${records.filter((e) => e.ready === false).length}`
                : '—',
            },
            {
              label: zh ? '未提供 Ready' : 'Ready unreported',
              children: records?.filter((e) => e.ready === undefined).length ?? '—',
            },
            {
              label: 'Terminating',
              children: records?.filter((e) => e.terminating === true).length ?? '—',
            },
          ]}
        />
        <span className="soha-slice-note">
          {zh
            ? '条件统计按地址记录计数；同一端点可有多个地址，不等于独立 Pod 数量。'
            : 'Condition counts use address records; an endpoint may have multiple addresses and is not necessarily a Pod.'}
        </span>
      </Card>
      <Card className="soha-detail-card" title={zh ? '地址预览' : 'Address preview'}>
        {records === undefined ? (
          <span>{zh ? '地址记录未返回' : 'Address records not returned'}</span>
        ) : records.length ? (
          <div className="soha-gateway-values">
            {records.slice(0, 6).map((e, i) => (
              <div className="soha-gateway-wrap" key={JSON.stringify([e.address, i])}>
                <strong>{e.address}</strong> · {target(e)}
                {e.nodeName && <> · {link('Node', e.nodeName)}</>}
              </div>
            ))}
          </div>
        ) : (
          <ManagementState
            compact
            kind="empty"
            description={zh ? '本次返回的地址记录为空' : 'No address records returned'}
          />
        )}
        {records && records.length > 6 && (
          <Button type="link" onClick={() => setTab('endpoints')}>
            {zh
              ? `查看全部 ${records.length} 条地址记录`
              : `View all ${records.length} address records`}
          </Button>
        )}
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
  return (
    <div className="soha-endpointslice-detail">
      {feedback}
      {actions.confirmation}
      {!actions.isConfirming && remove.feedback}
      <NetworkDetailShell
        key={identity}
        activeTabKey={active}
        onTabChange={setTab}
        kind="endpointslices"
        label="EndpointSlice"
        detail={detail}
        target={{ scope, name }}
        preserveYaml
        canUpdateYAML={canUpdate}
        tabOrder={tabs}
        overview={overview}
        header={
          <ResourceDetailHeader
            kind="EndpointSlice"
            name={detail.name}
            namespace={ns}
            ageSeconds={detail.ageSeconds}
            backTo="/network/gateway-api/endpointslices"
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  resourceLabel="EndpointSlice"
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  beforeRefresh={
                    canUpdate ? (
                      <Button
                        disabled={remove.blocked}
                        aria-label={zh ? '编辑 EndpointSlice' : 'Edit EndpointSlice'}
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
                    aria-label={zh ? '删除 EndpointSlice' : 'Delete EndpointSlice'}
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
            key: 'endpoints',
            label: zh ? '端点' : 'Endpoints',
            children: <SliceEndpoints key={identity} detail={detail} link={link} target={target} />,
          },
          {
            key: 'relationships',
            label: zh ? '关联资源' : 'Related resources',
            children: (
              <WorkloadRelationships
                key={identity}
                kind="EndpointSlice"
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
