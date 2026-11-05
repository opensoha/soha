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
import { pdbConfiguration, pdbBudgetStatus } from './pdb-model'
import { PDBOverview, PDBPods } from './overview'
import { StatusTag } from '@/components/status-tag'
import { ConfigurationQuickEditModal } from '../shared/configuration-quick-edit-modal'
import { pdbDetailQuery } from './queries'
import type { PodDisruptionBudgetResource } from './types'
import './styles.css'

export function ConfigurationPDBDetailPage() {
  const { name = '' } = useParams()
  const { clusterId, namespace } = useWorkloadDetailScope()
  return (
    <PDBDetailContent
      key={JSON.stringify([clusterId, namespace, name])}
      clusterId={clusterId || ''}
      namespace={namespace || ''}
      name={name}
    />
  )
}

function PDBDetailContent({
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
  const active = ['yaml', 'pods', 'relationships'].includes(tab) ? tab : 'overview'
  useEffect(() => {
    if (active !== tab) setTab('overview')
  }, [active, tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(permissions, 'platform.configuration.pod-disruption-budgets.view')
  const scope = toScopeKey(clusterId, namespace),
    target = { scope, name }
  const refresh = useWorkloadRefresh()
  const options = pdbDetailQuery(scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const detail = canRead ? readableQueryData(query) : undefined
  const yamlOptions = configurationQueries.yaml('poddisruptionbudgets', scope, name)
  const yaml = useQuery({
    ...yamlOptions,
    enabled: Boolean(detail && canRead && active !== 'yaml'),
    refetchInterval: active !== 'yaml' ? refresh.interval : false,
  })
  const yamlData = canRead ? readableQueryData(yaml) : undefined
  const configuration = useMemo<{
    value?: ReturnType<typeof pdbConfiguration>
    error?: boolean
  }>(() => {
    if (!detail || !yamlData) return {}
    try {
      return { value: pdbConfiguration(yamlData.content, detail) }
    } catch {
      return { error: true }
    }
  }, [detail, yamlData])
  const remove = usePlatformMutation(
    configurationMutations.remove('poddisruptionbudgets', client),
    {
      label: ['删除 PodDisruptionBudget', 'Delete PodDisruptionBudget'],
      target: platformResourceTarget,
    },
  )
  const canEdit = Boolean(
    detail &&
    hasPermission(permissions, 'platform.configuration.pod-disruption-budgets.update') &&
    hasAllowedAction(detail.allowedActions, 'update'),
  )
  const canDelete = Boolean(
    detail &&
    hasPermission(permissions, 'platform.configuration.pod-disruption-budgets.delete') &&
    hasAllowedAction(detail.allowedActions, 'delete'),
  )
  const targetIdentity = JSON.stringify([clusterId, namespace, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const actions = useKubernetesResourceActions<PodDisruptionBudgetResource>({
    resourceKey: 'pdb-detail',
    resourceLabel: 'PodDisruptionBudget',
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
                  ? '删除 PDB 将解除此对象施加的自愿驱逐保护，不会删除 Pod 或工作负载。'
                  : 'This removes this disruption budget, not the Pods or workload.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync(target)
                if (!isCurrentTarget()) return
                navigate(
                  '/configuration/poddisruptionbudgets?' +
                    new URLSearchParams({ clusterId, namespace }),
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
    return <ReadFeedback canRead={canRead} query={query} label="PodDisruptionBudget" />
  if (query.isPending) return <ManagementState kind="loading" />
  if (!detail)
    return (
      <ManagementState
        kind="not-found"
        description={zh ? 'PodDisruptionBudget 未找到' : 'PodDisruptionBudget not found'}
      />
    )
  const blocked = remove.blocked || Boolean(query.error)
  return (
    <div className="soha-pdb-detail">
      {actions.confirmation}
      {editing && canEdit && !blocked ? (
        <ConfigurationQuickEditModal
          kind="poddisruptionbudgets"
          name={name}
          namespace={namespace}
          onClose={() => setEditing(false)}
        />
      ) : null}
      <ReadFeedback canRead={canRead} query={query} label="PodDisruptionBudget" />
      {!actions.isConfirming && remove.feedback}
      <ConfigurationDetailShell
        kind="poddisruptionbudgets"
        label="PodDisruptionBudget"
        target={target}
        detail={detail}
        showReferences
        relatedResources={
          <WorkloadRelationships
            kind="PodDisruptionBudget"
            name={name}
            scope={scope}
            active={active === 'relationships'}
          />
        }
        additionalTabs={[
          {
            key: 'pods',
            label: 'Pods',
            children:
              active === 'pods' ? (
                <>
                  <ReadFeedback
                    canRead={canRead}
                    query={yaml}
                    label={zh ? '预算配置' : 'Budget configuration'}
                  />
                  {configuration.error ? (
                    <Alert
                      showIcon
                      type="warning"
                      title={
                        zh
                          ? '预算选择器无法解析，不能确认受保护范围。'
                          : 'The budget selector could not be parsed; selected Pods cannot be confirmed.'
                      }
                    />
                  ) : null}
                  <PDBPods detail={detail} configuration={configuration.value} scope={scope} />
                </>
              ) : null,
          },
        ]}
        activeTabKey={active}
        onTabChange={setTab}
        preserveYaml
        canUpdateYAML={canEdit && !blocked}
        header={
          <ResourceDetailHeader
            kind="PodDisruptionBudget"
            name={name}
            namespace={namespace}
            ageSeconds={detail.ageSeconds}
            backTo="/configuration/poddisruptionbudgets"
            facts={
              <>
                <StatusTag {...pdbBudgetStatus(configuration.value, zh)} />
                <span>
                  {zh ? '允许驱逐 ' : 'Disruptions allowed '}
                  {configuration.value
                    ? (configuration.value.disruptionsAllowed ?? '—')
                    : detail.disruptionsAllowed}
                </span>
                <span>
                  {zh ? '健康 ' : 'Healthy '}
                  {configuration.value
                    ? (configuration.value.currentHealthy ?? '—')
                    : detail.currentHealthy}{' '}
                  /{' '}
                  {configuration.value
                    ? (configuration.value.desiredHealthy ?? '—')
                    : detail.desiredHealthy}
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
                  resourceLabel="PodDisruptionBudget"
                  onRefresh={() => client.invalidateQueries({ queryKey: options.queryKey })}
                  beforeRefresh={
                    canEdit ? (
                      <Button disabled={blocked} onClick={() => setEditing(true)}>
                        {zh ? '编辑预算' : 'Edit budget'}
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
              label={zh ? '预算配置' : 'Budget configuration'}
            />
            {configuration.error ? (
              <Alert
                showIcon
                type="warning"
                title={
                  zh
                    ? '预算配置无法解析，暂不展示策略与受保护范围。可在 YAML 查看。'
                    : 'Budget configuration could not be parsed. Inspect YAML for policy and selection.'
                }
              />
            ) : null}
            <PDBOverview
              detail={detail}
              configuration={configuration.value}
              scope={scope}
              onShowPods={() => setTab('pods')}
            />
          </>
        }
      />
    </div>
  )
}
