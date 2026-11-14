import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import { listAlertIntegrations } from './api'

export const observabilityIntegrationQueries = {
  list: () =>
    queryOptions({
      queryKey: observabilityKeys.integrations.list(),
      queryFn: listAlertIntegrations,
    }),
}
