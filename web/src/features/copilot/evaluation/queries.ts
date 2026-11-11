import { queryOptions } from '@tanstack/react-query'
import { evaluationApi } from './api'
import { evaluationKeys } from './keys'

export const evaluationQueries = {
  datasets: () =>
    queryOptions({
      queryKey: evaluationKeys.datasets(),
      queryFn: evaluationApi.datasets.list,
    }),
  runs: () =>
    queryOptions({
      queryKey: evaluationKeys.runs(),
      queryFn: evaluationApi.runs.list,
      refetchInterval: 10_000,
    }),
  results: (runId?: string) =>
    queryOptions({
      queryKey: evaluationKeys.results(runId ?? ''),
      queryFn: () => evaluationApi.runs.results(runId!),
      enabled: Boolean(runId),
    }),
}
