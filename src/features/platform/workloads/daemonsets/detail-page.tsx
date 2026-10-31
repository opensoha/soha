import { ControllerDetailPage } from '../shared/controller-detail-page'

export function DaemonSetDetailPage() {
  return (
    <ControllerDetailPage
      resource="daemonsets"
      title="DaemonSet"
      paramKey="daemonSetName"
      metrics
    />
  )
}
