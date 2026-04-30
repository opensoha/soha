import { Button, Space } from 'antd'
import { useLocation, useNavigate } from '@umijs/max'
import {
  StorageClassesPage,
  StoragePvPage,
  StoragePvcPage,
} from '@/features/platform/network-storage-pages'
import { PlatformWorkspaceShell } from './workspace-shell'

const tabs = [
  { key: 'pvc', label: 'PVC', path: '/storage/persistentvolumeclaims' },
  { key: 'pv', label: 'PV', path: '/storage/persistentvolumes' },
  { key: 'classes', label: 'StorageClasses', path: '/storage/storageclasses' },
]

function renderContent(pathname: string) {
  if (pathname.startsWith('/storage/persistentvolumes')) return <StoragePvPage />
  if (pathname.startsWith('/storage/storageclasses')) return <StorageClassesPage />
  return <StoragePvcPage />
}

export default function PlatformStoragePage() {
  const { pathname } = useLocation()
  const navigate = useNavigate()

  return (
    <PlatformWorkspaceShell
      title="Storage"
      description="Review claims, backing volumes, and provisioner policy without leaving the shared platform scope."
      currentPath={pathname}
      tabs={tabs}
      badge="Namespace + cluster"
      metrics={[
        { label: 'Focus', value: 'Claims, volumes, classes' },
        { label: 'Scope behavior', value: 'PV is cluster-scoped, PVC follows namespace' },
        { label: 'Operator flow', value: 'Inspect capacity, bindings, reclaim policy' },
      ]}
      actions={(
        <Space>
          <Button onClick={() => navigate('/storage/persistentvolumeclaims')}>PVC</Button>
          <Button type="primary" onClick={() => navigate('/storage/persistentvolumes')}>PV</Button>
        </Space>
      )}
    >
      {renderContent(pathname)}
    </PlatformWorkspaceShell>
  )
}
