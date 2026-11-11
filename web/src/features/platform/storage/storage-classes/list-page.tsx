import { useNavigate } from 'react-router-dom'
import type { TableColumnsType } from 'antd'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { BooleanTag } from '@/components/status-tag'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { StorageListPage } from '../shared/list-page'
import { storageClassMutations } from './mutations'
import { storageClassQueries } from './queries'
import type { StorageClass } from './types'

const DEFAULT_TEMPLATE = `apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: example-storage-class
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
`

export function StorageClassesPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()
  const detailPath = (name: string, edit = false) =>
    `/storage/storageclasses/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId: clusterId || '', ...(edit ? { tab: 'yaml', edit: 'true' } : {}) })}`
  const columns: TableColumnsType<StorageClass> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      sorter: (a, b) => a.name.localeCompare(b.name),
      ellipsis: { showTitle: false },
      width: 260,
      render: (value: string) => (
        <TableCellLink label={value} onClick={() => navigate(detailPath(value))} />
      ),
    },
    {
      title: 'Provisioner',
      dataIndex: 'provisioner',
      sorter: (a, b) => a.provisioner.localeCompare(b.provisioner),
      ellipsis: { showTitle: false },
      width: 320,
      render: (value: string) => <TableCellText value={value} />,
    },
    {
      title: 'Reclaim Policy',
      dataIndex: 'reclaimPolicy',
      sorter: (a, b) => (a.reclaimPolicy || '').localeCompare(b.reclaimPolicy || ''),
      width: 140,
    },
    {
      title: 'Binding Mode',
      dataIndex: 'volumeBindingMode',
      sorter: (a, b) => (a.volumeBindingMode || '').localeCompare(b.volumeBindingMode || ''),
      width: 180,
    },
    {
      title: localeCode === 'zh_CN' ? '允许扩容' : 'Expansion',
      dataIndex: 'allowVolumeExpansion',
      sorter: (a, b) => Number(a.allowVolumeExpansion) - Number(b.allowVolumeExpansion),
      width: 110,
      render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" />,
    },
    {
      title: 'Age',
      dataIndex: 'ageSeconds',
      sorter: (a, b) => a.ageSeconds - b.ageSeconds,
      width: 104,
      render: formatAgeSeconds,
    },
  ]
  return (
    <StorageListPage
      autoRefresh
      permissionKey="platform.storage.storage-classes"
      onEdit={(record) => navigate(detailPath(record.name, true))}
      clusterScoped
      columns={columns}
      createDefaultTemplate={DEFAULT_TEMPLATE}
      emptyLabel={{
        zh_CN: '当前集群没有 StorageClass',
        en_US: 'No storage classes in this cluster',
      }}
      kind="StorageClass"
      listQuery={storageClassQueries.list}
      removeOptions={storageClassMutations.remove}
      resourceLabel="StorageClass"
      rowKey="name"
      searchPlaceholder={{
        zh_CN: '搜索 StorageClass / provisioner',
        en_US: 'Search StorageClass / provisioner',
      }}
      searchValues={(record) => [
        record.name,
        record.provisioner,
        record.reclaimPolicy,
        record.volumeBindingMode,
      ]}
    />
  )
}
