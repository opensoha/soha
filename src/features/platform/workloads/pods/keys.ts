import type { ScopeKey } from '@/types'
import { workloadKeys } from '../shared/keys'

export const podKeys = {
  containerConfiguration: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('pods', scope, name), 'container-configuration'] as const,
}
