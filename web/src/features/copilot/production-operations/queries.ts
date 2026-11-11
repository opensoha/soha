import { queryOptions } from '@tanstack/react-query'
import { aiProductionOperationsApi as api } from './api'
import { aiProductionOperationsKeys as k } from './keys'
export const aiProductionOperationsQueries = {
  snapshots: () =>
    queryOptions({ queryKey: k.snapshots(), queryFn: api.snapshots, refetchInterval: 15_000 }),
  evidence: () => queryOptions({ queryKey: k.evidence(), queryFn: api.evidence }),
}
