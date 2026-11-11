import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { evaluationApi } from './api'
import { evaluationKeys, evaluationMutationKeys } from './keys'

export const evaluationMutations = {
  createDataset: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: evaluationMutationKeys.createDataset,
      mutationFn: evaluationApi.datasets.create,
      onSuccess: () => queryClient.invalidateQueries({ queryKey: evaluationKeys.datasets() }),
    }),
  startRun: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: evaluationMutationKeys.startRun,
      mutationFn: evaluationApi.runs.create,
      onSuccess: () => queryClient.invalidateQueries({ queryKey: evaluationKeys.runs() }),
    }),
}
