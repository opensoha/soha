import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
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
import { Alert, Button } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { ConfigurationDetailShell } from '../shared/detail-shell'
import { configurationMutations } from '../shared/mutations'
import { configurationQueries } from '../shared/queries'
import { hpaConfiguration, hpaStatus } from './hpa-model'
import { HPAOverview } from './overview'
import { StatusTag } from '@/components/status-tag'
import { ConfigurationQuickEditModal } from '../shared/configuration-quick-edit-modal'
import { hpaDetailQuery } from './queries'
import type { HorizontalPodAutoscalerResource } from './types'
import './styles.css'

export function ConfigurationHPADetailPage() {
  const { name = '' } = useParams()
  const { clusterId, namespace } = useWorkloadDetailScope()
  return (
    <HPADetailContent
      key={JSON.stringify([clusterId, namespace, name])}
      clusterId={clusterId || ''}
      namespace={namespace || ''}
      name={name}
    />
  )
}

function HPADetailContent({
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
  const [editing, setEditing] = useState(false)
  const active = ['yaml', 'relationships'].includes(tab) ? tab : 'overview'
  useEffect(() => {
    if (active !== tab) setTab('overview')
  }, [active, tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(
    permissions,
    'platform.configuration.horizontal-pod-autoscalers.view',
  )
  const scope = toScopeKey(clusterId, namespace),
    target = { scope, name }
  const refresh = useWorkloadRefresh()
  const options = hpaDetailQuery(scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const detail = canRead ? readableQueryData(query) : undefined
  const yamlOptions = configurationQueries.yaml('hpas', scope, name)
  const yaml = useQuery({
    ...yamlOptions,
    enabled: Boolean(detail && canRead && active === 'overview'),
    refetchInterval: active === 'overview' ? refresh.interval : false,
  })
  const yamlData = canRead ? readableQueryData(yaml) : undefined
  const configuration = useMemo<{
    value?: ReturnType<typeof hpaConfiguration>
    error?: boolean
  }>(() => {
    if (!detail || !yamlData) return {}
    try {
      return { value: hpaConfiguration(yamlData.content, detail) }
    } catch {
      return { error: true }
    }
  }, [detail, yamlData])
  const remove = usePlatformMutation(configurationMutations.remove('hpas', client), {
    label: ['删除 HorizontalPodAutoscaler', 'Delete HorizontalPodAutoscaler'],
    target: platformResourceTarget,
  })
  const canEdit = Boolean(
    detail &&
    hasPermission(permissions, 'platform.configuration.horizontal-pod-autoscalers.update') &&
    hasAllowedAction(detail.allowedActions, 'update'),
  )
  const canDelete = Boolean(
    detail &&
    hasPermission(permissions, 'platform.configuration.horizontal-pod-autoscalers.delete') &&
    hasAllowedAction(detail.allowedActions, 'delete'),
  )
  const targetIdentity = JSON.stringify([clusterId, namespace, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const actions = useKubernetesResourceActions<HorizontalPodAutoscalerResource>({
    resourceKey: 'hpa-detail',
    resourceLabel: 'HorizontalPodAutoscaler',
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
                  ? '删除 HPA 将停止此对象的自动扩缩，不会删除目标工作负载。'
                  : 'This removes this autoscaler, not its target workload.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync(target)
                if (!isCurrentTarget()) return
                navigate('/configuration/hpas?' + new URLSearchParams({ clusterId, namespace }))
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
    return <ReadFeedback canRead={canRead} query={query} label="HorizontalPodAutoscaler" />
  if (query.isPending) return <ManagementState kind="loading" />
  if (!detail)
    return (
      <ManagementState
        kind="not-found"
        description={zh ? 'HorizontalPodAutoscaler 未找到' : 'HorizontalPodAutoscaler not found'}
      />
    )
  const blocked = remove.blocked || Boolean(query.error)
  return (
    <div className="soha-hpa-detail">
      {actions.confirmation}
      {editing && canEdit && !blocked ? (
        <ConfigurationQuickEditModal
          kind="hpas"
          name={name}
          namespace={namespace}
          onClose={() => setEditing(false)}
        />
      ) : null}
      <ReadFeedback canRead={canRead} query={query} label="HorizontalPodAutoscaler" />
      {!actions.isConfirming && remove.feedback}
      <ConfigurationDetailShell
        kind="hpas"
        label="HorizontalPodAutoscaler"
        target={target}
        detail={detail}
        showReferences
        relatedResources={
          <WorkloadRelationships
            kind="HorizontalPodAutoscaler"
            name={name}
            scope={scope}
            active={active === 'relationships'}
          />
        }
        activeTabKey={active}
        onTabChange={setTab}
        preserveYaml
        canUpdateYAML={canEdit && !blocked}
        header={
          <ResourceDetailHeader
            kind="HorizontalPodAutoscaler"
            name={name}
            namespace={namespace}
            ageSeconds={detail.ageSeconds}
            backTo="/configuration/hpas"
            facts={
              <>
                <StatusTag
                  {...hpaStatus(configuration.value?.conditions ?? detail.conditions, zh)}
                />
                <span>
                  {zh ? '当前 ' : 'Current '}
                  {configuration.value
                    ? (configuration.value.currentReplicas ?? '—')
                    : detail.currentReplicas}{' '}
                  / {zh ? '期望 ' : 'Desired '}
                  {configuration.value
                    ? (configuration.value.desiredReplicas ?? '—')
                    : detail.desiredReplicas}
                </span>
                <span>
                  {detail.minReplicas} – {detail.maxReplicas}
                </span>
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={[
                    query,
                    ...(active === 'yaml' ? [{ ...yaml, isEnabled: true }] : [yaml]),
                  ]}
                  resourceLabel="HorizontalPodAutoscaler"
                  onRefresh={() => client.invalidateQueries({ queryKey: options.queryKey })}
                  beforeRefresh={
                    canEdit ? (
                      <Button disabled={blocked} onClick={() => setEditing(true)}>
                        {zh ? '编辑扩缩' : 'Edit autoscaling'}
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
        overview={
          <>
            <ReadFeedback
              canRead={canRead}
              query={yaml}
              label={zh ? '扩缩配置' : 'Autoscaling configuration'}
            />
            {configuration.error ? (
              <Alert
                showIcon
                type="warning"
                title={
                  zh
                    ? '扩缩配置无法解析，暂不展示策略与精确指标类型。可在 YAML 查看。'
                    : 'Autoscaling configuration could not be parsed. Inspect YAML for behavior and precise metric types.'
                }
              />
            ) : null}
            <HPAOverview
              detail={detail}
              configuration={configuration.value}
              clusterId={clusterId}
            />
          </>
        }
      />
    </div>
  )
}
