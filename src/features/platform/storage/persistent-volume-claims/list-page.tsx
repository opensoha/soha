import { useNavigate } from 'react-router-dom'
import type { TableColumnsType } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { compareRequestedStorage } from './pvc-sorters'
import { StatusTag } from '@/components/status-tag'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { StorageListPage } from '../shared/list-page'
import { persistentVolumeClaimMutations } from './mutations'
import { persistentVolumeClaimQueries } from './queries'
import type { PersistentVolumeClaim } from './types'

const DEFAULT_TEMPLATE = `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: example-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
`

export function StoragePvcPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canViewPV = hasPermission(permissions, 'platform.storage.persistent-volumes.view')
  const canViewStorageClass = hasPermission(permissions, 'platform.storage.storage-classes.view')
  const detailPath = (record: PersistentVolumeClaim, edit = false) => {
    const params = new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace })
    if (edit) {
      params.set('tab', 'yaml')
      params.set('edit', 'true')
    }
    return `/storage/persistentvolumeclaims/${encodeURIComponent(record.name)}?${params}`
  }
  const relatedPath = (kind: 'persistentvolumes' | 'storageclasses', name: string) =>
    `/storage/${kind}/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId: clusterId || '' })}`
  const columns: TableColumnsType<PersistentVolumeClaim> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      sorter: (a, b) => a.name.localeCompare(b.name),
      ellipsis: { showTitle: false },
      width: 260,
      render: (value: string, record) => (
        <TableCellLink label={value} onClick={() => navigate(detailPath(record))} />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 180,
      sorter: (a, b) => a.namespace.localeCompare(b.namespace),
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      sorter: (a, b) => a.status.localeCompare(b.status),
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '申请容量' : 'Requested',
      dataIndex: 'requested',
      width: 120,
      sorter: (a, b) => compareRequestedStorage(a.requested, b.requested),
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '绑定 PV' : 'Bound PV',
      dataIndex: 'volumeName',
      ellipsis: { showTitle: false },
      width: 220,
      sorter: (a, b) => (a.volumeName ?? '').localeCompare(b.volumeName ?? ''),
      render: (value?: string) =>
        value && canViewPV ? (
          <TableCellLink
            label={value}
            onClick={() => navigate(relatedPath('persistentvolumes', value))}
          />
        ) : (
          <TableCellText value={value} />
        ),
    },
    {
      title: 'StorageClass',
      dataIndex: 'storageClass',
      sorter: (a, b) => (a.storageClass ?? '').localeCompare(b.storageClass ?? ''),
      ellipsis: { showTitle: false },
      width: 180,
      render: (value?: string) =>
        value && canViewStorageClass ? (
          <TableCellLink
            label={value}
            onClick={() => navigate(relatedPath('storageclasses', value))}
          />
        ) : (
          <TableCellText value={value} />
        ),
    },
    {
      title: 'Access Modes',
      dataIndex: 'accessModes',
      sorter: (a, b) =>
        (a.accessModes ?? []).join(', ').localeCompare((b.accessModes ?? []).join(', ')),
      ellipsis: { showTitle: false },
      width: 160,
      render: (value?: string[]) => <TableCellText value={value?.join(', ')} />,
    },
    {
      title: 'Age',
      dataIndex: 'ageSeconds',
      sorter: (a, b) => a.ageSeconds - b.ageSeconds,
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  return (
    <StorageListPage
      autoRefresh
      permissionKey="platform.storage.persistent-volume-claims"
      onEdit={(record) => navigate(detailPath(record, true))}
      clusterScoped={false}
      columns={columns}
      createDefaultTemplate={DEFAULT_TEMPLATE}
      emptyLabel={{ zh_CN: '当前范围没有 PVC', en_US: 'No PVCs in the current scope' }}
      getRecordNamespace={(record) => record.namespace}
      kind="PersistentVolumeClaim"
      listQuery={persistentVolumeClaimQueries.list}
      removeOptions={persistentVolumeClaimMutations.remove}
      resourceLabel="PVC"
      rowKey={(record) => `${record.namespace}/${record.name}`}
      searchPlaceholder={{
        zh_CN: '搜索 PVC / namespace / storageClass',
        en_US: 'Search PVC / namespace / storageClass',
      }}
      searchValues={(record) => [
        record.name,
        record.namespace,
        record.status,
        record.storageClass,
        record.volumeName,
      ]}
    />
  )
}
