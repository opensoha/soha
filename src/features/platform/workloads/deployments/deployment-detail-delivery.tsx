import type { ScopeKey } from '@/types'
import { WorkloadDetailDelivery } from '../shared/workload-detail-delivery'
export function DeploymentDetailDelivery(props: {
  scope: ScopeKey
  name: string
  refreshToken?: number
}) {
  return <WorkloadDetailDelivery {...props} kind="Deployment" />
}
