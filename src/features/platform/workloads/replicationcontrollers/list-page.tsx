import { useNavigate } from 'react-router-dom'
import type { TableColumnsType } from 'antd'
import { StatusTag } from '@/components/status-tag'
import { replicaControllerHealth } from '../shared/replica-controller-model'
import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { buildWorkloadDetailPath } from '@/features/platform/workloads-model'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { renderWorkloadNameLink } from '../shared/list-controls'
import { ReplicaControllerListPage } from '../shared/replica-controller-list'
import type { ReplicationController } from './types'
import '@/features/platform/workloads/styles.css'

export function WorkloadsReplicationControllersPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const clusterId = usePlatformScopeStore((state) => state.clusterId)
  const columns: TableColumnsType<ReplicationController> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      ellipsis: { showTitle: false },
      render: (name: string, record) =>
        renderWorkloadNameLink(name, () =>
          navigate(
            buildWorkloadDetailPath(
              'replicationcontrollers',
              name,
              null,
              record.namespace,
              clusterId,
            ),
          ),
        ),
      width: 260,
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 160,
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      ...tableColumnPresets.status,
      key: 'health',
      sorter: (a, b) =>
        replicaControllerHealth(a, undefined, true).localeCompare(
          replicaControllerHealth(b, undefined, true),
        ),
      width: 150,
      render: (_: unknown, record) => {
        const health = replicaControllerHealth(record, undefined, true)
        return (
          <StatusTag
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
                      ? '副本不足'
                      : 'Replicas unavailable'
            }
          />
        )
      },
    },
    {
      title: localeCode === 'zh_CN' ? '就绪 / 期望' : 'Ready / Desired',
      dataIndex: 'readyReplicas',
      width: 112,
      render: (_: number, record) =>
        `${record.readyReplicas ?? '—'}/${record.desiredReplicas ?? '—'}`,
    },
    {
      title: localeCode === 'zh_CN' ? '当前' : 'Current',
      dataIndex: 'currentReplicas',
      render: (v?: number) => v ?? '—',
      width: 96,
    },
    {
      title: localeCode === 'zh_CN' ? '可用' : 'Available',
      dataIndex: 'availableReplicas',
      render: (v?: number) => v ?? '—',
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

  return (
    <ReplicaControllerListPage
      columns={columns}
      kind="replicationcontrollers"
      label="ReplicationControllers"
    />
  )
}
