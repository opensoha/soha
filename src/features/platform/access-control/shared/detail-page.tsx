import {
  ResourceDetailHeader,
  ResourceDetailTabs,
  useResourceDetailTab,
} from '@/features/platform/shared/resource-detail-header'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useYamlDraft } from '@/components/use-yaml-draft'
import { useAIPageContext } from '@/features/copilot'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TabsProps } from 'antd'
import { App, Card, Spin, Typography } from 'antd'
import type { ReactNode } from 'react'
import { lazy, Suspense } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import '../styles.css'
import { accessControlMutations } from './mutations'
import { accessControlQueries } from './queries'
import {
  accessControlScopeFromSelection,
  accessControlScopeMode,
  resolveAccessControlNamespace,
} from './scope'
import type { AccessControlDetailBase, AccessControlKind, AccessControlTarget } from './types'

const ACCESS_CONTROL_ENTITY_KINDS: Record<AccessControlKind, string> = {
  serviceaccounts: 'ServiceAccount',
  roles: 'Role',
  rolebindings: 'RoleBinding',
  clusterroles: 'ClusterRole',
  clusterrolebindings: 'ClusterRoleBinding',
}

const { Paragraph, Text } = Typography

const K8sYamlEditor = lazy(async () => {
  const module = await import('@/components/k8s-yaml-editor')
  return { default: module.K8sYamlEditor }
})

