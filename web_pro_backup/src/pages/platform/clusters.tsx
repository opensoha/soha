import { Button } from 'antd'
import { useLocation, useNavigate } from '@umijs/max'
import { ClusterDetailPage, ClustersPage } from '@/features/platform/clusters-page'
import { PlatformWorkspaceShell } from './workspace-shell'

export default function PlatformClustersPage() {
  const location = useLocation()
  const navigate = useNavigate()

  if (location.pathname.startsWith('/clusters/')) {
    return <ClusterDetailPage />
  }

  return (
    <PlatformWorkspaceShell
      title="Clusters"
      description="Manage cluster registration, connection mode, health posture, and detail handoff into node operations."
      currentPath={location.pathname}
      tabs={[{ key: 'clusters', label: 'Cluster Inventory', path: '/clusters' }]}
      badge="Registration surface"
      metrics={[
        { label: 'Modes', value: 'direct_kubeconfig and agent' },
        { label: 'Drill-down', value: 'Cluster detail and node handoff' },
        { label: 'Primary actions', value: 'Create, edit, delete, inspect' },
      ]}
      actions={<Button type="primary" onClick={() => navigate('/clusters')}>Cluster Inventory</Button>}
    >
      <ClustersPage />
    </PlatformWorkspaceShell>
  )
}
