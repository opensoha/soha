import { Button } from 'antd'
import { useNavigate } from '@umijs/max'
import { OverviewPage } from '@/features/platform/overview-page'
import { PlatformWorkspaceShell } from './platform/workspace-shell'

export default function OverviewRoutePage() {
  const navigate = useNavigate()

  return (
    <PlatformWorkspaceShell
      title="Platform Overview"
      description="Start from fleet health, alert pressure, and pod runtime posture before drilling into platform workspaces."
      currentPath="/"
      tabs={[{ key: 'overview', label: 'Overview', path: '/' }]}
      badge="Entry workspace"
      metrics={[
        { label: 'Fleet view', value: 'Clusters, health, alert pressure' },
        { label: 'Runtime focus', value: 'Pod posture in current scope' },
        { label: 'Next hops', value: 'Pods, clusters, observability' },
      ]}
      actions={<Button type="primary" onClick={() => navigate('/workloads/pods')}>Open Pods</Button>}
    >
      <OverviewPage />
    </PlatformWorkspaceShell>
  )
}
