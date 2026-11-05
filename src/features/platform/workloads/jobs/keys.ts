import type { ScopeKey } from '@/types'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'

export const jobKeys = {
  template: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('jobs', scope, name), 'template'] as const,
}
