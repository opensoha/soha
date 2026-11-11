import { useNavigate } from 'react-router-dom'
import type { TableColumnsType } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { StatusTag } from '@/components/status-tag'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { StorageListPage } from '../shared/list-page'
import { persistentVolumeMutations } from './mutations'
import { persistentVolumeQueries } from './queries'
import type { PersistentVolume } from './types'
import { compareCapacity } from './pv-sorters'

function claimIdentity(record: PersistentVolume) {
  // Explicit identity takes precedence. Never fill a partial identity from another source.
  const parts =
    record.claimNamespace !== undefined || record.claimName !== undefined
      ? [record.claimNamespace, record.claimName]
      : record.claimRef?.split('/')
  if (!parts || parts.length !== 2) return null
  const [namespace, name] = parts
  const label = /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/
  if (!namespace || namespace.length > 63 || !label.test(namespace)) return null
  if (
    !name ||
    name.length > 253 ||
    !name.split('.').every((part) => part.length <= 63 && label.test(part))
  ) {
    return null
  }
  return { namespace, name }
}

function claimLabel(record: PersistentVolume) {
  const identity = claimIdentity(record)
  return identity ? `${identity.namespace}/${identity.name}` : record.claimRef
}

const DEFAULT_TEMPLATE = `apiVersion: v1
kind: PersistentVolume
metadata:
  name: example-pv
spec:
  capacity:
    storage: 10Gi
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  hostPath:
    path: /data/example-pv
`

export function StoragePvPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canViewPVC = hasPermission(permissions, 'platform.storage.persistent-volume-claims.view')
  const canViewStorageClass = hasPermission(permissions, 'platform.storage.storage-classes.view')
  const detailPath = (record: PersistentVolume, edit = false) => {
    const params = new URLSearchParams({ clusterId: clusterId || '' })
    if (edit) {
      params.set('tab', 'yaml')
      params.set('edit', 'true')
    }
    return `/storage/persistentvolumes/${encodeURIComponent(record.name)}?${params}`
  }
  const storageClassPath = (name: string) =>
    `/storage/storageclasses/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId: clusterId || '' })}`
  const columns: TableColumnsType<PersistentVolume> = [
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
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      sorter: (a, b) => a.status.localeCompare(b.status),
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '容量' : 'Capacity',
      dataIndex: 'capacity',
      sorter: (a, b) => compareCapacity(a.capacity, b.capacity),
      width: 120,
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '申领引用 (ClaimRef)' : 'ClaimRef',
      dataIndex: 'claimRef',
      ellipsis: { showTitle: false },
      width: 260,
      sorter: (a, b) => (claimLabel(a) ?? '').localeCompare(claimLabel(b) ?? ''),
      render: (_value: string | undefined, record) => {
        const identity = claimIdentity(record)
        const value = claimLabel(record)
        return identity && canViewPVC ? (
          <TableCellLink
            label={`${identity.namespace}/${identity.name}`}
            onClick={() => {
              const params = new URLSearchParams({
                clusterId: clusterId || '',
                namespace: identity.namespace,
              })
              navigate(
                `/storage/persistentvolumeclaims/${encodeURIComponent(identity.name)}?${params}`,
              )
            }}
          />
        ) : (
          <TableCellText value={value} />
        )
      },
    },
    {
      title: 'StorageClass',
      dataIndex: 'storageClass',
      ellipsis: { showTitle: false },
      width: 180,
      sorter: (a, b) => (a.storageClass ?? '').localeCompare(b.storageClass ?? ''),
      render: (value?: string) =>
        value && canViewStorageClass ? (
          <TableCellLink label={value} onClick={() => navigate(storageClassPath(value))} />
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
      title: 'Reclaim Policy',
      dataIndex: 'reclaimPolicy',
      width: 150,
      ellipsis: { showTitle: false },
      sorter: (a, b) => (a.reclaimPolicy ?? '').localeCompare(b.reclaimPolicy ?? ''),
      render: (value?: string) => <TableCellText value={value} />,
    },
    {
      title: 'VolumeMode',
      dataIndex: 'volumeMode',
      width: 140,
      sorter: (a, b) => (a.volumeMode ?? '').localeCompare(b.volumeMode ?? ''),
      render: (value?: string) => <TableCellText value={value} />,
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
      permissionKey="platform.storage.persistent-volumes"
      onEdit={(record) => navigate(detailPath(record, true))}
      clusterScoped
      columns={columns}
      createDefaultTemplate={DEFAULT_TEMPLATE}
      emptyLabel={{ zh_CN: '当前集群没有 PV', en_US: 'No PVs in this cluster' }}
      kind="PersistentVolume"
      listQuery={persistentVolumeQueries.list}
      removeOptions={persistentVolumeMutations.remove}
      resourceLabel="PV"
      rowKey="name"
      searchPlaceholder={{
        zh_CN: '搜索 PV / claim / storageClass',
        en_US: 'Search PV / claim / storageClass',
      }}
      searchValues={(record) => [
        record.name,
        record.status,
        record.storageClass,
        claimLabel(record),
        record.accessModes?.join(', '),
        record.capacity,
        record.reclaimPolicy,
        record.volumeMode,
      ]}
    />
  )
}
