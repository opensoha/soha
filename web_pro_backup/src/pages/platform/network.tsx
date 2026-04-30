import { Button, Space } from 'antd'
import { useLocation, useNavigate } from '@umijs/max'
import { NetworkTopologyPage } from '@/features/platform/network-topology-page'
import {
  NetworkGatewaysPage,
  NetworkIngressesPage,
  NetworkServicesPage,
  ServiceDetailPage,
} from '@/features/platform/network-storage-pages'
import {
  NetworkEndpointSlicesPage,
  NetworkIngressClassesPage,
  NetworkPoliciesPage,
  NetworkPortForwardPage,
} from '@/features/platform/platform-management-pages'
import { PlatformWorkspaceShell } from './workspace-shell'

const tabs = [
  { key: 'topology', label: 'Topology', path: '/network/topology' },
  { key: 'services', label: 'Services', path: '/network/services' },
  { key: 'ingresses', label: 'Ingresses', path: '/network/ingresses' },
  { key: 'gateways', label: 'Gateways', path: '/network/gateways' },
  { key: 'policies', label: 'Policies', path: '/network/networkpolicies' },
]

function renderContent(pathname: string) {
  if (pathname.startsWith('/network/services/')) return <ServiceDetailPage embedded />
  if (pathname.startsWith('/network/services')) return <NetworkServicesPage />
  if (pathname.startsWith('/network/ingresses')) return <NetworkIngressesPage />
  if (pathname.startsWith('/network/gateways')) return <NetworkGatewaysPage />
  if (pathname.startsWith('/network/endpointslices')) return <NetworkEndpointSlicesPage />
  if (pathname.startsWith('/network/ingressclasses')) return <NetworkIngressClassesPage />
  if (pathname.startsWith('/network/networkpolicies')) return <NetworkPoliciesPage />
  if (pathname.startsWith('/network/port-forward')) return <NetworkPortForwardPage />
  return <NetworkTopologyPage />
}

export default function PlatformNetworkPage() {
  const { pathname } = useLocation()
  const navigate = useNavigate()

  return (
    <PlatformWorkspaceShell
      title="Network"
      description="Track entry paths, service exposure, and policy-aware traffic surfaces through a Pro-native operations workspace."
      currentPath={pathname}
      tabs={tabs}
      badge="Topology first"
      metrics={[
        { label: 'Landing page', value: 'Layered topology graph' },
        { label: 'Primary drill-down', value: 'Services and ingress routes' },
        { label: 'Preview mode', value: 'Live data with explicit demo fallback' },
      ]}
      actions={(
        <Space>
          <Button onClick={() => navigate('/network/topology')}>Topology</Button>
          <Button type="primary" onClick={() => navigate('/network/services')}>Services</Button>
        </Space>
      )}
    >
      {renderContent(pathname)}
    </PlatformWorkspaceShell>
  )
}
