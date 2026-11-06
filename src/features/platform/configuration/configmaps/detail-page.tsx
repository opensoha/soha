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
import { Button } from 'antd'
import { useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { ConfigurationDetailShell } from '../shared/detail-shell'
import { configurationQueries } from '../shared/queries'
import { ConfigMapDataTab } from './data-tab'
import { configMapMutations } from './mutations'
import { configMapQueries } from './queries'
import { ConfigMapOverview } from './overview'
import type { ConfigMapDetail } from './types'
import './styles.css'

export function ConfigMapDetailPage() {
  const { configMapName = '' } = useParams()
  const { clusterId, namespace } = useWorkloadDetailScope()
  const detailNamespace = namespace || ''
  return (
    <ConfigMapDetailContent
      key={JSON.stringify([clusterId, detailNamespace, configMapName])}
      clusterId={clusterId || ''}
      namespace={detailNamespace}
      name={configMapName}
    />
  )
}

function ConfigMapDetailContent({
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
  const [params] = useSearchParams()
  const [tab, setTab] = useResourceDetailTab()
  const active = ['overview', 'data', 'relationships', 'yaml'].includes(tab) ? tab : 'overview'
  const [editRequest, setEditRequest] = useState(params.get('edit') === 'true' ? 1 : 0)
  useEffect(() => {
    if (active !== tab) setTab('overview')
  }, [active, tab, setTab])
  const scope = toScopeKey(clusterId, namespace)
  const target = { scope, name }
  const targetIdentity = JSON.stringify([clusterId, namespace, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const permissionQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionQuery)?.data
  const canRead = hasPermission(snapshot, 'platform.configuration.config-maps.view')
  const refresh = useWorkloadRefresh()
  const options = configMapQueries.detail(scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const yamlObserver = useQuery({
    ...configurationQueries.yaml('configmaps', scope, name),
    enabled: false,
  })

  const detail = canRead ? readableQueryData(query) : undefined
  const listOptions = configMapQueries.list(scope)
  const actionQuery = useQuery({
    ...listOptions,
    enabled: Boolean(clusterId && namespace && canRead),
    refetchInterval: refresh.interval,
  })
  const allowedActions =
    canRead && !actionQuery.error && !permissionQuery.error
      ? actionQuery.data?.find((item) => item.name === name && item.namespace === namespace)
          ?.allowedActions
      : undefined
  const update = usePlatformMutation(configMapMutations.updateData(client), {
    label: ['应用数据更改', 'Apply data changes'],
    target: (variables) => platformResourceTarget(variables.target),
  })
  const remove = usePlatformMutation(configMapMutations.remove(client), {
    label: ['删除 ConfigMap', 'Delete ConfigMap'],
    target: platformResourceTarget,
  })
  const canEdit = Boolean(
    detail &&
    !detail.immutable &&
    hasPermission(snapshot, 'platform.configuration.config-maps.update') &&
    hasAllowedAction(allowedActions, 'update'),
  )
  const canDelete = Boolean(
    detail &&
    hasPermission(snapshot, 'platform.configuration.config-maps.delete') &&
    hasAllowedAction(allowedActions, 'delete'),
  )
  const collection = `/configuration/configmaps?${new URLSearchParams({ clusterId, namespace })}`
  const actions = useKubernetesResourceActions<ConfigMapDetail>({
    resourceKey: 'configmap-detail',
    resourceLabel: 'ConfigMap',
    records: detail ? [detail] : [],
    scopeKey: targetIdentity,
    getName: (record) => record.name,
    getKey: (record) => `${record.namespace}/${record.name}`,
    actions: () =>
      canDelete
        ? [
            {
              key: 'delete',
              label: zh ? '删除' : 'Delete',
              danger: true,
              disabled: remove.blocked || update.blocked || Boolean(query.error),
              confirm: kubernetesDeleteConfirmation(
                { name, namespace },
                localeCode,
                zh
                  ? '引用此 ConfigMap 的工作负载可能无法读取配置。'
                  : 'Workloads referencing this ConfigMap may no longer read its configuration.',
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
  if (!clusterId || !namespace)
    return (
      <ManagementState
        kind="select-scope"
        description={zh ? '请选择集群和命名空间' : 'Select a cluster and namespace'}
      />
    )
  if (!canRead || (query.isError && !detail))
    return <ReadFeedback canRead={canRead} query={query} label="ConfigMap" />
  if (query.isPending)
    return (
      <ManagementState kind="loading" description={zh ? '加载 ConfigMap' : 'Loading ConfigMap'} />
    )
  if (!detail)
    return (
      <ManagementState
        kind="not-found"
        description={zh ? 'ConfigMap 未找到' : 'ConfigMap not found'}
      />
    )
  const blocked = update.blocked || remove.blocked || Boolean(query.error)
  const edit = () => {
    setTab('data')
    setEditRequest((value) => value + 1)
  }
  return (
    <div className="soha-configmap-detail">
      {actions.confirmation}
      <ReadFeedback canRead={canRead} query={query} label="ConfigMap" />
      <ReadFeedback canRead={canRead} query={actionQuery} label={zh ? '操作权限' : 'Action permissions'} />
      {update.feedback}
      {!actions.isConfirming && remove.feedback}
      <ConfigurationDetailShell
        target={target}
        detail={detail}
        kind="configmaps"
        label="ConfigMap"
        activeTabKey={active}
        onTabChange={setTab}
        preserveYaml
        canUpdateYAML={canEdit && !blocked}
        header={
          <ResourceDetailHeader
            kind="ConfigMap"
            name={detail.name}
            namespace={detail.namespace}
            ageSeconds={detail.ageSeconds}
            backTo="/configuration/configmaps"
            facts={
              <>
                <span>
                  {zh
                    ? `数据键 ${Object.keys(detail.data ?? {}).length + Object.keys(detail.binaryData ?? {}).length}`
                    : `${Object.keys(detail.data ?? {}).length + Object.keys(detail.binaryData ?? {}).length} keys`}
                </span>
                <span>
                  {detail.immutable ? (zh ? '不可变' : 'Immutable') : zh ? '可修改' : 'Mutable'}
                </span>
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={[
                    query,
                    actionQuery,
                    ...(active === 'yaml' ? [{ ...yamlObserver, isEnabled: true }] : []),
                  ]}
                  resourceLabel="ConfigMap"
                  onRefresh={() =>
                    Promise.all([
                      client.invalidateQueries({ queryKey: options.queryKey }),
                      client.invalidateQueries({ queryKey: listOptions.queryKey }),
                    ])
                  }
                  beforeRefresh={
                    canEdit ? (
                      <Button disabled={blocked} onClick={edit}>
                        {zh ? '编辑数据' : 'Edit data'}
                      </Button>
                    ) : undefined
                  }
                />
                {canDelete && (
                  <Button
                    danger
                    disabled={blocked}
                    onClick={() => actions.selectAction(detail, 'delete')}
                  >
                    {zh ? '删除' : 'Delete'}
                  </Button>
                )}
              </>
            }
          />
        }
        overview={
          <ConfigMapOverview detail={detail} clusterId={clusterId} onData={() => setTab('data')} />
        }
        relatedResources={
          <WorkloadRelationships
            kind="ConfigMap"
            name={name}
            scope={scope}
            active={active === 'relationships'}
          />
        }
        dataTab={
          <ConfigMapDataTab
            editRequest={editRequest}
            canEdit={canEdit}
            applying={update.isPending}
            blocked={blocked}
            detail={detail}
            onApply={(data) =>
              update.mutateAsync({ target, payload: { data, binaryData: detail.binaryData ?? {} } })
            }
          />
        }
      />
    </div>
  )
}
