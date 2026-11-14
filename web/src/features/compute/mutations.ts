import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import type {
  ComputeDomain,
  ComputeProviderDiscoverRequest,
  ComputeProviderDomain,
  ComputeProviderReadRequest,
  ComputeResourceActionRequest,
  ComputeResourceKind,
  ComputeTaskDomain,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { computeApi } from './api'
import { computeKeys, computeMutationKeys } from './keys'

export interface ComputeTaskMutationVariables {
  domain: ComputeTaskDomain
  taskId: string
}

export interface ComputeProviderMutationVariables {
  domain: ComputeProviderDomain
  providerKey: string
  instanceRef: string
  input: ComputeProviderReadRequest | ComputeProviderDiscoverRequest
}

export interface ComputeResourceActionVariables {
  domain: ComputeDomain
  kind: ComputeResourceKind
  id: string
  action: string
  input?: ComputeResourceActionRequest
}

export function invalidateComputeTaskQueries(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: computeKeys.all })
}

export const computeMutations = {
  cancelTask: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: computeMutationKeys.task('cancel'),
      mutationFn: ({ domain, taskId }: ComputeTaskMutationVariables) =>
        computeApi.cancelTask(domain, taskId),
      onSuccess: () => invalidateComputeTaskQueries(queryClient),
    }),
  retryTask: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: computeMutationKeys.task('retry'),
      mutationFn: ({ domain, taskId }: ComputeTaskMutationVariables) =>
        computeApi.retryTask(domain, taskId),
      onSuccess: () => invalidateComputeTaskQueries(queryClient),
    }),
  checkProviderHealth: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: computeMutationKeys.provider('health'),
      mutationFn: ({ domain, providerKey, instanceRef, input }: ComputeProviderMutationVariables) =>
        computeApi.checkProviderHealth(
          domain,
          providerKey,
          instanceRef,
          input as ComputeProviderReadRequest,
        ),
      onSuccess: () => invalidateComputeTaskQueries(queryClient),
    }),
  discoverProvider: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: computeMutationKeys.provider('discover'),
      mutationFn: ({ domain, providerKey, instanceRef, input }: ComputeProviderMutationVariables) =>
        computeApi.discoverProvider(
          domain,
          providerKey,
          instanceRef,
          input as ComputeProviderDiscoverRequest,
        ),
      onSuccess: () => invalidateComputeTaskQueries(queryClient),
    }),
  executeResourceAction: (queryClient: QueryClient, action: string) =>
    mutationOptions({
      mutationKey: computeMutationKeys.resourceAction(action),
      mutationFn: ({ domain, kind, id, action: requestedAction, input }: ComputeResourceActionVariables) =>
        computeApi.executeResourceAction(domain, kind, id, requestedAction, input),
      onSuccess: () => invalidateComputeTaskQueries(queryClient),
    }),
}
