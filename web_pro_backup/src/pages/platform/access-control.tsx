import { Button, Space } from 'antd'
import { useLocation, useNavigate } from '@umijs/max'
import {
  PlatformAccessControlClusterRoleBindingsPage,
  PlatformAccessControlClusterRolesPage,
  PlatformAccessControlRoleBindingsPage,
  PlatformAccessControlRolesPage,
  PlatformAccessControlServiceAccountsPage,
} from '@/features/platform/platform-management-pages'
import { PlatformWorkspaceShell } from './workspace-shell'

const tabs = [
  { key: 'serviceaccounts', label: 'ServiceAccounts', path: '/platform-access-control/serviceaccounts' },
  { key: 'clusterroles', label: 'ClusterRoles', path: '/platform-access-control/clusterroles' },
  { key: 'roles', label: 'Roles', path: '/platform-access-control/roles' },
  { key: 'clusterrolebindings', label: 'ClusterRoleBindings', path: '/platform-access-control/clusterrolebindings' },
  { key: 'rolebindings', label: 'RoleBindings', path: '/platform-access-control/rolebindings' },
]

function renderContent(pathname: string) {
  if (pathname.startsWith('/platform-access-control/clusterroles')) return <PlatformAccessControlClusterRolesPage />
  if (pathname.startsWith('/platform-access-control/roles')) return <PlatformAccessControlRolesPage />
  if (pathname.startsWith('/platform-access-control/clusterrolebindings')) return <PlatformAccessControlClusterRoleBindingsPage />
  if (pathname.startsWith('/platform-access-control/rolebindings')) return <PlatformAccessControlRoleBindingsPage />
  return <PlatformAccessControlServiceAccountsPage />
}

export default function PlatformAccessControlPage() {
  const { pathname } = useLocation()
  const navigate = useNavigate()

  return (
    <PlatformWorkspaceShell
      title="Kubernetes RBAC"
      description="Inspect service identities and RBAC bindings from a dedicated platform workspace instead of burying them under extension tooling."
      currentPath={pathname}
      tabs={tabs}
      badge="Identity + policy"
      metrics={[
        { label: 'Identity surface', value: 'Service accounts' },
        { label: 'Policy surface', value: 'Cluster and namespace roles' },
        { label: 'Binding surface', value: 'RoleBindings and ClusterRoleBindings' },
      ]}
      actions={(
        <Space>
          <Button onClick={() => navigate('/platform-access-control/serviceaccounts')}>ServiceAccounts</Button>
          <Button type="primary" onClick={() => navigate('/platform-access-control/clusterroles')}>ClusterRoles</Button>
        </Space>
      )}
    >
      {renderContent(pathname)}
    </PlatformWorkspaceShell>
  )
}
