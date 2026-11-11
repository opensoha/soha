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
import { Alert, Button, Card, Typography } from 'antd'
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
import { staleGatewayCondition, type GatewayCondition } from './gateway-configuration'
import { GatewayConditions, GatewayConditionTag } from './gateway-panels'
import { gatewayClassConfiguration } from './gatewayclass-configuration'
import { GatewayClassGateways } from './gatewayclass-panels'
import { gatewayAPIQueries } from './queries'
import type { GatewayClassDetail } from './types'
import './gatewayclass.css'

const tabs = ['overview', 'gateways', 'relationships', 'yaml']

export function GatewayClassDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { name = '' } = useParams()
  const navigate = useNavigate(),
    client = useQueryClient()
  const { clusterId } = useNetworkDetailScope(),
    scope = { clusterId, namespace: null },
    identity = JSON.stringify([clusterId, name])
  const [tab, setTab] = useResourceDetailTab(),
    active = tabs.includes(tab) ? tab : 'overview'
  useEffect(() => {
    if (!tabs.includes(tab)) setTab('overview')
  }, [tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.gateway-classes.${action}`)
  const canRead = permitted('view'),
    canGateways = hasPermission(permissions, 'platform.network.gateways.view')
  const refresh = useWorkloadRefresh()
  const options = gatewayAPIQueries.detail<GatewayClassDetail>('gatewayclasses', scope, name, true)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const returned = canRead ? readableQueryData(query) : undefined
  const identityMismatch = returned && (returned.name !== name || Boolean(returned.namespace))
  const detail = returned && !identityMismatch ? returned : undefined
  const yamlOptions = networkQueries.yaml('gatewayclasses', scope, name, true)
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
      return { data: gatewayClassConfiguration(source.content, name) }
    } catch {
      return {
        error: zh
          ? '无法读取此 GatewayClass 的完整配置，请查看 YAML。'
          : 'Cannot read the full GatewayClass configuration; inspect YAML.',
      }
    }
  }, [source, name, zh])
  const config = configuration?.data
  const canUpdate =
    canRead && permitted('update') && hasAllowedAction(detail?.allowedActions, 'update')
  const canDelete =
    canRead && permitted('delete') && hasAllowedAction(detail?.allowedActions, 'delete')
  const remove = usePlatformMutation(networkMutations.remove('gatewayclasses', client), {
    label: ['删除 GatewayClass', 'Delete GatewayClass'],
    target: platformResourceTarget,
  })
  const actions = useKubernetesResourceActions<GatewayClassDetail>({
    resourceKey: 'gatewayclass-detail',
    resourceLabel: 'GatewayClass',
    records: detail ? [detail] : [],
    getName: (record) => record.name,
    getKey: (record) => record.name,
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
                  ? '删除会移除网关类配置，可能影响引用它的 Gateway。'
                  : 'Deleting this class removes its configuration and may affect referencing Gateways.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/gateway-api/gatewayclasses?${new URLSearchParams({ clusterId: clusterId || '' })}`,
                )
              },
            },
          ]
        : [],
  })
  const activeQueries = [
    query,
    ...(configurationQuery.isEnabled || active === 'yaml'
      ? [{ ...configurationQuery, isEnabled: true }]
      : []),
  ]
  const feedback = (
    <>
      <ReadFeedback canRead={canRead} query={query} label="GatewayClass" />
      {configurationQuery.isEnabled && (
        <ReadFeedback canRead={canRead} query={configurationQuery} label="GatewayClass YAML" />
      )}
      {configuration?.error && <Alert type="warning" showIcon title={configuration.error} />}
      {identityMismatch && (
        <Alert
          type="error"
          showIcon
          title={
            zh
              ? '返回的 GatewayClass 与当前资源不一致，请重新读取。'
              : 'The returned GatewayClass does not match this resource; retry the read.'
          }
        />
      )}
    </>
  )
  if (!clusterId)
    return (
      <ManagementState kind="select-scope" description={zh ? '请选择集群' : 'Select a cluster'} />
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
            description={zh ? 'GatewayClass 未找到' : 'GatewayClass not found'}
          />
        ) : null}
      </div>
    )

  const conditions = config?.conditions ?? detail.conditions ?? []
  const accepted: GatewayCondition = conditions.find((item) => item.type === 'Accepted') ?? {
    type: 'Accepted',
    status: config ? 'Unknown' : detail.accepted || 'Unknown',
  }
  const stale = staleGatewayCondition(accepted, config?.generation)
  const gateways = canGateways
    ? (detail.gateways || []).filter((item) => !item.gatewayClass || item.gatewayClass === name)
    : []
  const parameters = config?.parameters
  const parameterPath =
    parameters &&
    !parameters.group &&
    parameters.namespace &&
    ['ConfigMap', 'Secret'].includes(parameters.kind) &&
    (parameters.kind !== 'Secret' ||
      hasPermission(permissions, 'platform.configuration.secret-data.view'))
      ? networkResourcePath(permissions, { ...parameters, clusterId })
      : null
  const parameterContent = parameters ? (
    <WorkloadOverviewConfiguration
      items={[
        { label: 'Kind', children: parameters.kind },
        {
          label: zh ? '名称' : 'Name',
          children: parameterPath ? (
            <Link className="soha-gateway-wrap" to={parameterPath}>
              {parameters.name}
            </Link>
          ) : (
            <Typography.Text className="soha-gateway-wrap" copyable>
              {parameters.name}
            </Typography.Text>
          ),
        },
        { label: 'Group', children: parameters.group || 'core' },
        ...(parameters.namespace
          ? [{ label: zh ? '命名空间' : 'Namespace', children: parameters.namespace }]
          : []),
      ]}
    />
  ) : (
    <Typography.Text className="soha-gateway-wrap" type="secondary">
      {parameters === null
        ? zh
          ? '未配置参数引用'
          : 'No parameters reference configured'
        : detail.parametersRef ||
          (configurationQuery.isFetching
            ? zh
              ? '正在读取配置…'
              : 'Reading configuration…'
            : zh
              ? '未配置参数引用'
              : 'No parameters reference configured')}
    </Typography.Text>
  )
  const parametersCard = (
    <Card className="soha-detail-card" title={zh ? '参数引用' : 'Parameters reference'}>
      {parameterContent}
    </Card>
  )
  const showGateways = () => setTab('gateways')
  const overview = (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '类配置' : 'Class configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '控制器' : 'Controller',
                  children: (
                    <Typography.Text
                      className="soha-gateway-wrap"
                      copyable={Boolean(detail.controllerName)}
                    >
                      {detail.controllerName || '—'}
                    </Typography.Text>
                  ),
                },
                { label: zh ? '作用域' : 'Scope', children: zh ? '集群' : 'Cluster' },
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
          {parametersCard}
        </>
      }
    >
      <Alert
        showIcon
        type={
          stale
            ? 'warning'
            : accepted.status === 'True'
              ? 'success'
              : accepted.status === 'False'
                ? 'error'
                : 'info'
        }
        title={
          stale
            ? zh
              ? 'Accepted 条件尚未反映当前配置'
              : 'Accepted has not observed this configuration'
            : accepted.status === 'True'
              ? zh
                ? '控制器已接纳此 GatewayClass'
                : 'Controller accepted this GatewayClass'
              : accepted.status === 'False'
                ? zh
                  ? '控制器未接纳此 GatewayClass'
                  : 'Controller has not accepted this GatewayClass'
                : zh
                  ? '尚未确认此 GatewayClass 的接纳状态'
                  : 'GatewayClass acceptance is not confirmed'
        }
        description={
          accepted.status === 'False'
            ? [accepted.reason, accepted.message].filter(Boolean).join(' · ') || undefined
            : undefined
        }
      />
      {config?.description && (
        <Card className="soha-detail-card" title={zh ? '描述' : 'Description'}>
          <span className="soha-gateway-wrap">{config.description}</span>
        </Card>
      )}
      <GatewayClassGateways
        items={gateways}
        clusterId={clusterId}
        canRead={canGateways}
        preview
        onShowAll={showGateways}
      />
      <GatewayConditions
        conditions={conditions.length ? conditions : [accepted]}
        generation={config?.generation}
        title={zh ? '控制器条件' : 'Controller conditions'}
      />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
  return (
    <div className="soha-gatewayclass-detail">
      {feedback}
      {actions.confirmation}
      {!actions.isConfirming && remove.feedback}
      <NetworkDetailShell
        key={identity}
        activeTabKey={active}
        onTabChange={setTab}
        clusterScoped
        kind="gatewayclasses"
        label="GatewayClass"
        detail={detail}
        target={{ scope, name }}
        preserveYaml
        canUpdateYAML={canUpdate}
        tabOrder={tabs}
        overview={overview}
        header={
          <ResourceDetailHeader
            kind="GatewayClass"
            name={detail.name}
            ageSeconds={detail.ageSeconds}
            backTo="/network/gateway-api/gatewayclasses"
            facts={<GatewayConditionTag condition={accepted} generation={config?.generation} />}
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  resourceLabel="GatewayClass"
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  beforeRefresh={
                    canUpdate ? (
                      <Button
                        aria-label={zh ? '编辑 GatewayClass' : 'Edit GatewayClass'}
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
                    aria-label={zh ? '删除 GatewayClass' : 'Delete GatewayClass'}
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
            key: 'gateways',
            label: 'Gateways',
            children: (
              <GatewayClassGateways
                key={identity}
                items={gateways}
                clusterId={clusterId}
                canRead={canGateways}
              />
            ),
          },
          {
            key: 'relationships',
            label: zh ? '关联资源' : 'Related resources',
            children: (
              <WorkloadRelationships
                key={identity}
                kind="GatewayClass"
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
