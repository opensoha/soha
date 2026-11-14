import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { capabilityTasksApi } from './api'
import { capabilityTaskKeys } from './queries'

export const capabilityTaskMutations = {
  validate: () =>
    mutationOptions({
      mutationKey: capabilityTaskKeys.mutation('validate'),
      mutationFn: capabilityTasksApi.validate,
    }),
  create: (client: QueryClient) =>
    mutationOptions({
      mutationKey: capabilityTaskKeys.mutation('create'),
      mutationFn: capabilityTasksApi.create,
      onSuccess: () => client.invalidateQueries({ queryKey: capabilityTaskKeys.all }),
    }),
  resume: (client: QueryClient) =>
    mutationOptions({
      mutationKey: capabilityTaskKeys.mutation('resume'),
      mutationFn: capabilityTasksApi.resume,
      onSuccess: () => client.invalidateQueries({ queryKey: capabilityTaskKeys.all }),
    }),
  cancel: (client: QueryClient) =>
    mutationOptions({
      mutationKey: capabilityTaskKeys.mutation('cancel'),
      mutationFn: capabilityTasksApi.cancel,
      onSuccess: () => client.invalidateQueries({ queryKey: capabilityTaskKeys.all }),
    }),
}
