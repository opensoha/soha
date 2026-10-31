import {
  ResourceDetailHeader,
  ResourceDetailTabs,
} from '@/features/platform/shared/resource-detail-header'
import { ReadFeedback } from '@/components/request-feedback'
import { useYamlDraft } from '@/components/use-yaml-draft'
import type { AIPageContext } from '@/features/copilot'
import { useAIPageContext } from '@/features/copilot'
import { ResourceGitOpsStatus } from '@/features/platform/shared/resource-gitops-status'
import { PlatformResourceOverview } from '@/features/platform/shared/resource-overview'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TabsProps } from 'antd'
import { App, Card, Spin } from 'antd'
import type { ReactNode } from 'react'
import { lazy, Suspense } from 'react'
import '../styles.css'
import { networkMutations } from './mutations'
import { networkQueries } from './queries'
import type { NetworkKind, NetworkResourceRecord, NetworkTarget } from './types'

const K8sYamlEditor = lazy(async () => {
  const module = await import('@/components/k8s-yaml-editor')
  return { default: module.K8sYamlEditor }
})

const ResourceGraphPanel = lazy(async () => {
  const module = await import('@/features/platform/shared/resource-insights/resource-graph-panel')
  return { default: module.ResourceGraphPanel }
})

const RESOURCE_GRAPH_KINDS: Partial<Record<NetworkKind, string>> = {
  ingresses: 'Ingress',
  services: 'Service',
}

const NETWORK_ENTITY_KINDS: Record<NetworkKind, string> = {
  services: 'kubernetes.service',
  ingresses: 'kubernetes.ingress',
  gatewayclasses: 'kubernetes.gatewayclass',
  gateways: 'kubernetes.gateway',
  httproutes: 'kubernetes.httproute',
  backendtlspolicies: 'kubernetes.backendtlspolicy',
  grpcroutes: 'kubernetes.grpcroute',
  referencegrants: 'kubernetes.referencegrant',
  endpointslices: 'kubernetes.endpointslice',
  ingressclasses: 'kubernetes.ingressclass',
  networkpolicies: 'kubernetes.networkpolicy',
}

export function NetworkResourceOverview({
  ageLabel,
  detail,
  extra,
}: {
  ageLabel?: ReactNode
  detail: NetworkResourceRecord
  extra?: Array<{ key: string; value: ReactNode }>
}) {
  return (
    <PlatformResourceOverview
      compact
      ageSeconds={detail.ageSeconds}
      annotations={detail.annotations}
      createdAtLabel={ageLabel}
      facts={(extra ?? []).map((item) => ({ ...item, label: item.key }))}
      labels={detail.labels}
      name={detail.name}
      namespace={detail.namespace || '-'}
    />
  )
}

function NetworkYAMLTab({
  clusterScoped,
  kind,
  target,
}: {
  clusterScoped: boolean
  kind: NetworkKind
  target: NetworkTarget
}) {
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const yamlQuery = useQuery(networkQueries.yaml(kind, target.scope, target.name, clusterScoped))
  const visibleYamlQueryData = readableQueryData(yamlQuery)
  const updateMutation = usePlatformMutation(networkMutations.updateYAML(kind, queryClient), {
    label: ['更新资源', 'Update resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = updateMutation.blocked
  const yamlState = useYamlDraft(
    JSON.stringify([kind, target.scope.clusterId, target.scope.namespace, target.name]),
    visibleYamlQueryData?.content,
  )
  const { value: draft, setValue: setDraft } = yamlState

  if (yamlQuery.isLoading) {
    return (
      <Card className="soha-detail-card">
        <Spin size="large" />
      </Card>
    )
  }

  return (
    <Suspense
      fallback={
        <Card className="soha-detail-card">
          <Spin size="large" />
        </Card>
      }
    >
      <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />

      {updateMutation.feedback}

      <div style={{ height: 620 }}>
        {yamlQuery.isError && visibleYamlQueryData === undefined ? null : (
          <K8sYamlEditor
            error={yamlQuery.error}
            original={yamlState.original}
            serverChanged={yamlState.serverChanged}
            onCompareLatest={yamlState.compareLatest}
            value={draft}
            onChange={setDraft}
            onReset={yamlState.reset}
            onSave={() =>
              void message.info(
                localeCode === 'zh_CN' ? '暂不支持本地草稿' : 'Local draft save disabled here',
              )
            }
            onApply={() =>
              updateMutation.mutate(
                { ...target, content: draft },
                {
                  onSuccess: (yaml) => {
                    yamlState.applied(yaml.content ?? draft)
                  },
                },
              )
            }
            saveDisabled
            applyDisabled={platformWriteBlocked || !draft.trim() || updateMutation.isPending}
            applying={updateMutation.isPending}
          />
        )}
      </div>
    </Suspense>
  )
}

export function NetworkDetailShell({
  activeTabKey,
  ageLabel,
  aiContext,
  clusterScoped = false,
  detail,
  extraTabs = [],
  kind,
  label,
  onTabChange,
  overviewExtra,
  overviewContent,
  target,
}: {
  activeTabKey: string
  ageLabel?: ReactNode
  aiContext?: Partial<AIPageContext>
  clusterScoped?: boolean
  detail: NetworkResourceRecord
  extraTabs?: NonNullable<TabsProps['items']>
  kind: NetworkKind
  label: string
  onTabChange: (key: string) => void
  overviewExtra?: Array<{ key: string; value: ReactNode }>
  overviewContent?: ReactNode
  target: NetworkTarget
}) {
  const { t } = useI18n()
  const graphKind = RESOURCE_GRAPH_KINDS[kind]
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: `${label} ${detail.name}`,
    entityKind: NETWORK_ENTITY_KINDS[kind],
    entityName: detail.name,
    clusterId: target.scope.clusterId || undefined,
    namespace: detail.namespace || undefined,
    service: kind === 'services' ? detail.name : undefined,
    ...aiContext,
  })
  const items: NonNullable<TabsProps['items']> = [
    {
      key: 'overview',
      label: t('common.overview', 'Overview'),
      children: (
        <div className="soha-detail-stack">
          <NetworkResourceOverview ageLabel={ageLabel} detail={detail} extra={overviewExtra} />
          <ResourceGitOpsStatus
            clusterId={target.scope.clusterId}
            kind={label}
            name={detail.name}
            namespace={detail.namespace}
          />
          {overviewContent}
        </div>
      ),
    },
    ...(graphKind
      ? [
          {
            key: 'relationships',
            label: t('common.relatedResources', '关联资源'),
            children: (
              <Suspense fallback={<Spin size="large" />}>
                <ResourceGraphPanel kind={graphKind} name={detail.name} scope={target.scope} />
              </Suspense>
            ),
          },
        ]
      : []),
    ...extraTabs,
    {
      key: 'yaml',
      label: t('common.yaml', 'YAML'),
      children:
        activeTabKey === 'yaml' ? (
          <NetworkYAMLTab clusterScoped={clusterScoped} kind={kind} target={target} />
        ) : null,
    },
  ]

  return (
    <div className="soha-page soha-workload-detail-page soha-k8s-detail-page">
      <ResourceDetailHeader
        kind={label}
        name={detail.name}
        namespace={detail.namespace}
        ageSeconds={detail.ageSeconds}
        queryKey={networkQueries.detail(kind, target.scope, target.name, clusterScoped).queryKey}
      />
      <ResourceDetailTabs
        activeKey={activeTabKey}
        className="soha-resource-tabs soha-workload-detail-tabs"
        indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
        items={items}
        onChange={onTabChange}
        size="small"
        tabBarGutter={32}
      />
    </div>
  )
}
