import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { evaluationLifecycleApi } from './api'
import { evaluationLifecycleKeys as k, evaluationLifecycleMutationKeys as m } from './keys'
export const evaluationLifecycleMutations = {
  execute: () =>
    mutationOptions({ mutationKey: m.execute, mutationFn: evaluationLifecycleApi.execute }),
  replay: (c: QueryClient) =>
    mutationOptions({
      mutationKey: m.replay,
      mutationFn: evaluationLifecycleApi.replays.create,
      onSuccess: () => c.invalidateQueries({ queryKey: k.replays() }),
    }),
  policy: (c: QueryClient) =>
    mutationOptions({
      mutationKey: m.policy,
      mutationFn: evaluationLifecycleApi.policies.create,
      onSuccess: () => c.invalidateQueries({ queryKey: k.policies() }),
    }),
  gate: () =>
    mutationOptions({ mutationKey: m.gate, mutationFn: evaluationLifecycleApi.evaluateGate }),
  feedback: (c: QueryClient) =>
    mutationOptions({
      mutationKey: m.feedback,
      mutationFn: evaluationLifecycleApi.feedback.create,
      onSuccess: () => c.invalidateQueries({ queryKey: k.feedback() }),
    }),
}
