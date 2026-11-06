import type { ScopeKey } from '@/types'
import { workloadKeys } from '../shared/keys'
export const daemonSetKeys = {
  configuration: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('daemonsets', scope, name), 'configuration'] as const,
  rollouts: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('daemonsets', scope, name), 'rollouts'] as const,
  revision: (scope: ScopeKey, name: string, revisionName: string, revision: string) =>
    [
      ...workloadKeys.detail('daemonsets', scope, name),
      'revision',
      revisionName,
      revision,
    ] as const,
}
