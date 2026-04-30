import { Button, Space } from 'antd'
import { useLocation, useNavigate } from '@umijs/max'
import { ClusterNamespacesPage, ClusterNodesPage } from '@/features/platform/cluster-resources-pages'
import { NodeDetailPage } from '@/features/platform/node-detail-page'
import { PlatformWorkspaceShell } from './workspace-shell'

const tabs = [
  { key: 'nodes', label: 'Nodes', path: '/cluster-resources/nodes' },
  { key: 'namespaces', label: 'Namespaces', path: '/cluster-resources/namespaces' },
]

function renderContent(pathname: string) {
  if (pathname.startsWith('/cluster-resources/nodes/')) return <NodeDetailPage />
  if (pathname.startsWith('/cluster-resources/namespaces')) return <ClusterNamespacesPage />
  return <ClusterNodesPage />
}

export default function PlatformClusterResourcesPage() {
  const location = useLocation()
  const navigate = useNavigate()

  if (location.pathname.startsWith('/cluster-resources/nodes/')) {
    return renderContent(location.pathname)
  }

  return (
    <PlatformWorkspaceShell
      title="Cluster Resources"
      description="Work through node operations and namespace inventory with cluster-scoped navigation that stays aligned with platform scope."
      currentPath={location.pathname}
      tabs={tabs}
      badge="Cluster-scoped"
      metrics={[
        { label: 'Primary objects', value: 'Nodes and namespaces' },
        { label: 'Detail mode', value: 'Node workspace for labels, taints, YAML' },
        { label: 'Scope semantics', value: 'Cluster selection required, namespace optional' },
      ]}
      actions={(
        <Space>
          <Button onClick={() => navigate('/cluster-resources/nodes')}>Nodes</Button>
          <Button type="primary" onClick={() => navigate('/cluster-resources/namespaces')}>Namespaces</Button>
        </Space>
      )}
    >
      {renderContent(location.pathname)}
    </PlatformWorkspaceShell>
  )
}
