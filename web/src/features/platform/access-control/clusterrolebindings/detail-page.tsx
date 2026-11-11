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
import type { ClusterRoleBindingDetail } from './types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Spin } from 'antd'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { bindingConfiguration } from './detail-model'
import { accessControlMutations } from '../shared/mutations'
import { accessControlQueries } from '../shared/queries'
import { ClusterRoleBindingOverview } from './overview'
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
const permission = 'platform.access-control.cluster-role-bindings'

export function PlatformAccessControlClusterRoleBindingDetailPage() {
  const { name = '' } = useParams()
  const { clusterId } = useWorkloadDetailScope()
  return (
    <ClusterRoleBindingDetailContent
      key={JSON.stringify([clusterId, name])}
      clusterId={clusterId || ''}
      name={name}
    />
  )
}
function ClusterRoleBindingDetailContent({ clusterId, name }: { clusterId: string; name: string }) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const navigate = useNavigate()
  const targetIdentity = JSON.stringify([clusterId, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const client = useQueryClient()
  const [tab, setTab] = useResourceDetailTab()
  const active = ['overview', 'relationships', 'yaml'].includes(tab) ? tab : 'overview'
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
    sourceTitle: 'ClusterRoleBinding',
    entityKind: 'ClusterRoleBinding',
    entityName: name,
    clusterId: clusterId || undefined,
  })
  const scope = { clusterId, namespace: null }
  const target = { scope, name }
  const options = accessControlQueries.detail<ClusterRoleBindingDetail>(
    'clusterrolebindings',
    scope,
    name,
  )
  const detailQuery = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const yamlOptions = accessControlQueries.yaml('clusterrolebindings', scope, name)
  const showYamlConfiguration = active !== 'relationships'
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled: Boolean(yamlOptions.enabled) && canRead && showYamlConfiguration,
    refetchInterval: showYamlConfiguration ? refresh.interval : false,
  })
  const candidate = canRead ? readableQueryData(detailQuery) : undefined
  const identityError = Boolean(candidate && candidate.name !== name)
  const detail = !identityError ? candidate : undefined
  const yaml = canRead ? readableQueryData(yamlQuery) : undefined
  const parsed = useMemo(() => {
    if (!yaml) return {}
    try {
      return { configuration: bindingConfiguration(yaml.content, name) }
    } catch {
      return { error: true }
    }
  }, [yaml, name])
  const yamlState = useYamlDraft(
    JSON.stringify([clusterId, name]),
    parsed.error ? undefined : yaml?.content,
  )
  const update = usePlatformMutation(
    accessControlMutations.updateYAML('clusterrolebindings', client),
    {
      label: ['应用 ClusterRoleBinding YAML', 'Apply ClusterRoleBinding YAML'],
      target: platformResourceTarget,
    },
  )
  const remove = usePlatformMutation(accessControlMutations.remove('clusterrolebindings', client), {
    label: ['删除 ClusterRoleBinding', 'Delete ClusterRoleBinding'],
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
    '/platform-access-control/clusterrolebindings',
    '?' + new URLSearchParams({ clusterId }),
    null,
  )
  const actions = useKubernetesResourceActions<ClusterRoleBindingDetail>({
    resourceKey: 'clusterrolebinding-detail',
    resourceLabel: 'ClusterRoleBinding',
    scopeKey: JSON.stringify([clusterId, name]),
    records: detail ? [detail] : [],
    getName: (record) => record.name,
    getKey: (record) => record.name,
    actions: () =>
      canDelete
        ? [
            {
              key: 'delete',
              label: zh ? '删除' : 'Delete',
              danger: true,
              disabled: blocked,
              confirm: kubernetesDeleteConfirmation(
                { name },
                localeCode,
                zh
                  ? '删除此集群角色绑定会撤销它授予主体的权限；角色和主体资源不会自动删除。'
                  : 'Deleting this cluster role binding revokes the access it grants; the role and subject resources are not deleted.',
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
      bindingConfiguration(yamlState.value, name)
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
  if (!clusterId)
    return (
      <ManagementState kind="select-scope" description={zh ? '请选择集群' : 'Select a cluster'} />
    )
  if (permissionsQuery.isPending || (canRead && detailQuery.isPending))
    return <ManagementState kind="loading" />
  if (!canRead || (detailQuery.isError && !detail))
    return <ReadFeedback canRead={canRead} query={detailQuery} label="ClusterRoleBinding" />
  if (identityError)
    return (
      <ManagementState
        kind="error"
        title={
          zh ? 'ClusterRoleBinding 详情身份不匹配' : 'ClusterRoleBinding detail identity mismatch'
        }
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
        title={zh ? 'ClusterRoleBinding 未找到' : 'ClusterRoleBinding not found'}
      />
    )
  return (
    <div className="soha-clusterrolebinding-detail">
      {actions.confirmation}
      <div className="soha-page soha-workload-detail-page soha-k8s-detail-page">
        <ResourceDetailHeader
          kind="ClusterRoleBinding"
          name={name}
          createdAt={detail.createdAt}
          ageSeconds={detail.ageSeconds}
          backTo="/platform-access-control/clusterrolebindings"
          actions={
            <>
              <WorkloadRefresh
                settings={refresh}
                queries={[detailQuery, yamlQuery]}
                resourceLabel="ClusterRoleBinding"
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
        <ReadFeedback canRead={canRead} query={detailQuery} label="ClusterRoleBinding" />
        {showYamlConfiguration ? (
          <ReadFeedback
            canRead={canRead}
            query={yamlQuery}
            label={zh ? 'ClusterRoleBinding 补充配置' : 'ClusterRoleBinding configuration'}
          />
        ) : null}
        {showYamlConfiguration && parsed.error ? (
          <ManagementState
            kind="error"
            title={
              zh
                ? 'ClusterRoleBinding 补充配置无法解析'
                : 'ClusterRoleBinding configuration could not be parsed'
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
                <ClusterRoleBindingOverview
                  detail={detail}
                  configuration={parsed.configuration}
                  clusterId={clusterId}
                />
              ),
            },
            {
              key: 'relationships',
              label: zh ? '关系' : 'Relationships',
              children:
                active === 'relationships' ? (
                  <Suspense fallback={<ManagementState kind="loading" />}>
                    <AccessControlRelationships
                      kind="ClusterRoleBinding"
                      name={name}
                      scope={scope}
                    />
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
