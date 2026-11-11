import type { LogQuery } from '@opensoha/contracts/gen/ts/sohaapi'
import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import { listLogDataSources, logTargetKey, queryLogs, type LogTarget } from './api'

export const observabilityLogQueries = {
  dataSources: () =>
    queryOptions({
      queryKey: observabilityKeys.logs.dataSources(),
      queryFn: listLogDataSources,
    }),
  snapshot: (target: LogTarget, query: LogQuery, enabled = true) =>
    queryOptions({
      queryKey: observabilityKeys.logs.snapshot(logTargetKey(target), query),
      queryFn: ({ signal }) => queryLogs(target, query, signal),
      enabled,
      retry: false,
    }),
}
