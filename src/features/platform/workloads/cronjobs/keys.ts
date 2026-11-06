import type { ScopeKey } from '@/types'
import { workloadKeys } from '../shared/keys'

export const cronJobKeys = {
  configuration: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('cronjobs', scope, name), 'configuration'] as const,
}
