import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useYamlDraft } from '@/components/use-yaml-draft'
import { useAIPageContext } from '@/features/copilot'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import { ResourceGitOpsStatus } from '@/features/platform/shared/resource-gitops-status'
import { PlatformResourceOverview } from '@/features/platform/shared/resource-overview'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { updateWorkloadYAML } from '@/features/platform/workloads/shared/api'
import { useWorkloadDetailScope } from '@/features/platform/workloads/shared/detail-scope'
import { workloadQueries } from '@/features/platform/workloads/shared/queries'
import type { WorkloadKind } from '@/features/platform/workloads/shared/types'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import type { TabsProps } from 'antd'
import { App, Card, Spin, Tabs } from 'antd'
import type { ReactNode } from 'react'
import { lazy, Suspense, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'

const K8sYamlEditor = lazy(async () => {
  const mod = await import('@/components/k8s-yaml-editor')
  return { default: mod.K8sYamlEditor }
})

const ResourceGraphPanel = lazy(async () => {
  const mod = await import('@/features/platform/shared/resource-insights/resource-graph-panel')
  return { default: mod.ResourceGraphPanel }
})

const RESOURCE_GRAPH_KINDS: Partial<Record<WorkloadKind, string>> = {
  deployments: 'Deployment',
  pods: 'Pod',
  replicasets: 'ReplicaSet',
}

const WORKLOAD_ENTITY_KINDS: Record<WorkloadKind, string> = {
  deployments: 'Deployment',
  pods: 'Pod',
  replicasets: 'ReplicaSet',
  replicationcontrollers: 'ReplicationController',
  statefulsets: 'StatefulSet',
  daemonsets: 'DaemonSet',
  jobs: 'Job',
  cronjobs: 'CronJob',
}

/* ─── generic workload detail ─── */

export interface WorkloadMeta {
  name: string
  namespace: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  createdAt: string
  yaml?: string
  [key: string]: unknown
}

export type WorkloadDetailExtraOverview = ReactNode | ((detail: WorkloadMeta) => ReactNode)

export type WorkloadDetailExtraTabPanes =
  | NonNullable<TabsProps['items']>
  | ((detail: WorkloadMeta) => NonNullable<TabsProps['items']>)

export function WorkloadDetailShell({
  title,
  resource,
  paramKey,
  extraTabPanes,
  extraOverview,
  activeTabKey,
  onTabChange,
  yamlLast = false,
  header,
  hideMetadataOverview = false,
  hideRelationshipsTab = false,
  detailRefetchOnMount = true,
}: {
  title: string
  resource: WorkloadKind
  paramKey: string
  extraTabPanes?: WorkloadDetailExtraTabPanes
  extraOverview?: WorkloadDetailExtraOverview
  activeTabKey?: string
  onTabChange?: (activeKey: string) => void
  yamlLast?: boolean
  header?: ReactNode
  hideMetadataOverview?: boolean
  hideRelationshipsTab?: boolean
  detailRefetchOnMount?: boolean
}) {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const params = useParams()
  const name = params[paramKey] as string
  const { clusterId, namespace: detailNamespace } = useWorkloadDetailScope()
  const [internalActiveTabKey, setInternalActiveTabKey] = useState('overview')
  const resolvedActiveTabKey = activeTabKey ?? internalActiveTabKey

  const detailScope = { clusterId, namespace: detailNamespace }
  const detailQuery = useQuery({
    ...workloadQueries.detail<WorkloadMeta>(resource, detailScope, name),
    refetchOnMount: detailRefetchOnMount,
  })
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const yamlQueryOptions = workloadQueries.yaml(resource, detailScope, name)
  const yamlQuery = useQuery({
    ...yamlQueryOptions,
    enabled: Boolean(yamlQueryOptions.enabled) && resolvedActiveTabKey === 'yaml',
  })
  const visibleYamlQueryData =
    Boolean(yamlQueryOptions.enabled) && resolvedActiveTabKey === 'yaml'
      ? readableQueryData(yamlQuery)
      : undefined
  const yamlDraftStorageKey = useMemo(
    () =>
      clusterId
        ? `kc:yaml-draft:${clusterId}:${resource}:${detailNamespace || 'default'}:${name}`
        : '',
    [clusterId, detailNamespace, name, resource],
  )
  const yamlState = useYamlDraft(
    yamlDraftStorageKey,
    visibleYamlQueryData?.content,
    yamlDraftStorageKey,
  )
  const { value: yamlDraft, setValue: setYamlDraft } = yamlState
  const yamlApplyCapability = useClusterCapability('resource.yaml.apply', localeCode)
  const yamlApplyDisabledReason = yamlApplyCapability.disabled
    ? yamlApplyCapability.reason
    : undefined
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: title,
    entityKind: WORKLOAD_ENTITY_KINDS[resource],
    entityName: name,
    clusterId: clusterId || undefined,
    namespace: detailNamespace || undefined,
    workload: name,
  })

  const applyYamlMutation = usePlatformMutation(
    {
      mutationFn: () =>
        updateWorkloadYAML(resource, detailScope, name, {
          content: yamlDraft,
        }),
      onSuccess: (resourceYAML) => {
        yamlState.applied(resourceYAML.content)

        yamlQuery.refetch()
        detailQuery.refetch()
      },
    },
    {
      label: ['应用 YAML', 'Apply YAML'],
      target: () => platformResourceTarget({ scope: detailScope, name }),
    },
  )
  const platformWriteBlocked = applyYamlMutation.blocked

  const detail = visibleDetailQueryData

  if (detailQuery.isLoading)
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  if (detailQuery.isError && !detail)
    return <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
  if (!detail)
    return (
      <ManagementState
        compact
        kind="not-found"
        title={localeCode === 'zh_CN' ? `${title}未找到` : `${title} not found`}
      />
    )

  const resolvedExtraOverview =
    typeof extraOverview === 'function' ? extraOverview(detail) : extraOverview
  const resolvedExtraTabPanes =
    typeof extraTabPanes === 'function' ? extraTabPanes(detail) : (extraTabPanes ?? [])
  const graphKind = RESOURCE_GRAPH_KINDS[resource]

  return (
    <div className="soha-page soha-workload-detail-page">
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
      <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />

      {applyYamlMutation.feedback}

      {header}

      <Tabs
        activeKey={resolvedActiveTabKey}
        className="soha-resource-tabs soha-workload-detail-tabs"
        onChange={(nextActiveKey) => {
          setInternalActiveTabKey(nextActiveKey)
          onTabChange?.(nextActiveKey)
        }}
        indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
        size="small"
        tabBarGutter={32}
        items={[
          {
            key: 'overview',
            label: t('common.overview', 'Overview'),
            children: (
              <div className="soha-detail-stack">
                {!hideMetadataOverview && (
                  <PlatformResourceOverview
                    annotations={detail.annotations}
                    createdAt={detail.createdAt}
                    labels={detail.labels}
                    name={detail.name}
                    namespace={detail.namespace}
                  />
                )}
                <ResourceGitOpsStatus
                  clusterId={clusterId}
                  kind={title}
                  name={detail.name}
                  namespace={detail.namespace}
                />
                {resolvedExtraOverview}
              </div>
            ),
          },
          ...(graphKind && !hideRelationshipsTab
            ? [
                {
                  key: 'relationships',
                  label: localeCode === 'zh_CN' ? '关系' : 'Relationships',
                  children: (
                    <Suspense fallback={<Spin size="large" />}>
                      <ResourceGraphPanel kind={graphKind} name={detail.name} scope={detailScope} />
                    </Suspense>
                  ),
                },
              ]
            : []),
          ...(yamlLast ? resolvedExtraTabPanes : []),
          {
            key: 'yaml',
            label: t('common.yaml', 'YAML'),
            children: (
              <Suspense
                fallback={
                  <Card className="soha-detail-card">
                    <Spin size="large" />
                  </Card>
                }
              >
                {yamlQuery.isError && visibleYamlQueryData === undefined ? null : (
                  <K8sYamlEditor
                    key={yamlState.resourceKey}
                    value={yamlDraft}
                    original={yamlState.original}
                    serverChanged={yamlState.serverChanged}
                    onCompareLatest={yamlState.compareLatest}
                    loading={yamlQuery.isPending}
                    error={yamlQuery.error}
                    onChange={setYamlDraft}
                    onReset={() => {
                      yamlState.reset()
                      void message.success(t('yamlEditor.resetSuccess', 'YAML draft reset'))
                    }}
                    onSave={() => {
                      if (!yamlDraftStorageKey) return
                      window.localStorage.setItem(yamlDraftStorageKey, yamlDraft)
                      void message.success(t('yamlEditor.saveSuccess', 'YAML draft saved locally'))
                    }}
                    onApply={() => applyYamlMutation.mutate()}
                    saveDisabled={!yamlDraftStorageKey}
                    applyDisabled={
                      platformWriteBlocked ||
                      !yamlQueryOptions.enabled ||
                      !yamlDraft.trim() ||
                      yamlApplyCapability.disabled
                    }
                    applyDisabledReason={yamlApplyDisabledReason}
                    applying={applyYamlMutation.isPending}
                  />
                )}
              </Suspense>
            ),
          },
          ...(yamlLast ? [] : resolvedExtraTabPanes),
        ]}
      />
    </div>
  )
}
