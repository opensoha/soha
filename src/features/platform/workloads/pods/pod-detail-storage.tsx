import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { buildVolumeDetailPath, formatVolumeTypeLabel } from '@/features/platform/workloads-model'
import type { PodVolume } from '@/types'
import { Card, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { Link } from 'react-router-dom'
import { podVolumeReadOnly } from './pod-volume-mounts'

const wrappingCell = () => ({
  style: {
    whiteSpace: 'normal' as const,
    overflowWrap: 'anywhere' as const,
    wordBreak: 'break-word' as const,
  },
})

export function PodDetailStorage({
  volumes,
  namespace,
  clusterId,
  zh,
}: {
  volumes: PodVolume[]
  namespace: string | null
  clusterId: string | null
  zh: boolean
}) {
  const columns: TableColumnsType<PodVolume> = [
    { title: zh ? '卷名' : 'Volume', dataIndex: 'name', width: 180, onCell: wrappingCell },
    {
      title: zh ? '类型' : 'Type',
      dataIndex: 'type',
      width: 160,
      onCell: wrappingCell,
      render: (type: string) => <MetadataTag label={formatVolumeTypeLabel(type)} />,
    },
    {
      title: zh ? '来源' : 'Source',
      width: 230,
      onCell: wrappingCell,
      render: (_: unknown, volume: PodVolume) => {
        const path = buildVolumeDetailPath(volume, namespace, clusterId)
        const details = volume.details?.filter(
          (detail) => !volume.sourceName || !detail.endsWith(`: ${volume.sourceName}`),
        )
        return (
          <div className="soha-pod-storage-source">
            {path ? (
              <Link to={path}>{volume.sourceName}</Link>
            ) : (
              <span>{volume.sourceName || '—'}</span>
            )}
            {Boolean(details?.length) && (
              <Typography.Text type="secondary">{details!.join(' · ')}</Typography.Text>
            )}
          </div>
        )
      },
    },
    {
      title: zh ? '容器与挂载路径' : 'Container and mount path',
      width: 360,
      onCell: wrappingCell,
      render: (_: unknown, volume: PodVolume) =>
        volume.volumeMounts?.length ? (
          <div className="soha-pod-storage-mounts">
            {volume.volumeMounts.map((mount) => (
              <div key={`${mount.name}:${mount.mountPath}:${mount.subPath || ''}`}>
                <span className="soha-pod-storage-container">
                  <strong>{mount.name}</strong>
                  <Typography.Text type="secondary">
                    {podVolumeReadOnly(volume, mount.readOnly)
                      ? zh
                        ? '只读'
                        : 'Read only'
                      : zh
                        ? '读写'
                        : 'Read / write'}
                  </Typography.Text>
                </span>
                <Typography.Text copyable>{mount.mountPath}</Typography.Text>
                {mount.subPath && (
                  <Typography.Text type="secondary">subPath: {mount.subPath}</Typography.Text>
                )}
              </div>
            ))}
          </div>
        ) : (
          <Typography.Text type="secondary">{zh ? '未挂载' : 'Not mounted'}</Typography.Text>
        ),
    },
  ]
  return (
    <Card
      className="soha-detail-card soha-pod-storage-card"
      title={zh ? '存储与挂载' : 'Storage and mounts'}
      extra={
        <Typography.Text type="secondary">
          {volumes.length} {zh ? '个卷' : 'volumes'}
        </Typography.Text>
      }
    >
      <AdminTable
        className="soha-pod-volumes-table"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={volumes}
        rowKey="name"
        pagination={false}
        tableLayout="fixed"
        scroll={{ x: 930 }}
        enableColumnSelection={false}
        empty={
          <ManagementState
            bordered={false}
            compact
            title={zh ? '当前 Pod 没有关联卷' : 'No pod volumes found'}
          />
        }
      />
    </Card>
  )
}
