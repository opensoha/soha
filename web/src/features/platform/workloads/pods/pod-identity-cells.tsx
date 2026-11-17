import { memo } from 'react'
import { Space, Tag, Typography } from 'antd'
import { TableCellText } from '@/components/table-cell-content'
import {
  ResourceNamespaceCell,
  ResourceStatusCell,
} from '@/features/platform/shared/resource-table-cells'

const { Link } = Typography

export const PodNameCell = memo(function PodNameCell({
  name,
  pvcCount,
  to,
  onNavigate,
}: {
  name: string
  pvcCount: number
  to: string
  onNavigate: (path: string) => void
}) {
  return (
    <div className="soha-pod-table-name-cell">
      <Link className="soha-pod-table-name-link" onClick={() => onNavigate(to)}>
        <TableCellText value={name} />
      </Link>
      {pvcCount > 0 ? (
        <Space size={4} wrap={false} className="soha-pod-table-meta">
          <Tag variant="filled" className="soha-pod-table-pvc-tag">
            PVC {pvcCount}
          </Tag>
        </Space>
      ) : null}
    </div>
  )
})

export const PodNamespaceCell = ResourceNamespaceCell
export const PodPhaseCell = ResourceStatusCell
export const PodRestartCell = memo(function PodRestartCell({ restarts }: { restarts: number }) {
  return (
    <Tag variant="filled" color={restarts > 0 ? 'warning' : 'default'}>
      {restarts}
    </Tag>
  )
})
