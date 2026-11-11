import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useYamlDraft } from '@/components/use-yaml-draft'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  ResourceDetailHeader,
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
import type { PersistentVolumeClaimDetail } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Spin } from 'antd'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { StorageDetailShell, StorageDetailTabs } from '../shared/detail-layout'
import { pvcConfiguration } from './detail-model'
import { persistentVolumeClaimMutations } from './mutations'
import { persistentVolumeClaimQueries } from './queries'
import { PvcOverview } from './overview'
import { PvcPodsTab } from './pods-tab'
import './styles.css'

const StorageRelationships = lazy(async () => {
  const module = await import('@/features/platform/shared/resource-insights/workload-relationships')
  return { default: module.WorkloadRelationships }
})
const StorageYAMLPanel = lazy(() => import('../shared/yaml-panel'))
const permission = 'platform.storage.persistent-volume-claims'

export function StoragePvcDetailPage() {
  const { name = '' } = useParams()
  const { clusterId, namespace } = useWorkloadDetailScope()
  return (
    <PvcDetailContent
      key={JSON.stringify([clusterId, namespace, name])}
      clusterId={clusterId || ''}
      namespace={namespace || ''}
      name={name}
    />
  )
}
function PvcDetailContent({
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
  const client = useQueryClient()
  const { namespace: listNamespace } = usePlatformScopeStore()
  const [tab, setTab] = useResourceDetailTab()
  const active = ['overview', 'pods', 'relationships', 'yaml'].includes(tab) ? tab : 'overview'
  const [visitedYaml, setVisitedYaml] = useState(active === 'yaml')
  const [draftError, setDraftError] = useState<string | null>(null)
  useEffect(() => {
    if (active !== tab) setTab('overview')
    if (active === 'yaml') setVisitedYaml(true)
  }, [active, tab, setTab])
  const refresh = useWorkloadRefresh()
  const permissionsQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionsQuery)?.data
  const canRead = hasPermission(snapshot, `${permission}.view`)
  const scope = { clusterId, namespace }
  const target = { scope, name }
  const options = persistentVolumeClaimQueries.detail(scope, name)
  const detailQuery = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const yamlOptions = persistentVolumeClaimQueries.yaml(scope, name)
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled: Boolean(yamlOptions.enabled) && canRead && active !== 'pods',
    refetchInterval: active !== 'pods' ? refresh.interval : false,
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
      return { configuration: pvcConfiguration(yaml.content, name, namespace) }
    } catch {
      return { error: true }
    }
  }, [yaml, name, namespace])
  const yamlState = useYamlDraft(
    JSON.stringify([clusterId, namespace, name]),
    parsed.error ? undefined : yaml?.content,
  )
  const update = usePlatformMutation(persistentVolumeClaimMutations.updateYAML(client), {
    label: ['应用 PVC YAML', 'Apply PVC YAML'],
    target: platformResourceTarget,
  })
  const remove = usePlatformMutation(persistentVolumeClaimMutations.remove(client), {
    label: ['删除 PVC', 'Delete PVC'],
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
  const applyBlocked = blocked || Boolean(yamlQuery.error) || Boolean(parsed.error)
  const collection = resourceCollectionPath(
    '/storage/persistentvolumeclaims',
    '?' + new URLSearchParams({ clusterId }),
    listNamespace,
  )
  const targetIdentity = JSON.stringify([clusterId, namespace, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const actions = useKubernetesResourceActions<PersistentVolumeClaimDetail>({
    resourceKey: 'pvc-detail',
    resourceLabel: 'PVC',
    scopeKey: targetIdentity,
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
                  ? '删除此声明可能影响引用它的工作负载，绑定卷和底层数据的后续处理取决于实际回收策略。'
                  : 'Deleting this claim may affect its consumers; volume and data handling depend on the actual reclaim policy.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync(target)
                if (!isCurrentTarget()) return
                navigate(collection)
              },
            },
          ]
        : [],
  })
  const apply = async () => {
    if (!canRead || !canUpdate || applyBlocked || !yaml || !yamlState.value.trim()) return
    try {
      pvcConfiguration(yamlState.value, name, namespace)
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
    return <ReadFeedback canRead={canRead} query={detailQuery} label="PVC" />
  if (identityError)
    return (
      <ManagementState
        kind="error"
        title={zh ? 'PVC 详情身份不匹配' : 'PVC detail identity mismatch'}
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
    return <ManagementState kind="not-found" title={zh ? 'PVC 未找到' : 'PVC not found'} />
  return (
    <div className="soha-pvc-detail">
      {actions.confirmation}
      <StorageDetailShell
        kind="PVC"
        name={name}
        detail={detail}
        scope={scope}
        header={
          <ResourceDetailHeader
            kind="PVC"
            name={name}
            namespace={namespace}
            status={detail.status}
            createdAt={detail.createdAt}
            ageSeconds={detail.ageSeconds}
            backTo="/storage/persistentvolumeclaims"
            facts={
              <>
                {detail.requested ? (
                  <span>
                    {zh ? '申请 ' : 'Requested '}
                    {detail.requested}
                  </span>
                ) : null}
                {detail.storageClass ? <span>{detail.storageClass}</span> : null}
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={[detailQuery, yamlQuery]}
                  resourceLabel="PVC"
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
        }
      >
        <ReadFeedback canRead={canRead} query={detailQuery} label="PVC" />
        {active !== 'pods' ? (
          <ReadFeedback
            canRead={canRead}
            query={yamlQuery}
            label={zh ? 'PVC 补充配置' : 'PVC configuration'}
          />
        ) : null}
        {active !== 'pods' && parsed.error ? (
          <ManagementState
            kind="error"
            title={zh ? 'PVC 补充配置无法解析' : 'PVC configuration could not be parsed'}
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
        <StorageDetailTabs
          activeKey={active}
          onChange={setTab}
          items={[
            {
              key: 'overview',
              label: zh ? '概览' : 'Overview',
              children: (
                <PvcOverview
                  detail={detail}
                  clusterId={clusterId}
                  configuration={parsed.configuration}
                  onPods={() => setTab('pods')}
                />
              ),
            },
            {
              key: 'pods',
              label: 'Pods',
              children: <PvcPodsTab detail={detail} clusterId={clusterId} />,
            },
            {
              key: 'relationships',
              label: zh ? '关系' : 'Relationships',
              children:
                active === 'relationships' ? (
                  <Suspense fallback={<ManagementState kind="loading" />}>
                    <StorageRelationships kind="PersistentVolumeClaim" name={name} scope={scope} />
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
                    <StorageYAMLPanel
                      readOnly={!canUpdate}
                      blocked={applyBlocked}
                      error={yamlQuery.error}
                      original={yamlState.original}
                      serverChanged={yamlState.serverChanged}
                      onCompareLatest={yamlState.compareLatest}
                      applying={update.isPending}
                      draft={yamlState.value}
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
      </StorageDetailShell>
    </div>
  )
}
