import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import { observabilityHealingApi } from './api'

export const observabilityHealingQueries = {
  policies: () =>
    queryOptions({
      queryKey: observabilityKeys.healing.policies(),
      queryFn: observabilityHealingApi.listPolicies,
    }),
  runs: () =>
    queryOptions({
      queryKey: observabilityKeys.healing.runs(),
      queryFn: observabilityHealingApi.listRuns,
    }),
  recentRuns: (limit: number) =>
    queryOptions({
      queryKey: observabilityKeys.healing.recentRuns(limit),
      queryFn: () => observabilityHealingApi.listRecentRuns(limit),
    }),
}
