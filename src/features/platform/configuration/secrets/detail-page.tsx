import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
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
import { useIsMutating, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useIsCurrentDetailTarget, useWorkloadDetailScope } from '../../workloads/shared/detail-scope'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { ConfigurationDetailShell } from '../shared/detail-shell'
import { configurationQueries } from '../shared/queries'
import { SecretDataTab } from './data-tab'
import { secretMutations } from './mutations'
import { SecretOverview } from './overview'
import { secretCertificateQuery, secretQueries } from './queries'
import type { SecretResource } from './types'
import { secretYAMLProperties } from './yaml-properties'
import './styles.css'

export function SecretDetailPage() {
  const { secretName = '' } = useParams()
  const { clusterId, namespace } = useWorkloadDetailScope()
  return (
    <SecretDetailContent
      key={JSON.stringify([clusterId, namespace, secretName])}
      clusterId={clusterId || ''}
      namespace={namespace || ''}
      name={secretName}
    />
  )
}

function SecretDetailContent({
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
  const client = useQueryClient()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const [tab, setTab] = useResourceDetailTab()
  const active = ['overview', 'data', 'relationships', 'yaml'].includes(tab) ? tab : 'overview'
  const [editRequest, setEditRequest] = useState(params.get('edit') === 'true' ? 1 : 0)
  const [yamlIdentityError, setYamlIdentityError] = useState(false)
  const permissionQuery = usePermissionSnapshot()
  const permissions = readableQueryData(permissionQuery)?.data
  const canRead = hasPermission(permissions, 'platform.configuration.secrets.view')
  const canReadData =
    canRead && hasPermission(permissions, 'platform.configuration.secret-data.view')
  const permissionRef = useRef(canReadData)
  permissionRef.current = canReadData
  const scope = toScopeKey(clusterId, namespace)
  const target = { scope, name }
  const contentWriteFilter = { mutationKey: secretMutations.contentKey(target), exact: true }
  const contentWritePending = useIsMutating(contentWriteFilter) > 0
  const targetIdentity = JSON.stringify([clusterId, namespace, name])
  const isCurrentTarget = useIsCurrentDetailTarget(targetIdentity)
  const refresh = useWorkloadRefresh()
  // The existing list is the metadata-only read. Never call full detail from Overview.
  const metadataOptions = secretQueries.list(scope)
  const metadataQuery = useQuery({
    ...metadataOptions,
    enabled: Boolean(clusterId && namespace && canRead),
    refetchInterval: refresh.interval,
  })
  const metadata = canRead
    ? readableQueryData(metadataQuery)?.find(
        (item) => item.name === name && item.namespace === namespace,
      )
    : undefined
  const options = secretQueries.detail(scope, name)
  const dataQuery = useQuery({
    ...options,
    enabled: Boolean(metadata && canReadData && active === 'data'),
    refetchInterval: active === 'data' ? refresh.interval : false,
  })
  const detail = canReadData ? readableQueryData(dataQuery) : undefined
  const certificateOptions = secretCertificateQuery(scope, name)
  const certificateQuery = useQuery({
    ...certificateOptions,
    enabled: Boolean(canRead && metadata?.type === 'kubernetes.io/tls' && active === 'overview'),
    refetchInterval: active === 'overview' ? refresh.interval : false,
  })
  const yamlOptions = configurationQueries.yaml('secrets', scope, name)
  const yamlObserver = useQuery({ ...yamlOptions, enabled: false, gcTime: 0 })
  const currentYAML = canReadData ? readableQueryData(yamlObserver) : undefined
  const yamlProperties = useMemo(() => {
    if (!currentYAML) return undefined
    try {
      return secretYAMLProperties(currentYAML.content, name, namespace)
    } catch {
      return undefined
    }
  }, [currentYAML, name, namespace])
  const canEditYAML =
    yamlProperties?.immutable === false && Boolean(yamlProperties.uid && yamlProperties.resourceVersion)

  const sensitiveKeys = JSON.stringify([options.queryKey, yamlOptions.queryKey])
  const purgeSensitive = useCallback(() => {
    const keys = JSON.parse(sensitiveKeys) as unknown[][]
    for (const queryKey of keys) {
      void client.cancelQueries({ queryKey, exact: true })
      client.removeQueries({ queryKey, exact: true })
    }
  }, [client, sensitiveKeys])
  useEffect(() => {
    permissionRef.current = canReadData
    if (!canReadData) purgeSensitive()
    return () => {
      permissionRef.current = false
      purgeSensitive()
    }
  }, [purgeSensitive, canReadData])
  useEffect(() => {
    if (active !== tab) setTab('overview')
  }, [active, tab, setTab])
  const update = usePlatformMutation(
    secretMutations.updateContent(client, target, () => isCurrentTarget() && permissionRef.current),
    {
      label: ['更新 Secret', 'Update Secret'],
      target: (variables) =>
        platformResourceTarget('payload' in variables ? variables.target : variables),
    },
  )
  const remove = usePlatformMutation(secretMutations.remove(client), {
    label: ['删除 Secret', 'Delete Secret'],
    target: platformResourceTarget,
  })
  const canUpdate = Boolean(
    metadata &&
    canReadData &&
    !permissionQuery.error &&
    !metadataQuery.error &&
    hasPermission(permissions, 'platform.configuration.secrets.update') &&
    hasAllowedAction(metadata.allowedActions, 'update') &&
    metadata.immutable !== true,
  )
  const canEdit = canUpdate && detail?.immutable === false
  const canDelete = Boolean(
    metadata &&
    !permissionQuery.error &&
    !metadataQuery.error &&
    hasPermission(permissions, 'platform.configuration.secrets.delete') &&
    hasAllowedAction(metadata.allowedActions, 'delete'),
  )
  const collection = '/configuration/secrets?' + new URLSearchParams({ clusterId, namespace })
  const actions = useKubernetesResourceActions<SecretResource>({
    resourceKey: 'secret-detail',
    resourceLabel: 'Secret',
    scopeKey: targetIdentity,
    records: metadata ? [metadata] : [],
    getName: (record) => record.name,
    getKey: (record) => record.namespace + '/' + record.name,
    actions: () =>
      canDelete
        ? [
            {
              key: 'delete',
              label: zh ? '删除' : 'Delete',
              danger: true,
              disabled:
                update.blocked ||
                contentWritePending ||
                remove.blocked ||
                remove.isPending ||
                Boolean(metadataQuery.error),
              confirm: kubernetesDeleteConfirmation(
                { name, namespace },
                localeCode,
                zh
                  ? '引用此 Secret 的工作负载可能无法读取凭据或证书。'
                  : 'Workloads referencing this Secret may lose access to credentials or certificates.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync(target)
                if (!isCurrentTarget()) return
                purgeSensitive()
                navigate(collection)
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
  if (!canRead || (metadataQuery.isError && !metadata))
    return <ReadFeedback canRead={canRead} query={metadataQuery} label="Secret" />
  if (metadataQuery.isPending) return <ManagementState kind="loading" />
  if (!metadata)
    return (
      <ManagementState
        kind="not-found"
        description={
          zh ? '当前范围未返回此 Secret' : 'This Secret was not returned in the current scope'
        }
      />
    )
  const blocked = contentWritePending || update.blocked || remove.blocked || metadataQuery.isFetching
  const dataBlocked = blocked || dataQuery.isFetching || Boolean(dataQuery.error)
  const canSubmit = () => {
    const current = client.getQueryState(metadataOptions.queryKey)
    return (
      canUpdate &&
      !blocked &&
      client.isMutating(contentWriteFilter) === 0 &&
      isCurrentTarget() &&
      permissionRef.current &&
      current?.fetchStatus === 'idle' &&
      !current.error &&
      !current.isInvalidated &&
      current.data === metadataQuery.data
    )
  }
  const edit = () => {
    setTab('data')
    setEditRequest((value) => value + 1)
  }
  const canOpenEdit = canUpdate && (detail?.immutable ?? metadata.immutable) === false
  return (
    <div className="soha-secret-detail">
      {actions.confirmation}
      <ReadFeedback canRead={canRead} query={metadataQuery} label="Secret" />
      {canReadData && update.feedback}
      {!actions.isConfirming && remove.feedback}
      {active === 'yaml' && canReadData && currentYAML && (!yamlProperties || yamlIdentityError) ? (
        <ManagementState
          kind="error"
          title={zh ? 'Secret YAML 无法校验' : 'Secret YAML validation failed'}
          description={
            yamlIdentityError
              ? zh
                ? '请保留当前资源的 UID 和 resourceVersion 后再应用。'
                : 'Keep the observed UID and resourceVersion before applying.'
              : zh
                ? '未能确认资源身份或不可变配置，请重新读取后再编辑。'
                : 'Resource identity or immutability could not be confirmed. Read the resource again before editing.'
          }
        />
      ) : null}
      <ConfigurationDetailShell
        kind="secrets"
        label="Secret"
        target={target}
        detail={metadata}
        activeTabKey={active}
        onTabChange={setTab}
        canReadYAML={canReadData}
        yamlWrite={{
          blocked,
          isPending: contentWritePending,
          apply: (content, onSuccess) => {
            if (!canSubmit() || !canEditYAML || !yamlProperties) return
            try {
              const draft = secretYAMLProperties(content, name, namespace)
              if (
                draft.uid !== yamlProperties.uid ||
                draft.resourceVersion !== yamlProperties.resourceVersion
              ) {
                setYamlIdentityError(true)
                return
              }
            } catch {
              setYamlIdentityError(true)
              return
            }
            setYamlIdentityError(false)
            update.mutate(
              { ...target, content, requireObservedIdentity: true },
              {
                onSuccess: (result) => {
                  if (result.yaml && isCurrentTarget() && permissionRef.current) {
                    onSuccess(result.yaml.content)
                  }
                },
              },
            )
          },
        }}
        preserveYaml
        canUpdateYAML={canUpdate && canEditYAML}
        header={
          <ResourceDetailHeader
            kind="Secret"
            name={name}
            namespace={namespace}
            ageSeconds={metadata.ageSeconds}
            backTo="/configuration/secrets"
            facts={
              <>
                <MetadataTag label={metadata.type || 'Opaque'} />
                <span>
                  {zh ? '数据键 ' + metadata.dataEntries : metadata.dataEntries + ' keys'}
                </span>
                {metadata.immutable === true ? (
                  <MetadataTag label={zh ? '不可变' : 'Immutable'} />
                ) : null}
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={[
                    metadataQuery,
                    ...(active === 'data'
                      ? [dataQuery]
                      : active === 'yaml' && canReadData
                        ? [{ ...yamlObserver, isEnabled: true }]
                        : active === 'overview' && metadata.type === 'kubernetes.io/tls'
                          ? [certificateQuery]
                          : []),
                  ]}
                  resourceLabel="Secret"
                  onRefresh={async () => {
                    await client.invalidateQueries({ queryKey: metadataOptions.queryKey })
                    await client.invalidateQueries({ queryKey: options.queryKey })
                  }}
                  beforeRefresh={
                    canOpenEdit ? (
                      <Button disabled={blocked} onClick={edit}>
                        {zh ? '编辑数据' : 'Edit data'}
                      </Button>
                    ) : undefined
                  }
                />
                {canDelete ? (
                  <Button
                    danger
                    disabled={blocked || remove.isPending}
                    onClick={() => actions.selectAction(metadata, 'delete')}
                  >
                    {zh ? '删除' : 'Delete'}
                  </Button>
                ) : null}
              </>
            }
          />
        }
        overview={
          <SecretOverview
            metadata={metadata}
            detail={detail}
            canReadData={canReadData}
            onData={() => setTab('data')}
            certificateQuery={certificateQuery}
          />
        }
        relatedResources={
          <WorkloadRelationships
            kind="Secret"
            name={name}
            scope={scope}
            active={active === 'relationships'}
          />
        }
        dataTab={
          !canReadData ? (
            <ManagementState
              kind="no-permission"
              description={
                zh
                  ? '查看或编辑 Secret 内容需要敏感数据查看权限。'
                  : 'Secret data permission is required to view or edit content.'
              }
            />
          ) : (
            <>
              <ReadFeedback
                canRead={canReadData}
                query={dataQuery}
                label={zh ? 'Secret 数据' : 'Secret data'}
              />
              {dataQuery.isPending && active === 'data' ? <ManagementState kind="loading" /> : null}
              {detail ? (
                <SecretDataTab
                  active={active === 'data'}
                  editRequest={editRequest}
                  detail={detail}
                  canEdit={canEdit}
                  applying={contentWritePending}
                  blocked={dataBlocked}
                  onApply={(data) => {
                    const current = client.getQueryState(options.queryKey)
                    if (
                      !canSubmit() ||
                      !canEdit ||
                      current?.fetchStatus !== 'idle' ||
                      current.error ||
                      current.isInvalidated ||
                      current.data !== detail
                    ) {
                      throw new Error(
                        zh ? '资源记录已变化，请重新读取。' : 'The resource changed. Read it again.',
                      )
                    }
                    return update.mutateAsync({ target, payload: { data } })
                  }}
                />
              ) : null}
            </>
          )
        }
      />
    </div>
  )
}
