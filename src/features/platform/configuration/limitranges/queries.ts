import type { ScopeKey } from '@/types'
import { getConfigurationDetail } from '../shared/api'
import { configurationQueries } from '../shared/queries'
import type { LimitRangeResource } from './types'

export function limitRangeDetailQuery(scope: ScopeKey, name: string) {
  return {
    ...configurationQueries.detail<LimitRangeResource>('limitranges', scope, name),
    queryFn: async () => {
      const detail = await getConfigurationDetail<LimitRangeResource>('limitranges', {
        scope,
        name,
      })
      if (detail.name !== name || detail.namespace !== scope.namespace)
        throw new Error('LimitRange identity does not match the requested resource')
      return detail
    },
  }
}
