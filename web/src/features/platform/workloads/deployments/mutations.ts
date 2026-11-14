import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { deleteDeployment, restartDeployment, rollbackDeployment, scaleDeployment } from './api'
import type {
  DeploymentTarget,
  RollbackDeploymentVariables,
  ScaleDeploymentVariables,
} from './types'

async function invalidateDeploymentCaches(queryClient: QueryClient, target: DeploymentTarget) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('deployments') }),
    queryClient.invalidateQueries({
      queryKey: workloadKeys.detail('deployments', target.scope, target.name),
    }),
    queryClient.invalidateQueries({ queryKey: workloadKeys.lists('pods') }),
  ])
}

export const deploymentMutations = {
  restart: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...workloadKeys.resource('deployments'), 'restart'] as const,
      mutationFn: restartDeployment,
      onSuccess: (_data, variables) => invalidateDeploymentCaches(queryClient, variables),
    }),
  scale: (queryClient: QueryClient) =>
    mutationOptions<void, Error, ScaleDeploymentVariables>({
      mutationKey: [...workloadKeys.resource('deployments'), 'scale'] as const,
      mutationFn: scaleDeployment,
      onSuccess: (_data, variables) => invalidateDeploymentCaches(queryClient, variables),
    }),
  rollback: (queryClient: QueryClient) =>
    mutationOptions<void, Error, RollbackDeploymentVariables>({
      mutationKey: [...workloadKeys.resource('deployments'), 'rollback'] as const,
      mutationFn: rollbackDeployment,
      onSuccess: (_data, variables) => invalidateDeploymentCaches(queryClient, variables),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...workloadKeys.resource('deployments'), 'delete'] as const,
      mutationFn: deleteDeployment,
      onSuccess: (_data, variables) => invalidateDeploymentCaches(queryClient, variables),
    }),
}
