import { queryOptions } from '@tanstack/react-query'
import { observeApi } from './api'
import { observeKeys } from './keys'

export const observeQueries = {
  overview: {
    sessions: () =>
      queryOptions({
        queryKey: observeKeys.overview.sessions(),
        queryFn: observeApi.overview.sessions,
      }),
    insights: () =>
      queryOptions({
        queryKey: observeKeys.overview.insights(),
        queryFn: observeApi.overview.insights,
      }),
    analysisRuns: () =>
      queryOptions({
        queryKey: observeKeys.overview.analysisRuns(),
        queryFn: observeApi.overview.analysisRuns,
      }),
    inspectionRuns: () =>
      queryOptions({
        queryKey: observeKeys.overview.inspectionRuns(),
        queryFn: observeApi.overview.inspectionRuns,
      }),
  },
  operations: {
    tasks: () =>
      queryOptions({
        queryKey: observeKeys.operations.tasks(),
        queryFn: observeApi.operations.tasks,
      }),
    runs: () =>
      queryOptions({
        queryKey: observeKeys.operations.runs(),
        queryFn: observeApi.operations.runs,
      }),
    policies: (enabled: boolean) =>
      queryOptions({
        queryKey: observeKeys.operations.policies(),
        queryFn: observeApi.operations.policies,
        enabled,
      }),
    catalog: () =>
      queryOptions({
        queryKey: observeKeys.operations.catalog(),
        queryFn: observeApi.operations.catalog,
      }),
  },
  tools: {
    catalog: () =>
      queryOptions({
        queryKey: observeKeys.tools.catalog(),
        queryFn: observeApi.tools.catalog,
      }),
    session: (sessionId?: string) =>
      queryOptions({
        queryKey: observeKeys.tools.session(sessionId),
        queryFn: () => observeApi.tools.session(sessionId!),
        enabled: Boolean(sessionId),
      }),
  },
}
