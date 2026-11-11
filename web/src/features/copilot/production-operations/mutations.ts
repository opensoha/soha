import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { aiProductionOperationsApi as api } from './api'
import { aiProductionOperationsKeys as k, aiProductionOperationsMutationKeys as m } from './keys'
export const aiProductionOperationsMutations = {
  start: (c: QueryClient) =>
    mutationOptions({
      mutationKey: m.start,
      mutationFn: api.start,
      onSuccess: () =>
        Promise.all([
          c.invalidateQueries({ queryKey: k.snapshots() }),
          c.invalidateQueries({ queryKey: k.evidence() }),
        ]),
    }),
}
