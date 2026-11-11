import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { memoryApi } from './api'
import { memoryKeys, memoryMutationKeys } from './keys'
export const memoryMutations = {
  deleteRecord: (c: QueryClient) =>
    mutationOptions({
      mutationKey: memoryMutationKeys.deleteRecord,
      mutationFn: memoryApi.records.delete,
      onSuccess: () => c.invalidateQueries({ queryKey: memoryKeys.records() }),
    }),
  createPolicy: (c: QueryClient) =>
    mutationOptions({
      mutationKey: memoryMutationKeys.createPolicy,
      mutationFn: memoryApi.policies.create,
      onSuccess: () => c.invalidateQueries({ queryKey: memoryKeys.policies() }),
    }),
}
