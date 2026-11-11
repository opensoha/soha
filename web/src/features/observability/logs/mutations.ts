import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { observabilityKeys, observabilityMutationKeys } from '../keys'
import { createLogDataSource, updateLogDataSource, validateLogDataSource } from './api'

function invalidateDataSources(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: observabilityKeys.logs.dataSources() })
}

export const observabilityLogMutations = {
  createDataSource: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.logs('create-data-source'),
      mutationFn: createLogDataSource,
      onSuccess: () => invalidateDataSources(queryClient),
    }),
  updateDataSource: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.logs('update-data-source'),
      mutationFn: updateLogDataSource,
      onSuccess: () => invalidateDataSources(queryClient),
    }),
  validateDataSource: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.logs('validate-data-source'),
      mutationFn: validateLogDataSource,
      onSuccess: () => invalidateDataSources(queryClient),
    }),
}
