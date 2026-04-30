import { Button, Space } from 'antd'
import { useLocation, useNavigate } from '@umijs/max'
import {
  CronJobDetailPage,
  DaemonSetDetailPage,
  DeploymentDetailPage,
  JobDetailPage,
  PodDetailPage,
  StatefulSetDetailPage,
  WorkloadsCronJobsPage,
  WorkloadsDaemonSetsPage,
  WorkloadsDeploymentsPage,
  WorkloadsJobsPage,
  WorkloadsOverviewPage,
  WorkloadsPodsPage,
  WorkloadsStatefulSetsPage,
} from '@/features/platform/workloads-pages'
import {
  WorkloadsReplicaSetsPage,
  WorkloadsReplicationControllersPage,
} from '@/features/platform/platform-management-pages'
import { PlatformWorkspaceShell } from './workspace-shell'

const tabs = [
  { key: 'overview', label: 'Overview', path: '/workloads/overview' },
  { key: 'deployments', label: 'Deployments', path: '/workloads/deployments' },
  { key: 'pods', label: 'Pods', path: '/workloads/pods' },
  { key: 'statefulsets', label: 'StatefulSets', path: '/workloads/statefulsets' },
  { key: 'daemonsets', label: 'DaemonSets', path: '/workloads/daemonsets' },
  { key: 'jobs', label: 'Jobs', path: '/workloads/jobs' },
  { key: 'cronjobs', label: 'CronJobs', path: '/workloads/cronjobs' },
]

function isDetailPath(pathname: string) {
  return [
    '/workloads/deployments/',
    '/workloads/pods/',
    '/workloads/statefulsets/',
    '/workloads/daemonsets/',
    '/workloads/jobs/',
    '/workloads/cronjobs/',
  ].some((prefix) => pathname.startsWith(prefix))
}

function renderContent(pathname: string) {
  if (pathname.startsWith('/workloads/deployments/')) return <DeploymentDetailPage />
  if (pathname.startsWith('/workloads/pods/')) return <PodDetailPage />
  if (pathname.startsWith('/workloads/statefulsets/')) return <StatefulSetDetailPage />
  if (pathname.startsWith('/workloads/daemonsets/')) return <DaemonSetDetailPage />
  if (pathname.startsWith('/workloads/jobs/')) return <JobDetailPage />
  if (pathname.startsWith('/workloads/cronjobs/')) return <CronJobDetailPage />
  if (pathname.startsWith('/workloads/deployments')) return <WorkloadsDeploymentsPage />
  if (pathname.startsWith('/workloads/pods')) return <WorkloadsPodsPage />
  if (pathname.startsWith('/workloads/statefulsets')) return <WorkloadsStatefulSetsPage />
  if (pathname.startsWith('/workloads/daemonsets')) return <WorkloadsDaemonSetsPage />
  if (pathname.startsWith('/workloads/jobs')) return <WorkloadsJobsPage />
  if (pathname.startsWith('/workloads/cronjobs')) return <WorkloadsCronJobsPage />
  if (pathname.startsWith('/workloads/replicasets')) return <WorkloadsReplicaSetsPage />
  if (pathname.startsWith('/workloads/replicationcontrollers')) return <WorkloadsReplicationControllersPage />
  return <WorkloadsOverviewPage />
}

export default function PlatformWorkloadsPage() {
  const { pathname } = useLocation()
  const navigate = useNavigate()

  if (isDetailPath(pathname)) {
    return renderContent(pathname)
  }

  return (
    <PlatformWorkspaceShell
      title="Workloads"
      description="Operate workload inventory, runtime status, and rollout-aware drill-downs from a single platform workspace."
      currentPath={pathname}
      tabs={tabs}
      badge="Platform"
      metrics={[
        { label: 'Primary flow', value: 'Overview -> list -> detail' },
        { label: 'Scope model', value: 'Shared cluster and namespace scope' },
        { label: 'Operations', value: 'Search, batch actions, runtime drill-downs' },
      ]}
      actions={(
        <Space>
          <Button onClick={() => navigate('/workloads/overview')}>Overview</Button>
          <Button type="primary" onClick={() => navigate('/workloads/pods')}>Open Pods</Button>
        </Space>
      )}
    >
      {renderContent(pathname)}
    </PlatformWorkspaceShell>
  )
}
