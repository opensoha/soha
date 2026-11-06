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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Typography } from 'antd'
import { useMemo } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
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
import { ingressClassParameters } from './ingressclass-configuration'
import { IngressClassIngresses } from './ingressclass-panels'
import { networkCoreQueries } from './queries'
import type { IngressClassDetail } from './types'
import './ingressclass.css'

const tabs = ['overview', 'ingresses', 'relationships', 'yaml']
export function IngressClassDetailPage() {
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
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (action: string) =>
    hasPermission(permissions, `platform.network.ingress-classes.${action}`)
  const canRead = permitted('view'),
    canIngresses = hasPermission(permissions, 'platform.network.ingresses.view')
  const refresh = useWorkloadRefresh(),
    options = networkCoreQueries.detail<IngressClassDetail>('ingressclasses', scope, name, true)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const detail = canRead ? readableQueryData(query) : undefined
  const yamlOptions = networkQueries.yaml('ingressclasses', scope, name, true)
  const configurationQuery = useQuery({
    ...yamlOptions,
    enabled: Boolean(yamlOptions.enabled) && canRead && active === 'overview',
    refetchInterval: refresh.interval,
  })
  const source = canRead ? readableQueryData(configurationQuery) : undefined
  const parameters = useMemo(() => {
    if (!source) return undefined
    try {
      return { data: ingressClassParameters(source.content, name) }
    } catch {
      return {
        error: zh
          ? '无法解析当前 IngressClass 参数配置，请查看 YAML。'
          : 'Cannot parse this IngressClass configuration; inspect YAML.',
      }
    }
  }, [source, name, zh])
  const parameterFacts = parameters?.data
    ? [
        { label: zh ? '参数类型' : 'Parameter kind', children: parameters.data.kind },
        { label: zh ? '参数名称' : 'Parameter name', children: parameters.data.name },
        {
          label: zh ? '参数 API 组' : 'Parameter API group',
          children: parameters.data.apiGroup || 'core',
        },
        { label: zh ? '参数作用域' : 'Parameter scope', children: parameters.data.scope },
        ...(parameters.data.namespace
          ? [
              {
                label: zh ? '参数命名空间' : 'Parameter namespace',
                children: parameters.data.namespace,
              },
            ]
          : []),
      ]
    : []
  const activeQueries = [
    query,
    ...(configurationQuery.isEnabled || active === 'yaml'
      ? [{ ...configurationQuery, isEnabled: true }]
      : []),
  ]
  const canUpdate = permitted('update') && hasAllowedAction(detail?.allowedActions, 'update')
  const canDelete = permitted('delete') && hasAllowedAction(detail?.allowedActions, 'delete')
  const remove = usePlatformMutation(networkMutations.remove('ingressclasses', client), {
    label: ['删除 IngressClass', 'Delete IngressClass'],
    target: platformResourceTarget,
  })
  const actions = useKubernetesResourceActions<IngressClassDetail>({
    resourceKey: 'ingressclass-detail',
    resourceLabel: 'IngressClass',
    records: detail ? [detail] : [],
    getName: (r) => r.name,
    getKey: (r) => r.name,
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
                  ? '删除此 Class 不会删除引用它的 Ingress，也不会卸载控制器。'
                  : 'Deleting this Class does not delete referencing Ingresses or uninstall the controller.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/ingressclasses?${new URLSearchParams({ clusterId: clusterId || '' })}`,
                )
              },
            },
          ]
        : [],
  })
  const feedback = (
    <>
      <ReadFeedback canRead={canRead} query={query} label="IngressClass" />
      {configurationQuery.isEnabled && (
        <ReadFeedback canRead={canRead} query={configurationQuery} label="IngressClass YAML" />
      )}
      {parameters?.error && <Alert type="warning" showIcon title={parameters.error} />}
    </>
  )
  if (!clusterId)
    return (
      <ManagementState kind="select-scope" description={zh ? '请选择集群' : 'Select a cluster'} />
    )
  if (!detail)
    return (
      <>
        {feedback}
        {query.isLoading ? (
          <Card className="soha-detail-card" loading />
        ) : canRead && !query.error ? (
          <ManagementState
            kind="not-found"
            description={zh ? 'IngressClass 未找到' : 'IngressClass not found'}
          />
        ) : null}
      </>
    )
  const config = (
    <WorkloadOverviewConfiguration
      items={[
        {
          label: 'Controller',
          children: (
            <Typography.Text
              className="soha-ingressclass-wrap"
              copyable={Boolean(detail.controller)}
            >
              {detail.controller || '—'}
            </Typography.Text>
          ),
        },
        {
          label: zh ? '默认类' : 'Default class',
          children: detail.isDefault ? (
            <MetadataTag tone="blue" label={zh ? '默认类' : 'Default'} />
          ) : zh ? (
            '否'
          ) : (
            'No'
          ),
        },
        {
          label: zh ? '参数引用' : 'Parameters',
          children: (
            <span className="soha-ingressclass-wrap">
              {detail.parameters || (zh ? '未配置' : 'Not configured')}
            </span>
          ),
        },
        { label: zh ? '作用域' : 'Scope', children: zh ? '集群' : 'Cluster' },
        ...parameterFacts,
      ]}
    />
  )
  const overview = (
    <WorkloadOverview
      aside={
        <Card className="soha-detail-card" title={zh ? '使用说明' : 'Usage'}>
          <Typography.Paragraph>
            {zh
              ? 'Controller 是控制器标识，不代表控制器已安装或运行正常。'
              : 'Controller is an identifier, not evidence of installation or health.'}
          </Typography.Paragraph>
          <Typography.Paragraph>
            {zh
              ? '默认类用于未显式设置 ingressClassName 的新 Ingress；多个默认类可能导致创建被拒绝。'
              : 'A default class applies to new Ingresses without ingressClassName. Multiple defaults may prevent creation.'}
          </Typography.Paragraph>
        </Card>
      }
    >
      <Card
        className="soha-detail-card"
        title={zh ? '类配置' : 'Class configuration'}
        extra={
          <Button type="link" onClick={() => setTab('yaml')}>
            {zh ? '查看 YAML' : 'View YAML'}
          </Button>
        }
      >
        {config}
      </Card>
      <IngressClassIngresses
        items={detail.ingresses || []}
        clusterId={clusterId}
        canRead={canIngresses}
        preview
        onShowAll={() => setTab('ingresses')}
      />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
  return (
    <div className="soha-ingressclass-detail">
      {feedback}
      {actions.confirmation}
      {!actions.isConfirming && remove.feedback}
      <NetworkDetailShell
        key={identity}
        activeTabKey={active}
        onTabChange={setTab}
        clusterScoped
        kind="ingressclasses"
        label="IngressClass"
        detail={detail}
        target={{ scope, name }}
        tabOrder={tabs}
        preserveYaml
        canUpdateYAML={canUpdate}
        overview={overview}
        header={
          <ResourceDetailHeader
            kind="IngressClass"
            name={detail.name}
            ageSeconds={detail.ageSeconds}
            facts={
              detail.isDefault ? (
                <MetadataTag tone="blue" label={zh ? '默认类' : 'Default'} />
              ) : undefined
            }
            backTo="/network/ingressclasses"
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  resourceLabel="IngressClass"
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  beforeRefresh={
                    canUpdate ? (
                      <Button
                        aria-label={zh ? '编辑 IngressClass' : 'Edit IngressClass'}
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
                    aria-label={zh ? '删除 IngressClass' : 'Delete IngressClass'}
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
            key: 'ingresses',
            label: 'Ingresses',
            children: (
              <IngressClassIngresses
                items={detail.ingresses || []}
                clusterId={clusterId}
                canRead={canIngresses}
              />
            ),
          },
          {
            key: 'relationships',
            label: zh ? '关联资源' : 'Related resources',
            children: (
              <WorkloadRelationships
                key={identity}
                kind="IngressClass"
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
