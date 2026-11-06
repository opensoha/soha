import type { ScopeKey } from '@/types'
import { getConfigurationDetail } from '../shared/api'
import { configurationQueries } from '../shared/queries'
import type { HorizontalPodAutoscalerResource } from './types'
export function hpaDetailQuery(scope: ScopeKey, name: string) {
  return {
    ...configurationQueries.detail<HorizontalPodAutoscalerResource>('hpas', scope, name),
    queryFn: async () => {
      const detail = await getConfigurationDetail<HorizontalPodAutoscalerResource>('hpas', {
        scope,
        name,
      })
      if (detail.name !== name || detail.namespace !== scope.namespace)
        throw new Error('HPA identity does not match the requested resource')
      return detail
    },
  }
}
