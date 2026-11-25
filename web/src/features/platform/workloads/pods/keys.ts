import type { ScopeKey } from '@/types'
import { workloadKeys } from '../shared/keys'

export const podKeys = {
  containerConfiguration: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('pods', scope, name), 'container-configuration'] as const,
  metrics: (
    scope: ScopeKey,
    name: string,
    rangeMinutes: number,
    uid?: string,
    container?: string,
  ) =>
    [
      ...workloadKeys.metrics('pods', scope, name, rangeMinutes, uid),
      ...(container ? ['container', container] : []),
    ] as const,
  deliveryPreview: (queryKey: readonly unknown[]) => [...queryKey, 'delivery-preview'] as const,
}
