import { localeText, useI18n } from '@/i18n'
import { Navigate, useSearchParams } from 'react-router-dom'
import { Spin } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { ManagementState } from '@/components/management-list'
const routing = [
  ['/network/ingresses', 'platform.network.ingresses.view'],
  ['/network/ingressclasses', 'platform.network.ingress-classes.view'],
  ['/network/gateway-api/gatewayclasses', 'platform.network.gateway-classes.view'],
  ['/network/gateway-api/gateways', 'platform.network.gateways.view'],
  ['/network/gateway-api/httproutes', 'platform.network.http-routes.view'],
  ['/network/gateway-api/grpcroutes', 'platform.network.grpc-routes.view'],
  ['/network/gateway-api/backendtlspolicies', 'platform.network.backend-tls-policies.view'],
  ['/network/gateway-api/referencegrants', 'platform.network.reference-grants.view'],
]
const backends = [
  ['/network/services', 'platform.network.services.view'],
  ['/network/endpointslices', 'platform.network.endpoint-slices.view'],
]
function Entry({ entries }: { entries: string[][] }) {
  const { localeCode } = useI18n()
  const query = usePermissionSnapshot()
  const [params] = useSearchParams()
  const snapshot = readableQueryData(query)?.data
  const first = entries.find(([, permission]) => hasPermission(snapshot, permission))
  if (query.isLoading) return <Spin />
  return first ? (
    <Navigate replace to={`${first[0]}?${params}`} />
  ) : (
    <ManagementState
      kind="error"
      title={localeText(localeCode, '没有可查看的网络资源', 'No network resources are accessible')}
    />
  )
}
export function NetworkRoutingEntry() {
  return <Entry entries={routing} />
}
export function NetworkBackendsEntry() {
  return <Entry entries={backends} />
}
export function NetworkEntry() {
  return (
    <Entry
      entries={[
        ['/network/topology', 'platform.network.topology.view'],
        ...routing,
        ...backends,
        ['/network/networkpolicies', 'platform.network.network-policies.view'],
        ['/network/port-forward', 'platform.network.port-forwards.view'],
      ]}
    />
  )
}
