import { ControllerDetailPage } from '../shared/controller-detail-page'

export function ReplicaSetDetailPage() {
  return (
    <ControllerDetailPage resource="replicasets" title="ReplicaSet" paramKey="replicaSetName" />
  )
}
