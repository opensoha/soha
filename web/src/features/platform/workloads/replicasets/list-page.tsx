import {
  ResourceNameLink,
  ResourceStatusCell,
} from '@/features/platform/shared/resource-table-cells'
import { replicaSetHealth } from './replicaset-detail-model'
import { useNavigate } from 'react-router-dom'
import type { TableColumnsType } from 'antd'
import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { buildWorkloadDetailPath } from '@/features/platform/workloads-model'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
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
      render: (name: string, record) => (
        <ResourceNameLink
          name={name}
          to={buildWorkloadDetailPath('replicasets', name, null, record.namespace, clusterId)}
          onNavigate={navigate}
        />
      ),

      width: 240,
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 160,
    },
    {
      ...tableColumnPresets.status,
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      key: 'health',
      width: 150,
      sorter: (a, b) => replicaSetHealth(a).localeCompare(replicaSetHealth(b)),
      render: (_, record) => {
        const health = replicaSetHealth(record)
        return (
          <ResourceStatusCell
            value={health}
            label={
              health === 'healthy'
                ? localeCode === 'zh_CN'
                  ? '健康'
                  : 'Healthy'
                : health === 'inactive'
                  ? localeCode === 'zh_CN'
                    ? '期望为 0'
                    : 'Zero desired'
                  : health === 'unknown'
                    ? localeCode === 'zh_CN'
                      ? '状态未知'
                      : 'Unknown'
                    : localeCode === 'zh_CN'
                      ? '未就绪或不可用'
                      : 'Not ready or available'
            }
          />
        )
      },
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
