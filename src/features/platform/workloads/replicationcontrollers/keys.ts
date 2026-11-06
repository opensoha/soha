import type { ScopeKey } from '@/types'
import { workloadKeys } from '../shared/keys'
export const replicationControllerKeys = {
  configuration: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('replicationcontrollers', scope, name), 'configuration'] as const,
}
