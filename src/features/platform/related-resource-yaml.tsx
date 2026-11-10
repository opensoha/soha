import { useMemo } from 'react'
import { parseDocument } from 'yaml'
import { useQueries } from '@tanstack/react-query'
import { Button } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { ManagementState } from '@/components/management-list'
import { toScopeKey } from '@/types'
import { workloadQueries } from './workloads/shared/queries'
import { configurationQueries } from './configuration/shared/queries'
import { networkQueries } from './network/shared/queries'
import { persistentVolumeClaimQueries } from './storage/persistent-volume-claims/queries'
import { accessControlQueries } from './access-control/shared/queries'

type Target = { kind: string; name: string; namespace: string; clusterId: string }

function resourceQuery(target: Target) {
  const scope = toScopeKey(target.clusterId, target.namespace)
  const workloadKinds = {
    Deployment: ['deployments', 'platform.deployment.view'],
    Pod: ['pods', 'platform.pods.view'],
    ReplicaSet: ['replicasets', 'platform.workloads.replica-sets.view'],
    ReplicationController: [
      'replicationcontrollers',
      'platform.workloads.replication-controllers.view',
    ],
    StatefulSet: ['statefulsets', 'platform.workloads.stateful-sets.view'],
    DaemonSet: ['daemonsets', 'platform.workloads.daemon-sets.view'],
    Job: ['jobs', 'platform.workloads.jobs.view'],
    CronJob: ['cronjobs', 'platform.workloads.cron-jobs.view'],
  } as const
  const workload = workloadKinds[target.kind as keyof typeof workloadKinds]
  if (workload)
    return {
      permission: workload[1],
      options: workloadQueries.yaml(workload[0], scope, target.name),
    }
  const networkKinds = {
    Service: ['services', 'platform.network.services.view'],
    Ingress: ['ingresses', 'platform.network.ingresses.view'],
    Gateway: ['gateways', 'platform.network.gateways.view'],
    HTTPRoute: ['httproutes', 'platform.network.http-routes.view'],
    GRPCRoute: ['grpcroutes', 'platform.network.grpc-routes.view'],
  } as const
  const network = networkKinds[target.kind as keyof typeof networkKinds]
  if (network)
    return { permission: network[1], options: networkQueries.yaml(network[0], scope, target.name) }
  if (target.kind === 'ConfigMap' || target.kind === 'Secret') {
    const kind = target.kind === 'ConfigMap' ? 'configmaps' : 'secrets'
    return {
      permission:
        target.kind === 'ConfigMap'
          ? 'platform.configuration.config-maps.view'
          : 'platform.configuration.secrets.view',
      options: configurationQueries.yaml(kind, scope, target.name),
    }
  }
  if (target.kind === 'PersistentVolumeClaim')
    return {
      permission: 'platform.storage.persistent-volume-claims.view',
      options: persistentVolumeClaimQueries.yaml(scope, target.name),
    }
  if (target.kind === 'ServiceAccount')
    return {
      permission: 'platform.access-control.service-accounts.view',
      options: accessControlQueries.yaml('serviceaccounts', scope, target.name),
    }
}

export function RelatedResourceYaml({
  target,
  expectedUid,
  fresh = false,
}: {
  target: Target
  expectedUid?: string
  fresh?: boolean
}) {
  const permissions = usePermissionSnapshot().data?.data
  const source = resourceQuery(target)
  if (!source)
    return <ManagementState compact kind="not-configured" title="该资源类型暂不支持 YAML 预览" />
  if (!hasPermission(permissions, source.permission))
    return <ManagementState compact kind="no-permission" />
  return (
    <YamlQuery
      key={`${target.clusterId}/${target.namespace}/${target.kind}/${target.name}/${expectedUid || ''}/${fresh ? 'fresh' : 'cached'}`}
      options={source.options}
      target={target}
      expectedUid={expectedUid}
      fresh={fresh}
    />
  )
}

function YamlQuery({
  options,
  target,
  expectedUid,
  fresh,
}: {
  target: Target
  expectedUid?: string
  fresh: boolean
  options: NonNullable<ReturnType<typeof resourceQuery>>['options']
}) {
  const [query] = useQueries({
    queries: [
      {
        ...options,
        ...(expectedUid || fresh
          ? { staleTime: 0, gcTime: 0, refetchOnMount: 'always' as const }
          : {}),
      },
    ],
  })
  const identityValid = useMemo(() => {
    if (!expectedUid) return true
    try {
      const document = parseDocument(query.data?.content || '')
      if (document.errors.length) return false
      const value: unknown = document.toJS({ maxAliasCount: 50 })
      if (!value || typeof value !== 'object' || !('metadata' in value)) return false
      const metadata = value.metadata
      return Boolean(
        metadata &&
        typeof metadata === 'object' &&
        'uid' in metadata &&
        metadata.uid === expectedUid &&
        'name' in metadata &&
        metadata.name === target.name &&
        'namespace' in metadata &&
        metadata.namespace === target.namespace,
      )
    } catch {
      return false
    }
  }, [expectedUid, query.data?.content, target.name, target.namespace])
  if (query.isPending || ((expectedUid || fresh) && query.isFetching))
    return <ManagementState compact kind="loading" />
  if (query.isError)
    return (
      <ManagementState
        compact
        kind="error"
        description={query.error.message}
        actions={<Button onClick={() => void query.refetch()}>重试</Button>}
      />
    )
  if (!identityValid)
    return (
      <ManagementState
        compact
        kind="error"
        title="资源已重建或身份无法确认"
        description="请刷新关系后重新选择，不展示同名新资源的配置。"
      />
    )
  return (
    <pre className="soha-resource-yaml-content" tabIndex={0} aria-label="资源 YAML">
      {query.data?.content || '未返回 YAML'}
    </pre>
  )
}
