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
import { useEffect, useMemo } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { ConfigurationDetailShell } from '../shared/detail-shell'
import { configurationMutations } from '../shared/mutations'
import { configurationQueries } from '../shared/queries'
import { quotaConfiguration } from './configuration'
import { QuotaOverview } from './overview'
import { quotaRows } from './quota-model'
import { resourceQuotaDetailQuery } from './queries'
import type { ResourceQuotaResource } from './types'
import './styles.css'

export function ConfigurationResourceQuotaDetailPage() {
  const { name = '' } = useParams()
  const { clusterId, namespace } = useWorkloadDetailScope()
  return (
    <QuotaDetailContent
      key={JSON.stringify([clusterId, namespace, name])}
      clusterId={clusterId || ''}
      namespace={namespace || ''}
      name={name}
    />
  )
}

function QuotaDetailContent({
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
  const canRead = hasPermission(permissions, 'platform.configuration.resource-quotas.view')
  const scope = toScopeKey(clusterId, namespace),
    target = { scope, name }
  const refresh = useWorkloadRefresh()
  const options = resourceQuotaDetailQuery(scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const detail = canRead ? readableQueryData(query) : undefined
  const yamlOptions = configurationQueries.yaml('resourcequotas', scope, name)
  const yaml = useQuery({
    ...yamlOptions,
    enabled: Boolean(detail && canRead && active === 'overview'),
    refetchInterval: active === 'overview' ? refresh.interval : false,
  })
  const yamlData = canRead ? readableQueryData(yaml) : undefined
  const configuration = useMemo<{
    value?: ReturnType<typeof quotaConfiguration>
    error?: boolean
  }>(() => {
    if (!detail || !yamlData) return {}
    try {
      return { value: quotaConfiguration(yamlData.content, detail) }
    } catch {
      return { error: true }
    }
  }, [detail, yamlData])
  const remove = usePlatformMutation(configurationMutations.remove('resourcequotas', client), {
    label: ['删除 ResourceQuota', 'Delete ResourceQuota'],
    target: platformResourceTarget,
  })
  const canEdit = Boolean(
    detail &&
    hasPermission(permissions, 'platform.configuration.resource-quotas.update') &&
    hasAllowedAction(detail.allowedActions, 'update'),
  )
  const canDelete = Boolean(
    detail &&
    hasPermission(permissions, 'platform.configuration.resource-quotas.delete') &&
    hasAllowedAction(detail.allowedActions, 'delete'),
  )
  const targetIdentity = JSON.stringify([clusterId, namespace, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const actions = useKubernetesResourceActions<ResourceQuotaResource>({
    resourceKey: 'resourcequota-detail',
    resourceLabel: 'ResourceQuota',
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
                  ? '删除将解除此对象施加的配额限制，不会删除命名空间或其中资源。'
                  : 'This removes quota restrictions, not the namespace or its resources.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync(target)
                if (!isCurrentTarget()) return
                navigate(
                  '/configuration/resourcequotas?' + new URLSearchParams({ clusterId, namespace }),
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
    return <ReadFeedback canRead={canRead} query={query} label="ResourceQuota" />
  if (query.isPending) return <ManagementState kind="loading" />
  if (!detail)
    return (
      <ManagementState
        kind="not-found"
        description={zh ? 'ResourceQuota 未找到' : 'ResourceQuota not found'}
      />
    )
  const blocked = remove.blocked || Boolean(query.error)
  return (
    <div className="soha-resourcequota-detail">
      {actions.confirmation}
      <ReadFeedback canRead={canRead} query={query} label="ResourceQuota" />
      {!actions.isConfirming && remove.feedback}
      <ConfigurationDetailShell
        kind="resourcequotas"
        label="ResourceQuota"
        target={target}
        detail={detail}
        showReferences={false}
        activeTabKey={active}
        onTabChange={setTab}
        preserveYaml
        canUpdateYAML={canEdit && !blocked}
        header={
          <ResourceDetailHeader
            kind="ResourceQuota"
            name={name}
            namespace={namespace}
            ageSeconds={detail.ageSeconds}
            backTo="/configuration/resourcequotas"
            facts={
              <span>
                {zh
                  ? quotaRows(detail, configuration.value?.hard).length + ' 项资源'
                  : quotaRows(detail, configuration.value?.hard).length + ' resources'}
              </span>
            }
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={[
                    query,
                    ...(active === 'yaml' ? [{ ...yaml, isEnabled: true }] : [yaml]),
                  ]}
                  resourceLabel="ResourceQuota"
                  onRefresh={() => client.invalidateQueries({ queryKey: options.queryKey })}
                  beforeRefresh={
                    canEdit ? (
                      <Button disabled={blocked} onClick={() => setTab('yaml')}>
                        {zh ? '编辑配额' : 'Edit quota'}
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
              label={zh ? '配额配置' : 'Quota configuration'}
            />
            {configuration.error ? (
              <Alert
                showIcon
                type="warning"
                title={
                  zh
                    ? '配额配置无法解析，暂不展示配置上限及范围选择器。'
                    : 'Quota configuration could not be parsed. Configured limits and scope selector are unavailable.'
                }
              />
            ) : null}
            <QuotaOverview detail={detail} configuration={configuration.value} />
          </>
        }
      />
    </div>
  )
}
