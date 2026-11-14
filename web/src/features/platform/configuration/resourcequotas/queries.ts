import { configurationQueries } from '../shared/queries'
import { getConfigurationDetail } from '../shared/api'
import type { ScopeKey } from '@/types'
import type { ResourceQuotaResource } from './types'

export function resourceQuotaDetailQuery(scope: ScopeKey, name: string) {
  return {
    ...configurationQueries.detail<ResourceQuotaResource>('resourcequotas', scope, name),
    queryFn: async () => {
      const detail = await getConfigurationDetail<ResourceQuotaResource>('resourcequotas', {
        scope,
        name,
      })
      if (detail.name !== name || detail.namespace !== scope.namespace)
        throw new Error('ResourceQuota identity does not match the requested resource')
      return detail
    },
  }
}
