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
import type { StorageClassDetail } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Spin } from 'antd'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { StorageDetailShell, StorageDetailTabs } from '../shared/detail-layout'
import { scConfiguration } from './detail-model'
import { storageClassMutations } from './mutations'
import { storageClassQueries } from './queries'
import { ScOverview } from './overview'
import { ScRelations } from './relations'
import { toClusterStorageScope } from '../shared/scope'
import './styles.css'

const StorageRelationships = lazy(async () => {
  const module = await import('@/features/platform/shared/resource-insights/workload-relationships')
  return { default: module.WorkloadRelationships }
})
const StorageYAMLPanel = lazy(() => import('../shared/yaml-panel'))
const permission = 'platform.storage.storage-classes'

export function StorageClassDetailPage() {
  const { name = '' } = useParams()
  const { clusterId } = useWorkloadDetailScope()
  return (
    <ScDetailContent
      key={JSON.stringify([clusterId, name])}
      clusterId={clusterId || ''}
      name={name}
    />
  )
}
function ScDetailContent({ clusterId, name }: { clusterId: string; name: string }) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const navigate = useNavigate()
  const client = useQueryClient()
  const [tab, setTab] = useResourceDetailTab()
  const active = ['overview', 'volumes', 'claims', 'relationships', 'yaml'].includes(tab)
    ? tab
    : 'overview'
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
  const scope = toClusterStorageScope(clusterId)
  const target = { scope, name }
  const options = storageClassQueries.detail(scope, name)
  const detailQuery = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const yamlOptions = storageClassQueries.yaml(scope, name)
  const yamlQuery = useQuery({
    ...yamlOptions,
    enabled: Boolean(yamlOptions.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const candidate = canRead ? readableQueryData(detailQuery) : undefined
  const identityError = Boolean(candidate && candidate.name !== name)
  const detail = !identityError ? candidate : undefined
  const yaml = canRead ? readableQueryData(yamlQuery) : undefined
  const parsed = useMemo(() => {
    if (!yaml) return {}
    try {
      return { configuration: scConfiguration(yaml.content, name) }
    } catch {
      return { error: true }
    }
  }, [yaml, name])
  const yamlState = useYamlDraft(
    JSON.stringify([clusterId, name]),
    parsed.error ? undefined : yaml?.content,
  )
  const update = usePlatformMutation(storageClassMutations.updateYAML(client), {
    label: ['应用 StorageClass YAML', 'Apply StorageClass YAML'],
    target: platformResourceTarget,
  })
  const remove = usePlatformMutation(storageClassMutations.remove(client), {
    label: ['删除 StorageClass', 'Delete StorageClass'],
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
    '/storage/storageclasses',
    '?' + new URLSearchParams({ clusterId }),
    null,
  )
  const targetIdentity = JSON.stringify([clusterId, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const actions = useKubernetesResourceActions<StorageClassDetail>({
    resourceKey: 'sc-detail',
    resourceLabel: 'StorageClass',
    scopeKey: targetIdentity,
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
                  ? '删除此存储类会影响后续置备；不代表已有 PV、PVC 或底层数据被删除。'
                  : 'Deleting this storage class affects future provisioning; it does not mean existing volumes, claims or data were deleted.',
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
      scConfiguration(yamlState.value, name)
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
    return <ReadFeedback canRead={canRead} query={detailQuery} label="StorageClass" />
  if (identityError)
    return (
      <ManagementState
        kind="error"
        title={zh ? 'StorageClass 详情身份不匹配' : 'StorageClass detail identity mismatch'}
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
        title={zh ? 'StorageClass 未找到' : 'StorageClass not found'}
      />
    )
  return (
    <div className="soha-sc-detail">
      {actions.confirmation}
      <StorageDetailShell
        kind="StorageClass"
        name={name}
        detail={detail}
        scope={scope}
        header={
          <ResourceDetailHeader
            kind="StorageClass"
            name={name}
            createdAt={detail.createdAt}
            ageSeconds={detail.ageSeconds}
            backTo="/storage/storageclasses"
            facts={<span>{detail.provisioner}</span>}
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={[detailQuery, yamlQuery]}
                  resourceLabel="StorageClass"
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
        <ReadFeedback canRead={canRead} query={detailQuery} label="StorageClass" />
        <ReadFeedback
          canRead={canRead}
          query={yamlQuery}
          label={zh ? 'StorageClass 补充配置' : 'StorageClass configuration'}
        />
        {parsed.error ? (
          <ManagementState
            kind="error"
            title={
              zh
                ? 'StorageClass 补充配置无法解析'
                : 'StorageClass configuration could not be parsed'
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
        <StorageDetailTabs
          activeKey={active}
          onChange={setTab}
          items={[
            {
              key: 'overview',
              label: zh ? '概览' : 'Overview',
              children: (
                <ScOverview
                  detail={detail}
                  clusterId={clusterId}
                  configuration={parsed.configuration}
                  onTab={setTab}
                />
              ),
            },
            {
              key: 'volumes',
              label: 'PVs',
              children: <ScRelations detail={detail} clusterId={clusterId} kind="volumes" />,
            },
            {
              key: 'claims',
              label: 'PVCs',
              children: <ScRelations detail={detail} clusterId={clusterId} kind="claims" />,
            },
            {
              key: 'relationships',
              label: zh ? '关系' : 'Relationships',
              children:
                active === 'relationships' ? (
                  <Suspense fallback={<ManagementState kind="loading" />}>
                    <StorageRelationships kind="StorageClass" name={name} scope={scope} />
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
