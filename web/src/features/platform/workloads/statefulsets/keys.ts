import type { ScopeKey } from '@/types'
import { workloadKeys } from '../shared/keys'
export const statefulSetKeys = {
  configuration: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('statefulsets', scope, name), 'configuration'] as const,
  rollouts: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('statefulsets', scope, name), 'rollouts'] as const,
  revision: (scope: ScopeKey, name: string, revisionName: string, revision: string) =>
    [
      ...workloadKeys.detail('statefulsets', scope, name),
      'revision',
      revisionName,
      revision,
    ] as const,
}
