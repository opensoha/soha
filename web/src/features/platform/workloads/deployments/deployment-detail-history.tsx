import { lazy, Suspense } from 'react'
import { Spin } from 'antd'
import type { RolloutHistory, ScopeKey } from '@/types'
import { WorkloadRevisionHistory } from '../shared/workload-revision-history'
const Revision = lazy(async () => ({
  default: (await import('./deployment-detail-revision')).DeploymentDetailRevision,
}))
export function DeploymentDetailHistory(props: {
  scope: ScopeKey
  records: RolloutHistory[]
  currentRevision?: string
  loading: boolean
  error?: string
  canRollback: boolean
  blocked: boolean
  onRollback: (record: RolloutHistory) => void
  onRefresh: () => void
}) {
  return (
    <WorkloadRevisionHistory
      {...props}
      renderRevision={(record) => (
        <Suspense fallback={<Spin />}>
          <Revision
            key={record.name}
            record={record}
            records={props.records}
            currentRevision={props.currentRevision}
            scope={props.scope}
          />
        </Suspense>
      )}
    />
  )
}
