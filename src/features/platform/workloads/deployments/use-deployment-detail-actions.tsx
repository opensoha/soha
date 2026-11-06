import type { DeploymentDetail, ScopeKey } from '@/types'
import { useWorkloadDetailActions } from '../shared/use-workload-detail-actions'
import { deploymentMutations } from './mutations'
export function useDeploymentDetailActions(
  detail: DeploymentDetail | undefined,
  scope: ScopeKey,
  currentRevision?: string,
) {
  return useWorkloadDetailActions({
    detail,
    scope,
    currentRevision,
    resource: 'deployments',
    kind: 'Deployment',
    permission: 'platform.deployment',
    mutations: deploymentMutations,
  })
}
