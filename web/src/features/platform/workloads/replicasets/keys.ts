import type { ScopeKey } from '@/types'
import { workloadKeys } from '../shared/keys'
export const replicaSetKeys = {
  configuration: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('replicasets', scope, name), 'configuration'] as const,
}
