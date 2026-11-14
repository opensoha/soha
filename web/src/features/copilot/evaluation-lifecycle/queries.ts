import { queryOptions } from '@tanstack/react-query'
import { evaluationLifecycleApi } from './api'
import { evaluationLifecycleKeys } from './keys'
export const evaluationLifecycleQueries = {
  replays: () =>
    queryOptions({
      queryKey: evaluationLifecycleKeys.replays(),
      queryFn: evaluationLifecycleApi.replays.list,
      refetchInterval: 10_000,
    }),
  policies: () =>
    queryOptions({
      queryKey: evaluationLifecycleKeys.policies(),
      queryFn: evaluationLifecycleApi.policies.list,
    }),
  feedback: () =>
    queryOptions({
      queryKey: evaluationLifecycleKeys.feedback(),
      queryFn: evaluationLifecycleApi.feedback.list,
    }),
}
