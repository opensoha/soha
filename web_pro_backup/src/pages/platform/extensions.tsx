import { Button, Space } from 'antd'
import { useLocation, useNavigate } from '@umijs/max'
import { CRDPage, HelmChartsPage, HelmReleasesPage } from '@/features/platform/extensions-pages'
import { PlatformWorkspaceShell } from './workspace-shell'

const tabs = [
  { key: 'crd', label: 'CRD', path: '/extensions' },
  { key: 'helm-releases', label: 'Helm Releases', path: '/helm/releases' },
  { key: 'helm-charts', label: 'Helm Charts', path: '/helm/charts' },
]

function renderContent(pathname: string) {
  if (pathname.startsWith('/helm/charts')) return <HelmChartsPage />
  if (pathname.startsWith('/helm/releases')) return <HelmReleasesPage />
  return <CRDPage />
}

export default function PlatformExtensionsPage() {
  const { pathname } = useLocation()
  const navigate = useNavigate()

  return (
    <PlatformWorkspaceShell
      title="Extensions"
      description="Operate CRD-backed resources and Helm release surfaces from one extension workspace while keeping unsupported backend paths explicit."
      currentPath={pathname}
      tabs={tabs}
      badge="CRD + Helm"
      metrics={[
        { label: 'CRD workspace', value: 'Catalog, scoped listing, YAML CRUD' },
        { label: 'Helm status', value: 'Releases live, charts placeholder when API missing' },
        { label: 'Mode support', value: 'Direct-cluster CRUD only for CRD YAML flows' },
      ]}
      actions={(
        <Space>
          <Button onClick={() => navigate('/extensions')}>CRD</Button>
          <Button type="primary" onClick={() => navigate('/helm/releases')}>Helm Releases</Button>
        </Space>
      )}
    >
      {renderContent(pathname)}
    </PlatformWorkspaceShell>
  )
}
