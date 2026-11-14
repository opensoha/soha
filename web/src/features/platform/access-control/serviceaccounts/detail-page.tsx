import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useAIPageContext } from '@/features/copilot'
import { useYamlDraft } from '@/components/use-yaml-draft'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  ResourceDetailHeader,
  ResourceDetailTabs,
  useResourceDetailTab,
} from '@/features/platform/shared/resource-detail-header'
import { resourceCollectionPath } from '@/features/platform/shared/resource-collection-path'
import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { ServiceAccountDetail } from './types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Spin } from 'antd'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { saConfiguration } from './detail-model'
import { accessControlMutations } from '../shared/mutations'
import { accessControlQueries } from '../shared/queries'
import { SaOverview } from './overview'
import { SaBindings } from './bindings'
import { ServiceAccountEffectiveAccessPanel } from './effective-access-panel'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import './styles.css'

const AccessControlRelationships = lazy(async () => {
  const module = await import('@/features/platform/shared/resource-insights/workload-relationships')
  return { default: module.WorkloadRelationships }
})
const YAMLEditor = lazy(async () => {
  const module = await import('@/components/k8s-yaml-editor')
  return { default: module.K8sYamlEditor }
})
const permission = 'platform.access-control.service-accounts'

export function PlatformAccessControlServiceAccountDetailPage() {
  const { name = '' } = useParams()
  const { clusterId, namespace } = useWorkloadDetailScope()
  return (
    <SaDetailContent
      key={JSON.stringify([clusterId, namespace, name])}
      clusterId={clusterId || ''}
      namespace={namespace || ''}
      name={name}
    />
  )
}
function SaDetailContent({
  clusterId,
  namespace,
  name,
}: {
  clusterId: string
  namespace: string
  name: string
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const navigate = useNavigate()
  const targetIdentity = JSON.stringify([clusterId, namespace, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const client = useQueryClient()
  const { namespace: listNamespace } = usePlatformScopeStore()
  const [tab, setTab] = useResourceDetailTab()
  const active = ['overview', 'bindings', 'effective-access', 'relationships', 'yaml'].includes(tab)
    ? tab
    : 'overview'
  const [visitedYaml, setVisitedYaml] = useState(active === 'yaml')
  const [draftError, setDraftError] = useState<string | null>(null)
  useEffect(() => {
    if (active !== tab) setTab('overview')
    if (active === 'yaml') setVisitedYaml(true)
  }, [active, tab, setTab])
  const refresh = useWorkloadRefresh()
  const capability = useClusterCapability('resource.yaml.apply', localeCode)
  const permissionsQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionsQuery)?.data
  const canRead = hasPermission(snapshot, `${permission}.view`)
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: 'ServiceAccount',
    entityKind: 'ServiceAccount',
    entityName: name,
    clusterId: clusterId || undefined,
    namespace: namespace || undefined,
  })
  const scope = { clusterId, namespace }
  const target = { scope, name }
  const options = accessControlQueries.detail<ServiceAccountDetail>('serviceaccounts', scope, name)
  const detailQuery = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const yamlOptions = accessControlQueries.yaml('serviceaccounts', scope, name)
  const showYamlConfiguration = !['bindings', 'effective-access', 'relationships'].includes(active)
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled: Boolean(yamlOptions.enabled) && canRead && showYamlConfiguration,
    refetchInterval: showYamlConfiguration ? refresh.interval : false,
  })
  const candidate = canRead ? readableQueryData(detailQuery) : undefined
  const identityError = Boolean(
    candidate && (candidate.name !== name || candidate.namespace !== namespace),
  )
  const detail = !identityError ? candidate : undefined
  const yaml = canRead ? readableQueryData(yamlQuery) : undefined
  const parsed = useMemo(() => {
    if (!yaml) return {}
    try {
      return { configuration: saConfiguration(yaml.content, name, namespace) }
    } catch {
      return { error: true }
    }
  }, [yaml, name, namespace])
  const yamlState = useYamlDraft(
    JSON.stringify([clusterId, namespace, name]),
    parsed.error ? undefined : yaml?.content,
  )
  const update = usePlatformMutation(accessControlMutations.updateYAML('serviceaccounts', client), {
    label: ['应用 ServiceAccount YAML', 'Apply ServiceAccount YAML'],
    target: platformResourceTarget,
  })
  const remove = usePlatformMutation(accessControlMutations.remove('serviceaccounts', client), {
    label: ['删除 ServiceAccount', 'Delete ServiceAccount'],
    target: platformResourceTarget,
  })
  const canUpdate = Boolean(
    detail &&
    !detailQuery.error &&
    hasPermission(snapshot, `${permission}.update`) &&
    hasAllowedAction(detail.allowedActions, 'update'),
  )
  const canDelete = Boolean(
    detail &&
    !detailQuery.error &&
    hasPermission(snapshot, `${permission}.delete`) &&
    hasAllowedAction(detail.allowedActions, 'delete'),
  )
  const blocked =
    update.blocked ||
    remove.blocked ||
    update.isPending ||
    remove.isPending ||
    Boolean(detailQuery.error)
  const applyBlocked =
    blocked ||
    capability.isLoading ||
    capability.disabled ||
    Boolean(yamlQuery.error) ||
    Boolean(parsed.error)
  const collection = resourceCollectionPath(
    '/platform-access-control/serviceaccounts',
    '?' + new URLSearchParams({ clusterId }),
    listNamespace,
  )
  const actions = useKubernetesResourceActions<ServiceAccountDetail>({
    resourceKey: 'sa-detail',
    resourceLabel: 'ServiceAccount',
    scopeKey: JSON.stringify([clusterId, namespace, name]),
    records: detail ? [detail] : [],
    getName: (record) => record.name,
    getKey: (record) => `${record.namespace}/${record.name}`,
    actions: () =>
      canDelete
        ? [
            {
              key: 'delete',
              label: zh ? '删除' : 'Delete',
              danger: true,
              disabled: blocked,
              confirm: kubernetesDeleteConfirmation(
                { name, namespace },
                localeCode,
                zh
                  ? '删除此账户可能影响引用它的工作负载；不会自动删除引用它的角色绑定。'
                  : 'Deleting this account may affect workloads; referencing bindings are not automatically deleted.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync(target)
                if (isCurrentTarget()) navigate(collection)
              },
            },
          ]
        : [],
  })
  const apply = async () => {
    if (!canRead || !canUpdate || applyBlocked || !yaml || !yamlState.value.trim()) return
    try {
      saConfiguration(yamlState.value, name, namespace)
    } catch {
      setDraftError(
        zh
          ? 'YAML 无法解析或资源身份不匹配，未提交。'
          : 'Invalid YAML or resource identity mismatch; nothing was submitted.',
      )
      return
    }
    setDraftError(null)
    const result = await update.mutateAsync({ ...target, content: yamlState.value })
    yamlState.applied(result.content ?? yamlState.value)
  }
  if (!clusterId || !namespace)
    return (
      <ManagementState
        kind="select-scope"
        description={zh ? '请选择集群和命名空间' : 'Select a cluster and namespace'}
      />
    )
  if (permissionsQuery.isPending || (canRead && detailQuery.isPending))
    return <ManagementState kind="loading" />
  if (!canRead || (detailQuery.isError && !detail))
    return <ReadFeedback canRead={canRead} query={detailQuery} label="ServiceAccount" />
  if (identityError)
    return (
      <ManagementState
        kind="error"
        title={zh ? 'ServiceAccount 详情身份不匹配' : 'ServiceAccount detail identity mismatch'}
        description={
          zh
            ? '未展示其他资源的数据，请重新读取。'
            : 'The response belongs to another resource. Read it again.'
        }
        actions={
          <Button onClick={() => void detailQuery.refetch()}>{zh ? '重试' : 'Retry'}</Button>
        }
      />
    )
  if (!detail)
    return (
      <ManagementState
        kind="not-found"
        title={zh ? 'ServiceAccount 未找到' : 'ServiceAccount not found'}
      />
    )
  return (
    <div className="soha-sa-detail">
      {actions.confirmation}
      <div className="soha-page soha-workload-detail-page soha-k8s-detail-page">
        <ResourceDetailHeader
          kind="ServiceAccount"
          name={name}
          namespace={namespace}
          createdAt={detail.createdAt}
          ageSeconds={detail.ageSeconds}
          backTo="/platform-access-control/serviceaccounts"
          actions={
            <>
              <WorkloadRefresh
                settings={refresh}
                queries={[detailQuery, yamlQuery]}
                resourceLabel="ServiceAccount"
                onRefresh={() => client.invalidateQueries({ queryKey: options.queryKey })}
                beforeRefresh={
                  canUpdate ? (
                    <Button disabled={blocked} onClick={() => setTab('yaml')}>
                      {zh ? '编辑 YAML' : 'Edit YAML'}
                    </Button>
                  ) : undefined
                }
              />
              {canDelete ? (
                <Button
                  danger
                  disabled={blocked}
                  onClick={() => actions.selectAction(detail, 'delete')}
                >
                  {zh ? '删除' : 'Delete'}
                </Button>
              ) : null}
            </>
          }
        />
        <ReadFeedback canRead={canRead} query={detailQuery} label="ServiceAccount" />
        {showYamlConfiguration ? (
          <ReadFeedback
            canRead={canRead}
            query={yamlQuery}
            label={zh ? 'ServiceAccount 补充配置' : 'ServiceAccount configuration'}
          />
        ) : null}
        {showYamlConfiguration && parsed.error ? (
          <ManagementState
            kind="error"
            title={
              zh
                ? 'ServiceAccount 补充配置无法解析'
                : 'ServiceAccount configuration could not be parsed'
            }
            description={
              zh
                ? '资源身份或配置字段无法确认，已保留可读详情并暂停应用。'
                : 'Identity or fields could not be confirmed. Readable detail remains; apply is disabled.'
            }
            actions={
              <Button onClick={() => void yamlQuery.refetch()}>{zh ? '重试' : 'Retry'}</Button>
            }
          />
        ) : null}
        {!actions.isConfirming && remove.feedback}
        {update.feedback}
        {draftError ? <ManagementState kind="error" title={draftError} /> : null}
        <ResourceDetailTabs
          activeKey={active}
          onChange={setTab}
          items={[
            {
              key: 'overview',
              label: zh ? '概览' : 'Overview',
              children: (
                <SaOverview
                  detail={detail}
                  clusterId={clusterId}
                  configuration={parsed.configuration}
                  onTab={setTab}
                />
              ),
            },
            {
              key: 'bindings',
              label: zh ? '绑定' : 'Bindings',
              children:
                active === 'bindings' ? (
                  <SaBindings name={name} namespace={namespace} clusterId={clusterId} />
                ) : null,
            },
            {
              key: 'effective-access',
              label: zh ? '有效权限' : 'Effective access',
              children:
                active === 'effective-access' ? (
                  <ServiceAccountEffectiveAccessPanel
                    name={name}
                    namespace={namespace}
                    clusterId={clusterId}
                  />
                ) : null,
            },
            {
              key: 'relationships',
              label: zh ? '关系' : 'Relationships',
              children:
                active === 'relationships' ? (
                  <Suspense fallback={<ManagementState kind="loading" />}>
                    <AccessControlRelationships kind="ServiceAccount" name={name} scope={scope} />
                  </Suspense>
                ) : null,
            },
            {
              key: 'yaml',
              label: 'YAML',
              children:
                (active === 'yaml' || visitedYaml) && yaml && !parsed.error ? (
                  <Suspense
                    fallback={
                      <Card loading>
                        <Spin />
                      </Card>
                    }
                  >
                    <YAMLEditor
                      readOnly={!canUpdate}
                      applyDisabled={applyBlocked || !yamlState.value.trim()}
                      onSave={() => undefined}
                      saveDisabled
                      error={yamlQuery.error}
                      original={yamlState.original}
                      serverChanged={yamlState.serverChanged}
                      onCompareLatest={yamlState.compareLatest}
                      applying={update.isPending}
                      value={yamlState.value}
                      onChange={(value) => {
                        setDraftError(null)
                        yamlState.setValue(value)
                      }}
                      onReset={() => {
                        setDraftError(null)
                        yamlState.reset()
                      }}
                      onApply={() => void apply().catch(() => undefined)}
                    />
                  </Suspense>
                ) : active === 'yaml' && yamlQuery.isPending ? (
                  <ManagementState kind="loading" />
                ) : null,
            },
          ]}
        />
      </div>
    </div>
  )
}
