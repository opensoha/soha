import { Button, Typography } from 'antd'
import { useQueries, useQuery } from '@tanstack/react-query'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  gatewayAPIQueries,
  type GatewayDetail,
  type HTTPRoute,
  type GRPCRoute,
} from '@/features/platform'
import { toScopeKey } from '@/types'
import { deliveryQueries } from '../queries'
import type { ApplicationRuntimeWorkload } from '../types'

const { Text } = Typography

function AddressField({
  label,
  values,
  empty,
  note,
  retry,
}: {
  label: string
  values: string[]
  empty: string
  note?: string
  retry?: () => void
}) {
  const addresses = [...new Set(values.map((value) => value.trim()).filter(Boolean))]
  return (
    <div className="soha-service-address-field">
      <dt>{label}</dt>
      <dd>
        {addresses.length > 1 ? (
          <details className="soha-service-address-values">
            <summary>
              {addresses[0]} <span>+{addresses.length - 1}</span>
            </summary>
            {addresses.map((address) => (
              <Text key={address} copyable>
                {address}
              </Text>
            ))}
          </details>
        ) : addresses.length ? (
          <Text className="soha-service-address-value" copyable>
            {addresses[0]}
          </Text>
        ) : (
          <Text type="secondary">{empty}</Text>
        )}
        {note && addresses.length > 0 ? <Text type="secondary">{note}</Text> : null}
        {retry ? (
          <Button className="soha-service-address-value" size="small" type="link" onClick={retry}>
            重试地址
          </Button>
        ) : null}
      </dd>
    </div>
  )
}

export function ServiceAddresses({
  applicationId,
  workload,
}: {
  applicationId: string
  workload: ApplicationRuntimeWorkload
}) {
  const permissionQuery = usePermissionSnapshot()
  const snapshot = permissionQuery.data?.data
  const runtimeEnabled =
    hasPermission(snapshot, 'delivery.applications.view') &&
    workload.workloadKind === 'Deployment' &&
    (!workload.sourceStatus || ['linked', 'changed'].includes(workload.sourceStatus))
  // ponytail: one cached runtime read per visible workload; use a batch endpoint for large environments.
  const query = useQuery(
    deliveryQueries.workloads.runtime(
      {
        applicationId,
        applicationEnvironmentId: workload.applicationEnvironmentId,
        workloadName: workload.workloadName,
      },
      runtimeEnabled,
    ),
  )
  const relations = runtimeEnabled ? query.data?.deployment?.relatedResources : undefined
  const routes = (relations ?? []).filter(
    (resource) => resource.kind === 'HTTPRoute' || resource.kind === 'GRPCRoute',
  )
  const routePermissions = routes.map((route) =>
    hasPermission(
      snapshot,
      route.kind === 'HTTPRoute'
        ? 'platform.network.http-routes.view'
        : 'platform.network.grpc-routes.view',
    ),
  )
  const hasDeniedRoutes = routePermissions.some((allowed) => !allowed)
  const canReadGateways = hasPermission(snapshot, 'platform.network.gateways.view')
  const routeQueries = useQueries({
    queries: routes.map((route, index) => {
      const options = gatewayAPIQueries.detail<HTTPRoute | GRPCRoute>(
        route.kind === 'HTTPRoute' ? 'httproutes' : 'grpcroutes',
        toScopeKey(workload.clusterId, route.namespace || workload.namespace),
        route.name,
      )
      return { ...options, enabled: options.enabled && routePermissions[index] && canReadGateways }
    }),
  })
  const readableRouteQueries = routeQueries.filter((_, index) => routePermissions[index])
  const parents = [
    ...new Set(readableRouteQueries.flatMap((route) => route.data?.parentRefs ?? [])),
  ]
    .map((reference) => reference.split('/'))
    .filter((parts) => parts.length === 2 && parts.every(Boolean))
  const gatewayQueries = useQueries({
    queries: parents.map(([namespace, name]) => {
      const options = gatewayAPIQueries.detail<GatewayDetail>(
        'gateways',
        toScopeKey(workload.clusterId, namespace),
        name,
      )
      return { ...options, enabled: options.enabled && canReadGateways }
    }),
  })
  const gatewayError =
    readableRouteQueries.some((result) => result.isError) ||
    gatewayQueries.some((result) => result.isError)
  const gatewayLoading =
    readableRouteQueries.some((result) => result.isPending) ||
    gatewayQueries.some((result) => result.isPending)
  const isRelated = (kind: string, name: string, namespace: string) =>
    (relations ?? []).some(
      (resource) =>
        resource.kind === kind &&
        resource.name === name &&
        (resource.namespace || workload.namespace) === namespace,
    )
  const services = (query.data?.services ?? []).filter((service) =>
    isRelated('Service', service.name, service.namespace),
  )
  const ingresses = (query.data?.ingresses ?? []).filter((ingress) =>
    isRelated('Ingress', ingress.name, ingress.namespace),
  )
  const unavailable = query.isError ? '地址读取失败' : query.isLoading ? '读取中' : undefined
  if (!runtimeEnabled) return null
  return (
    <dl className="soha-service-addresses" aria-label="服务访问地址">
      <AddressField
        label="集群内 · Service"
        values={services
          .map((service) => service.clusterIp)
          .filter((address): address is string =>
            Boolean(address && address.toLowerCase() !== 'none'),
          )}
        empty={
          unavailable ||
          (!relations ? '未返回关联关系' : services.length ? '未返回 ClusterIP' : '未关联')
        }
        note={`ClusterIP · ${[...new Set(services.flatMap((service) => service.ports ?? []))].join(' · ') || '集群内'}`}
        retry={query.isError ? () => void query.refetch() : undefined}
      />
      <AddressField
        label="Ingress 入口"
        values={ingresses.flatMap((ingress) => [
          ...(ingress.hosts ?? []),
          ...(ingress.address ? ingress.address.split(',').map((value) => value.trim()) : []),
        ])}
        empty={
          unavailable ||
          (!relations ? '未返回关联关系' : ingresses.length ? '未分配地址' : '未关联')
        }
        note="域名 / 控制器地址"
      />
      <AddressField
        label="Gateway 入口"
        values={
          readableRouteQueries.length > 0 && canReadGateways
            ? gatewayQueries.flatMap((gateway) => gateway.data?.addresses ?? [])
            : []
        }
        empty={
          unavailable ||
          (!relations
            ? '未返回关联关系'
            : !routes.length
              ? '未关联'
              : !readableRouteQueries.length || !canReadGateways
                ? '无查看权限'
                : gatewayError
                  ? '地址读取失败'
                  : gatewayLoading
                    ? '读取中'
                    : !parents.length
                      ? hasDeniedRoutes
                        ? '部分路由无查看权限'
                        : '未绑定 Gateway'
                      : '未分配地址')
        }
        note={hasDeniedRoutes ? '部分路由无查看权限' : undefined}
        retry={
          gatewayError && readableRouteQueries.length > 0 && canReadGateways
            ? () => {
                readableRouteQueries.forEach((route) => void route.refetch())
                gatewayQueries.forEach((gateway) => void gateway.refetch())
              }
            : undefined
        }
      />
    </dl>
  )
}
