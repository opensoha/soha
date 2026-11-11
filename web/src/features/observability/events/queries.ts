import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import { listObservabilityEvents } from './api'

export const observabilityEventQueries = {
  list: () =>
    queryOptions({
      queryKey: observabilityKeys.events.list(),
      queryFn: listObservabilityEvents,
    }),
}
