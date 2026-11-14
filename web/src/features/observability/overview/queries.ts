import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import { getMonitoringSummary } from './api'

export const observabilityOverviewQueries = {
  summary: () =>
    queryOptions({
      queryKey: observabilityKeys.monitoring.summary(),
      queryFn: getMonitoringSummary,
    }),
}
