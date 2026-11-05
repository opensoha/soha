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
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from 'antd'
import { useEffect } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { ConfigurationDetailShell } from '../shared/detail-shell'
import { configurationMutations } from '../shared/mutations'
import { configurationQueries } from '../shared/queries'
import { limitGroups } from './limit-model'
import { LimitRangeOverview } from './overview'
import { limitRangeDetailQuery } from './queries'
import type { LimitRangeResource } from './types'
import './styles.css'

export function ConfigurationLimitRangeDetailPage() {
  const { name = '' } = useParams()
  const { clusterId, namespace } = useWorkloadDetailScope()
  return (
    <LimitRangeDetailContent
      key={JSON.stringify([clusterId, namespace, name])}
      clusterId={clusterId || ''}
      namespace={namespace || ''}
      name={name}
    />
  )
}

function LimitRangeDetailContent({
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
  const client = useQueryClient(),
    navigate = useNavigate()
  const [tab, setTab] = useResourceDetailTab()
  const active = tab === 'yaml' ? 'yaml' : 'overview'
  useEffect(() => {
    if (active !== tab) setTab('overview')
  }, [active, tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(permissions, 'platform.configuration.limit-ranges.view')
  const scope = toScopeKey(clusterId, namespace),
    target = { scope, name }
  const refresh = useWorkloadRefresh()
  const options = limitRangeDetailQuery(scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const detail = canRead ? readableQueryData(query) : undefined
  const yaml = useQuery({
    ...configurationQueries.yaml('limitranges', scope, name),
    enabled: false,
  })
  const remove = usePlatformMutation(configurationMutations.remove('limitranges', client), {
    label: ['删除 LimitRange', 'Delete LimitRange'],
    target: platformResourceTarget,
  })
  const canEdit = Boolean(
    detail &&
    hasPermission(permissions, 'platform.configuration.limit-ranges.update') &&
    hasAllowedAction(detail.allowedActions, 'update'),
  )
  const canDelete = Boolean(
    detail &&
    hasPermission(permissions, 'platform.configuration.limit-ranges.delete') &&
    hasAllowedAction(detail.allowedActions, 'delete'),
  )
  const targetIdentity = JSON.stringify([clusterId, namespace, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const actions = useKubernetesResourceActions<LimitRangeResource>({
    resourceKey: 'limitrange-detail',
    resourceLabel: 'LimitRange',
    scopeKey: targetIdentity,
    records: detail ? [detail] : [],
    getName: (record) => record.name,
    getKey: (record) => record.namespace + '/' + record.name,
    actions: () =>
      canDelete
        ? [
            {
              key: 'delete',
              label: zh ? '删除' : 'Delete',
              danger: true,
              disabled: remove.blocked || remove.isPending || Boolean(query.error),
              confirm: kubernetesDeleteConfirmation(
                { name, namespace },
                localeCode,
                zh
                  ? '后续准入不再受此对象约束；不会删除已有 Pod 或 PVC。'
                  : 'Future admissions will no longer use this object. Existing Pods and PVCs will not be deleted.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync(target)
                if (!isCurrentTarget()) return
                navigate(
                  '/configuration/limitranges?' + new URLSearchParams({ clusterId, namespace }),
                )
              },
            },
          ]
        : [],
  })
  if (!clusterId || !namespace)
    return (
      <ManagementState
        kind="select-scope"
        description={zh ? '请选择集群和命名空间' : 'Select a cluster and namespace'}
      />
    )
  if (!canRead || (query.isError && !detail))
    return <ReadFeedback canRead={canRead} query={query} label="LimitRange" />
  if (query.isPending) return <ManagementState kind="loading" />
  if (!detail)
    return (
      <ManagementState
        kind="not-found"
        description={zh ? 'LimitRange 未找到' : 'LimitRange not found'}
      />
    )
  const blocked = remove.blocked || Boolean(query.error)
  return (
    <div className="soha-limitrange-detail">
      {actions.confirmation}
      <ReadFeedback canRead={canRead} query={query} label="LimitRange" />
      {!actions.isConfirming && remove.feedback}
      <ConfigurationDetailShell
        kind="limitranges"
        label="LimitRange"
        target={target}
        detail={detail}
        showReferences={false}
        activeTabKey={active}
        onTabChange={setTab}
        preserveYaml
        canUpdateYAML={canEdit && !blocked}
        header={
          <ResourceDetailHeader
            kind="LimitRange"
            name={name}
            namespace={namespace}
            ageSeconds={detail.ageSeconds}
            backTo="/configuration/limitranges"
            facts={
              <>
                <span>{zh ? detail.limits + ' 条规则' : detail.limits + ' rules'}</span>
                {limitGroups(detail.rules || []).map((group) => (
                  <MetadataTag key={group.type} label={group.type} />
                ))}
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={[query, ...(active === 'yaml' ? [{ ...yaml, isEnabled: true }] : [])]}
                  resourceLabel="LimitRange"
                  onRefresh={() => client.invalidateQueries({ queryKey: options.queryKey })}
                  beforeRefresh={
                    canEdit ? (
                      <Button disabled={blocked} onClick={() => setTab('yaml')}>
                        {zh ? '编辑限制' : 'Edit limits'}
                      </Button>
                    ) : undefined
                  }
                />
                {canDelete ? (
                  <Button
                    danger
                    disabled={blocked || remove.isPending}
                    onClick={() => actions.selectAction(detail, 'delete')}
                  >
                    {zh ? '删除' : 'Delete'}
                  </Button>
                ) : null}
              </>
            }
          />
        }
        overview={<LimitRangeOverview detail={detail} />}
      />
    </div>
  )
}
