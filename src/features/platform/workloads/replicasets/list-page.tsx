import { useNavigate } from 'react-router-dom'
import type { TableColumnsType } from 'antd'
import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { buildWorkloadDetailPath } from '@/features/platform/workloads-model'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { renderWorkloadNameLink } from '../shared/list-controls'
import { ReplicaControllerListPage } from '../shared/replica-controller-list'
import type { ReplicaSet } from './types'
import '@/features/platform/workloads/styles.css'

export function WorkloadsReplicaSetsPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const clusterId = usePlatformScopeStore((state) => state.clusterId)
  const columns: TableColumnsType<ReplicaSet> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      ellipsis: { showTitle: false },
      render: (name: string, record) =>
        renderWorkloadNameLink(name, () =>
          navigate(buildWorkloadDetailPath('replicasets', name, null, record.namespace, clusterId)),
        ),
      width: 240,
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 160,
    },
    {
      title: localeCode === 'zh_CN' ? '就绪 / 期望' : 'Ready / Desired',
      dataIndex: 'readyReplicas',
      width: 112,
      render: (_: number, record) => `${record.readyReplicas}/${record.desiredReplicas}`,
    },
    {
      title: localeCode === 'zh_CN' ? '可用' : 'Available',
      dataIndex: 'availableReplicas',
      width: 110,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 120,
      render: (value: number) => formatAgeSeconds(value),
    },
  ]

  return <ReplicaControllerListPage columns={columns} kind="replicasets" label="ReplicaSets" />
}