function AccessControlYAMLTab({
  kind,
  target,
}: {
  kind: AccessControlKind
  target: AccessControlTarget
}) {
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const yamlQuery = useQuery(accessControlQueries.yaml(kind, target.scope, target.name))
  const visibleYamlQueryData = readableQueryData(yamlQuery)
  const updateMutation = usePlatformMutation(accessControlMutations.updateYAML(kind, queryClient), {
    label: ['更新资源', 'Update resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = updateMutation.blocked
  const yamlState = useYamlDraft(
    JSON.stringify([kind, target.scope.clusterId, target.scope.namespace, target.name]),
    visibleYamlQueryData?.content,
  )
  const { value: draft, setValue: setDraft } = yamlState

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

      {yamlQuery.isError && visibleYamlQueryData === undefined ? null : (
        <K8sYamlEditor
          key={yamlState.resourceKey}
          value={draft}
          original={yamlState.original}
          serverChanged={yamlState.serverChanged}
          onCompareLatest={yamlState.compareLatest}
          loading={yamlQuery.isPending}
          error={yamlQuery.error}
          onApply={() =>
            updateMutation.mutate(
              { ...target, content: draft },
              {
                onSuccess: (yaml) => {
                  yamlState.applied(yaml.content)
                },
              },
            )
          }
          onChange={setDraft}
          onReset={yamlState.reset}
          onSave={() =>
            void message.info(
              localeCode === 'zh_CN' ? '暂不支持本地草稿' : 'Local draft save disabled here',
            )
          }
          applyDisabled={platformWriteBlocked || !draft.trim() || updateMutation.isPending}
          applying={updateMutation.isPending}
          saveDisabled
        />
      )}
    </Suspense>
  )
}

export function renderAccessControlRuleSummaries(values: string[] | undefined, emptyLabel: string) {
  if (!values?.length) return <Text type="secondary">{emptyLabel}</Text>
  return (
    <div className="soha-access-control-rule-list">
      {values.map((value) => (
        <div key={value} className="soha-access-control-rule-item">
          <Paragraph style={{ margin: 0 }}>{value}</Paragraph>
        </div>
      ))}
    </div>
  )
}

export function AccessControlResourceDetailPage<TDetail extends AccessControlDetailBase>({
  kind,
  label,
  renderOverview,
  renderEffectiveAccess,
  renderRelationships,
}: {
  kind: AccessControlKind
  label: string
  renderOverview: (detail: TDetail) => ReactNode
  renderEffectiveAccess?: (detail: TDetail) => ReactNode
  renderRelationships?: (detail: TDetail) => ReactNode
}) {
  const { localeCode } = useI18n()
  const { name = '' } = useParams()
  const [searchParams] = useSearchParams()
  const { clusterId, namespace } = usePlatformScopeStore()
  const scopeMode = accessControlScopeMode(kind)
  const detailNamespace =
    scopeMode === 'namespace'
      ? resolveAccessControlNamespace(namespace, searchParams.get('namespace'))
      : null
  const scope = accessControlScopeFromSelection(kind, clusterId, detailNamespace)
  const target = { scope, name }
  const detailQuery = useQuery(accessControlQueries.detail<TDetail>(kind, scope, name))
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const yamlCapability = useClusterCapability('resource.yaml.apply', localeCode)
  const [activeTabKey, setActiveTabKey] = useResourceDetailTab()
  const detail = visibleDetailQueryData
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: label,
    entityKind: ACCESS_CONTROL_ENTITY_KINDS[kind],
    entityName: name,
    clusterId: clusterId || undefined,
    namespace: detailNamespace || undefined,
  })

  if (!clusterId || (scopeMode === 'namespace' && !detailNamespace)) {
    return (
      <div className="soha-page">
        <ManagementState
          description={
            localeCode === 'zh_CN'
              ? '请选择集群和命名空间查看 RBAC 资源。'
              : 'Select a cluster and namespace to inspect this RBAC resource.'
          }
          kind="select-scope"
        />
      </div>
    )
  }
  if (detailQuery.isLoading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Spin size="large" />
      </div>
    )
  }
  if (detailQuery.isError && visibleDetailQueryData === undefined) {
    return <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源记录" />
  }
  if (detailQuery.isError && !detail)
    return <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
  if (!detail) {
    return (
      <div className="soha-page">
        <ManagementState
          description={localeCode === 'zh_CN' ? `${label} 未找到` : `${label} not found`}
          kind="not-found"
        />
      </div>
    )
  }

  const items: NonNullable<TabsProps['items']> = [
    {
      key: 'overview',
      label: localeCode === 'zh_CN' ? '概览' : 'Overview',
      children: renderOverview(detail),
    },
    ...(renderRelationships
      ? [
          {
            key: 'relationships',
            label: localeCode === 'zh_CN' ? '关联资源' : 'Related resources',
            children: activeTabKey === 'relationships' ? renderRelationships(detail) : null,
          },
        ]
      : []),
    ...(renderEffectiveAccess
      ? [
          {
            key: 'effective-access',
            label: localeCode === 'zh_CN' ? '有效权限' : 'Effective Access',
            children: activeTabKey === 'effective-access' ? renderEffectiveAccess(detail) : null,
          },
        ]
      : []),
    {
      key: 'yaml',
      label: 'YAML',
      children:
        activeTabKey !== 'yaml' ? null : yamlCapability.isLoading ? (
          <ManagementState kind="loading" />
        ) : yamlCapability.disabled ? (
          <ManagementState
            description={
              yamlCapability.reason ||
              (localeCode === 'zh_CN'
                ? '当前集群连接模式暂不支持 YAML 查看与应用。'
                : 'The current cluster connection mode does not support YAML view and apply yet.')
            }
            kind="unsupported"
          />
        ) : (
          <AccessControlYAMLTab kind={kind} target={target} />
        ),
    },
  ]

  return (
    <div className="soha-page soha-workload-detail-page soha-k8s-detail-page">
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />

      <ResourceDetailHeader
        kind={label}
        name={detail.name}
        namespace={detail.namespace}
        ageSeconds={detail.ageSeconds}
        queryKey={accessControlQueries.detail(kind, scope, name).queryKey}
      />
      <ResourceDetailTabs
        activeKey={activeTabKey}
        className="soha-resource-tabs soha-workload-detail-tabs"
        indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
        items={items}
        onChange={setActiveTabKey}
        size="small"
        tabBarGutter={32}
      />
    </div>
  )
}
