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
import type { PersistentVolumeDetail } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Spin } from 'antd'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { StorageDetailShell, StorageDetailTabs } from '../shared/detail-layout'
import { pvConfiguration } from './detail-model'
import { persistentVolumeMutations } from './mutations'
import { persistentVolumeQueries } from './queries'
import { PvOverview } from './overview'
import { toClusterStorageScope } from '../shared/scope'
import './styles.css'

const StorageRelationships = lazy(async () => {
  const module = await import('@/features/platform/shared/resource-insights/workload-relationships')
  return { default: module.WorkloadRelationships }
})
const StorageYAMLPanel = lazy(() => import('../shared/yaml-panel'))
const permission = 'platform.storage.persistent-volumes'

export function StoragePvDetailPage() {
  const { name = '' } = useParams()
  const { clusterId } = useWorkloadDetailScope()
  return (
    <PvDetailContent
      key={JSON.stringify([clusterId, name])}
      clusterId={clusterId || ''}
      name={name}
    />
  )
}
function PvDetailContent({ clusterId, name }: { clusterId: string; name: string }) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const navigate = useNavigate()
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
  const permissionsQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionsQuery)?.data
  const canRead = hasPermission(snapshot, `${permission}.view`)
  const scope = toClusterStorageScope(clusterId)
  const target = { scope, name }
  const options = persistentVolumeQueries.detail(scope, name)
  const detailQuery = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const yamlOptions = persistentVolumeQueries.yaml(scope, name)
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
      return { configuration: pvConfiguration(yaml.content, name) }
    } catch {
      return { error: true }
    }
  }, [yaml, name])
  const yamlState = useYamlDraft(
    JSON.stringify([clusterId, name]),
    parsed.error ? undefined : yaml?.content,
  )
  const update = usePlatformMutation(persistentVolumeMutations.updateYAML(client), {
    label: ['应用 PV YAML', 'Apply PV YAML'],
    target: platformResourceTarget,
  })
  const remove = usePlatformMutation(persistentVolumeMutations.remove(client), {
    label: ['删除 PV', 'Delete PV'],
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
    '/storage/persistentvolumes',
    '?' + new URLSearchParams({ clusterId }),
    null,
  )
  const targetIdentity = JSON.stringify([clusterId, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const actions = useKubernetesResourceActions<PersistentVolumeDetail>({
    resourceKey: 'pv-detail',
    resourceLabel: 'PV',
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
                  ? `删除此 PV 对象可能影响使用它的声明与工作负载；当前回收策略为 ${detail?.reclaimPolicy || '未提供'}。底层数据的后续处理取决于实际回收策略与存储后端。`
                  : `Deleting this PV object may affect its claim and workloads. Reported reclaim policy: ${detail?.reclaimPolicy || 'not provided'}. Data handling depends on the actual policy and storage backend.`,
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
      pvConfiguration(yamlState.value, name)
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
    return <ReadFeedback canRead={canRead} query={detailQuery} label="PV" />
  if (identityError)
    return (
      <ManagementState
        kind="error"
        title={zh ? 'PV 详情身份不匹配' : 'PV detail identity mismatch'}
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
  if (!detail) return <ManagementState kind="not-found" title={zh ? 'PV 未找到' : 'PV not found'} />
  return (
    <div className="soha-pv-detail">
      {actions.confirmation}
      <StorageDetailShell
        kind="PV"
        name={name}
        detail={detail}
        scope={scope}
        header={
          <ResourceDetailHeader
            kind="PV"
            name={name}
            status={detail.status}
            createdAt={detail.createdAt}
            ageSeconds={detail.ageSeconds}
            backTo="/storage/persistentvolumes"
            facts={
              <>
                {detail.capacity ? (
                  <span>
                    {zh ? '容量 ' : 'Capacity '}
                    {detail.capacity}
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
                  resourceLabel="PV"
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
        <ReadFeedback canRead={canRead} query={detailQuery} label="PV" />
        <ReadFeedback
          canRead={canRead}
          query={yamlQuery}
          label={zh ? 'PV 补充配置' : 'PV configuration'}
        />
        {parsed.error ? (
          <ManagementState
            kind="error"
            title={zh ? 'PV 补充配置无法解析' : 'PV configuration could not be parsed'}
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
                <PvOverview
                  detail={detail}
                  clusterId={clusterId}
                  configuration={parsed.configuration}
                  onYaml={() => setTab('yaml')}
                />
              ),
            },
            {
              key: 'relationships',
              label: zh ? '关系' : 'Relationships',
              children:
                active === 'relationships' ? (
                  <Suspense fallback={<ManagementState kind="loading" />}>
                    <StorageRelationships kind="PersistentVolume" name={name} scope={scope} />
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
