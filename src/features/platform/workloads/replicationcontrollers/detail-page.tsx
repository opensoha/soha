import { ControllerDetailPage } from '../shared/controller-detail-page'

export function ReplicationControllerDetailPage() {
  return (
    <ControllerDetailPage
      resource="replicationcontrollers"
      title="ReplicationController"
      paramKey="replicationControllerName"
    />
  )
}
