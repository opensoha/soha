import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { observabilityKeys, observabilityMutationKeys } from '../keys'
import { observabilityHealingApi } from './api'

const invalidatePolicies = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: observabilityKeys.healing.policies() })
const invalidateRuns = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: observabilityKeys.healing.runs() })

export const observabilityHealingMutations = {
  createPolicy: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.healing('create'),
      mutationFn: observabilityHealingApi.createPolicy,
      onSuccess: () => invalidatePolicies(queryClient),
    }),
  updatePolicy: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.healing('update'),
      mutationFn: observabilityHealingApi.updatePolicy,
      onSuccess: () => invalidatePolicies(queryClient),
    }),
  approveRun: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.healing('approve'),
      mutationFn: observabilityHealingApi.approveRun,
      onSuccess: () => invalidateRuns(queryClient),
    }),
  rejectRun: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.healing('reject'),
      mutationFn: observabilityHealingApi.rejectRun,
      onSuccess: () => invalidateRuns(queryClient),
    }),
  retryRun: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.healing('retry'),
      mutationFn: observabilityHealingApi.retryRun,
      onSuccess: () => invalidateRuns(queryClient),
    }),
}
