import type { ScopeKey } from '@/types'
import { getConfigurationDetail } from '../shared/api'
import { configurationQueries } from '../shared/queries'
import type { PodDisruptionBudgetResource } from './types'
export function pdbDetailQuery(scope: ScopeKey, name: string) {
  return {
    ...configurationQueries.detail<PodDisruptionBudgetResource>(
      'poddisruptionbudgets',
      scope,
      name,
    ),
    queryFn: async () => {
      const detail = await getConfigurationDetail<PodDisruptionBudgetResource>(
        'poddisruptionbudgets',
        { scope, name },
      )
      if (detail.name !== name || detail.namespace !== scope.namespace)
        throw new Error('PDB identity does not match the requested resource')
      return detail
    },
  }
}
