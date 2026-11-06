import type { ScopeKey } from '@/types'
import type {
  AgentInstallation,
  LogCollectionDisableInput,
  LogCollectionEnableInput,
  LogCollectionPlan,
  LogCollectionPreflightInput,
  LogCollectionState,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  createAgentInstallation,
  createCluster,
  deleteCluster,
  disableClusterLogCollection,
  enableClusterLogCollection,
  preflightClusterLogCollection,
  updateCluster,
  upgradeAgent,
} from './api'
import { clusterKeys } from './keys'
import { monitoringKeys } from '../resource-monitoring/keys'
import type {
  ClusterPayload,
  ClusterTarget,
  DeleteClustersVariables,
  UpdateClusterVariables,
} from './types'

async function invalidateClusterTarget(queryClient: QueryClient, target: ClusterTarget) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: clusterKeys.list() }),
    queryClient.invalidateQueries({ queryKey: clusterKeys.detail(target.scope) }),
    queryClient.invalidateQueries({ queryKey: clusterKeys.nodes(target.scope) }),
    queryClient.invalidateQueries({
      queryKey: monitoringKeys.all,
      predicate: (query) =>
        query.queryKey.some(
          (part) =>
            typeof part === 'object' &&
            part !== null &&
            'clusterId' in part &&
            part.clusterId === target.scope.clusterId,
        ),
    }),
  ])
}

export const clusterMutations = {
  upgradeAgent: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...clusterKeys.all, 'agent-upgrade'] as const,
      mutationFn: upgradeAgent,
      onSuccess: (_data, variables) => invalidateClusterTarget(queryClient, variables),
    }),
  createAgentInstallation: () =>
    mutationOptions<AgentInstallation, Error, ClusterTarget>({
      mutationKey: [...clusterKeys.all, 'agent-installation'] as const,
      mutationFn: createAgentInstallation,
    }),
  create: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...clusterKeys.all, 'create'] as const,
      mutationFn: (values: ClusterPayload) => createCluster(values),
      onSuccess: () => {
        void queryClient.invalidateQueries({ queryKey: clusterKeys.list() })
      },
    }),
  update: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...clusterKeys.all, 'update'] as const,
      mutationFn: (variables: UpdateClusterVariables) => updateCluster(variables),
      onSuccess: (_data, variables) => invalidateClusterTarget(queryClient, variables),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...clusterKeys.all, 'delete'] as const,
      mutationFn: (target: ClusterTarget) => deleteCluster(target),
      onSuccess: (_data, variables) => invalidateClusterTarget(queryClient, variables),
    }),
  removeMany: (queryClient: QueryClient) =>
    mutationOptions<PromiseSettledResult<void>[], Error, DeleteClustersVariables>({
      mutationKey: [...clusterKeys.all, 'delete-many'] as const,
      mutationFn: async ({ scopes }) => {
        return Promise.allSettled(scopes.map((scope) => deleteCluster({ scope })))
      },
      onSuccess: async (_data, { scopes }) => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: clusterKeys.list() }),
          ...scopes.flatMap((scope) => [
            queryClient.invalidateQueries({ queryKey: clusterKeys.detail(scope) }),
            queryClient.invalidateQueries({ queryKey: clusterKeys.nodes(scope) }),
          ]),
        ])
      },
    }),
  preflightLogCollection: () =>
    mutationOptions<
      LogCollectionPlan,
      Error,
      { scope: ScopeKey; input: LogCollectionPreflightInput }
    >({
      mutationKey: [...clusterKeys.all, 'log-collection', 'preflight'] as const,
      mutationFn: preflightClusterLogCollection,
    }),
  enableLogCollection: (queryClient: QueryClient) =>
    mutationOptions<
      LogCollectionState,
      Error,
      { scope: ScopeKey; input: LogCollectionEnableInput }
    >({
      mutationKey: [...clusterKeys.all, 'log-collection', 'enable'] as const,
      mutationFn: enableClusterLogCollection,
      onSuccess: (_data, variables) =>
        queryClient.invalidateQueries({ queryKey: clusterKeys.logCollection(variables.scope) }),
    }),
  disableLogCollection: (queryClient: QueryClient) =>
    mutationOptions<
      LogCollectionState,
      Error,
      { scope: ScopeKey; input: LogCollectionDisableInput }
    >({
      mutationKey: [...clusterKeys.all, 'log-collection', 'disable'] as const,
      mutationFn: disableClusterLogCollection,
      onSuccess: (_data, variables) =>
        queryClient.invalidateQueries({ queryKey: clusterKeys.logCollection(variables.scope) }),
    }),
}
