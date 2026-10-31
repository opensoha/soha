import { ControllerDetailPage } from '../shared/controller-detail-page'

export function StatefulSetDetailPage() {
  return (
    <ControllerDetailPage
      resource="statefulsets"
      title="StatefulSet"
      paramKey="statefulSetName"
      metrics
    />
  )
}
