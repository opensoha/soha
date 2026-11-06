import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
import type { RelationshipRootKind } from '@/features/platform/shared/resource-insights/relationship-model'
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
import { useEffect, useMemo, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { ConfigurationDetailShell } from './detail-shell'
import { configurationMutations } from './mutations'
import { configurationQueries } from './queries'
import type { ConfigurationDetailBase, ConfigurationResourceRecord } from './types'

interface YAMLDetailProps<T extends ConfigurationDetailBase> {
  kind:
    | 'priorityclasses'
    | 'runtimeclasses'
    | 'leases'
    | 'mutatingwebhookconfigurations'
    | 'validatingwebhookconfigurations'
  label: string
  className: string
  fromYAML: (content: string, name: string, namespace?: string) => T
  renderOverview: (detail: T) => ReactNode
  tabs?: readonly { key: string; label: ReactNode; render: (detail: T) => ReactNode }[]
  renderFacts?: (detail: T, zh: boolean) => ReactNode
  deleteDescription: readonly [string, string]
}
export function ConfigurationYAMLDetailPage<T extends ConfigurationDetailBase>(
  props: YAMLDetailProps<T>,
) {
  const { name = '' } = useParams()
  const scope = useWorkloadDetailScope()
  const clusterId = scope.clusterId || ''
  const namespace = props.kind === 'leases' ? scope.namespace || '' : ''
  return (
    <YAMLDetailContent
      key={JSON.stringify([props.kind, clusterId, namespace, name])}
      {...props}
      clusterId={clusterId}
      namespace={namespace}
      name={name}
    />
  )
}
function YAMLDetailContent<T extends ConfigurationDetailBase>({
  clusterId,
  namespace,
  name,
  kind,
  label,
  className,
  fromYAML,
  renderOverview,
  renderFacts,
  tabs,
  deleteDescription,
}: YAMLDetailProps<T> & { clusterId: string; namespace: string; name: string }) {
  const permissionKey = {
    priorityclasses: 'platform.configuration.priority-classes',
    runtimeclasses: 'platform.configuration.runtime-classes',
    leases: 'platform.configuration.leases',
    mutatingwebhookconfigurations: 'platform.configuration.mutating-webhook-configurations',
    validatingwebhookconfigurations: 'platform.configuration.validating-webhook-configurations',
  }[kind]
  const scopeMode = kind === 'leases' ? 'namespace' : 'cluster'
  const collection = '/configuration/' + kind
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const client = useQueryClient(),
    navigate = useNavigate(),
    refresh = useWorkloadRefresh()
  const relationshipKind: RelationshipRootKind | undefined = {
    leases: undefined,
    priorityclasses: 'PriorityClass',
    runtimeclasses: 'RuntimeClass',
    mutatingwebhookconfigurations: 'MutatingWebhookConfiguration',
    validatingwebhookconfigurations: 'ValidatingWebhookConfiguration',
  }[kind] as RelationshipRootKind | undefined
  const [tab, setTab] = useResourceDetailTab(),
    active =
      tab === 'yaml' ||
      (relationshipKind && tab === 'relationships') ||
      tabs?.some((item) => item.key === tab)
        ? tab
        : 'overview'
  useEffect(() => {
    if (active !== tab) setTab('overview')
  }, [active, tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(permissions, `${permissionKey}.view`)
  const scope = toScopeKey(clusterId, namespace || null),
    target = { scope, name }
  const options = configurationQueries.yaml(kind, scope, name, scopeMode)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const yaml = canRead ? readableQueryData(query) : undefined
  const parsed = useMemo<{ detail?: T; error?: boolean }>(() => {
    if (!yaml) return {}
    try {
      return { detail: fromYAML(yaml.content, name, namespace || undefined) }
    } catch {
      return { error: true }
    }
  }, [yaml, name, namespace, fromYAML])
  const detail = parsed.detail
  // List data supplies only server-authorized actions, never the detail fields.
  const listOptions = configurationQueries.list<ConfigurationResourceRecord>(kind, scope)
  const list = useQuery({
    ...listOptions,
    enabled:
      Boolean(listOptions.enabled) && canRead && (scopeMode === 'cluster' || Boolean(namespace)),
    refetchInterval: refresh.interval,
  })
  const actionRecord = canRead
    ? readableQueryData(list)?.find(
        (item) => item.name === name && (scopeMode === 'cluster' || item.namespace === namespace),
      )
    : undefined
  const canEdit = Boolean(
    detail &&
    !list.error &&
    hasPermission(permissions, `${permissionKey}.update`) &&
    hasAllowedAction(actionRecord?.allowedActions, 'update'),
  )
  const canDelete = Boolean(
    detail &&
    !list.error &&
    hasPermission(permissions, `${permissionKey}.delete`) &&
    hasAllowedAction(actionRecord?.allowedActions, 'delete'),
  )
  const remove = usePlatformMutation(configurationMutations.remove(kind, client), {
    label: [`删除 ${label}`, `Delete ${label}`],
    target: platformResourceTarget,
  })
  const targetIdentity = JSON.stringify([clusterId, namespace, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const actions = useKubernetesResourceActions<T>({
    resourceKey:
      kind === 'priorityclasses'
        ? 'priorityclass-detail'
        : kind === 'runtimeclasses'
          ? 'runtimeclass-detail'
          : kind === 'leases'
            ? 'lease-detail'
            : kind === 'mutatingwebhookconfigurations'
              ? 'mutatingwebhookconfiguration-detail'
              : 'validatingwebhookconfiguration-detail',
    resourceLabel: label,
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
              disabled: remove.blocked || remove.isPending || Boolean(query.error),
              confirm: kubernetesDeleteConfirmation(
                { name, namespace: namespace || undefined },
                localeCode,
                deleteDescription[zh ? 0 : 1],
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync(target)
                if (!isCurrentTarget()) return
                navigate(
                  collection +
                    '?' +
                    new URLSearchParams({ clusterId, ...(namespace ? { namespace } : {}) }),
                )
              },
            },
          ]
        : [],
  })
  if (!clusterId || (scopeMode === 'namespace' && !namespace))
    return (
      <ManagementState
        kind="select-scope"
        description={zh ? '请选择集群及所需命名空间' : 'Select a cluster and required namespace'}
      />
    )
  if (!canRead || (query.isError && !yaml))
    return <ReadFeedback canRead={canRead} query={query} label={label} />
  if (query.isPending) return <ManagementState kind="loading" />
  if (parsed.error)
    return (
      <ManagementState
        kind="error"
        title={zh ? `${label} 配置无法解析` : `${label} configuration could not be parsed`}
        description={
          zh
            ? '未能确认资源身份或配置字段，请重新读取。'
            : 'Resource identity or configuration fields could not be confirmed. Read the resource again.'
        }
        actions={<Button onClick={() => void query.refetch()}>{zh ? '重试' : 'Retry'}</Button>}
      />
    )
  if (!detail)
    return (
      <ManagementState kind="not-found" title={zh ? `${label} 未找到` : `${label} not found`} />
    )
  const blocked = remove.blocked || Boolean(query.error)
  return (
    <div className={className}>
      {actions.confirmation}
      <ReadFeedback canRead={canRead} query={query} label={label} />
      {!actions.isConfirming && remove.feedback}
      <ReadFeedback canRead={canRead} query={list} label={zh ? '操作权限' : 'Action permissions'} />
      <ConfigurationDetailShell
        kind={kind}
        label={label}
        scopeMode={scopeMode}
        target={target}
        detail={detail}
        showReferences={Boolean(relationshipKind)}
        relatedResources={
          relationshipKind ? (
            <WorkloadRelationships
              kind={relationshipKind}
              name={name}
              scope={scope}
              active={active === 'relationships'}
            />
          ) : undefined
        }
        additionalTabs={tabs?.map((item) => ({
          key: item.key,
          label: item.label,
          children: active === item.key ? item.render(detail) : null,
        }))}
        activeTabKey={active}
        onTabChange={setTab}
        preserveYaml
        canUpdateYAML={canEdit && !blocked}
        header={
          <ResourceDetailHeader
            kind={label}
            name={name}
            namespace={namespace || undefined}
            createdAt={detail.createdAt}
            backTo={collection}
            facts={
              <>
                {scopeMode === 'cluster' ? (
                  <MetadataTag label={zh ? '集群级' : 'Cluster scope'} />
                ) : null}
                {renderFacts?.(detail, zh)}
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={[query, list]}
                  resourceLabel={label}
                  onRefresh={() =>
                    Promise.all([
                      client.invalidateQueries({ queryKey: options.queryKey }),
                      client.invalidateQueries({ queryKey: listOptions.queryKey }),
                    ])
                  }
                  beforeRefresh={
                    canEdit ? (
                      <Button disabled={blocked} onClick={() => setTab('yaml')}>
                        {zh ? '编辑 YAML' : 'Edit YAML'}
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
        overview={renderOverview(detail)}
      />
    </div>
  )
}
