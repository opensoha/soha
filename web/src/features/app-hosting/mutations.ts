import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { appHostingApi } from './api'
import { appHostingKeys } from './keys'
import type {
  CreateHostedAppRequest,
  DeleteHostedAppRequest,
  HostedAppLifecycleActionRequest,
  HostedAppRollbackRequest,
  UpdateHostedAppMaintainersRequest,
  UpdateHostedAppRequest,
  UpdateHostedAppVisibilityRequest,
  WorkspaceHostedAppDeploymentRequest,
} from './types'

export async function invalidateHostedAppChange(queryClient: QueryClient, appId?: string) {
  const tasks: Array<Promise<unknown>> = [
    queryClient.invalidateQueries({ queryKey: appHostingKeys.myAppsLists() }),
    queryClient.invalidateQueries({ queryKey: appHostingKeys.portalAll() }),
    queryClient.invalidateQueries({ queryKey: appHostingKeys.approvalsAll() }),
  ]
  if (appId?.trim()) {
    tasks.push(
      queryClient.invalidateQueries({ queryKey: appHostingKeys.appDetail(appId) }),
      queryClient.invalidateQueries({ queryKey: appHostingKeys.revisionsAll(appId) }),
    )
  }
  await Promise.all(tasks)
}

export const appHostingMutations = {
  createApp: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'create'] as const,
      mutationFn: (input: CreateHostedAppRequest) => appHostingApi.createApp(input),
      onSuccess: (app) => invalidateHostedAppChange(queryClient, app.id),
    }),

  updateApp: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'update'] as const,
      mutationFn: ({ appId, input }: { appId: string; input: UpdateHostedAppRequest }) =>
        appHostingApi.updateApp(appId, input),
      onSuccess: (app) => invalidateHostedAppChange(queryClient, app.id),
    }),

  updateVisibility: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'visibility'] as const,
      mutationFn: ({ appId, input }: { appId: string; input: UpdateHostedAppVisibilityRequest }) =>
        appHostingApi.updateVisibility(appId, input),
      onSuccess: (app) => invalidateHostedAppChange(queryClient, app.id),
    }),

  withdrawVisibilityApproval: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'withdraw-visibility-approval'] as const,
      mutationFn: ({ appId, input }: { appId: string; input?: HostedAppLifecycleActionRequest }) =>
        appHostingApi.withdrawVisibilityApproval(appId, input),
      onSuccess: (app) => invalidateHostedAppChange(queryClient, app.id),
    }),

  updateMaintainers: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'maintainers'] as const,
      mutationFn: ({ appId, input }: { appId: string; input: UpdateHostedAppMaintainersRequest }) =>
        appHostingApi.updateMaintainers(appId, input),
      onSuccess: (app) => invalidateHostedAppChange(queryClient, app.id),
    }),

  takeOffline: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'offline'] as const,
      mutationFn: ({ appId, input }: { appId: string; input?: HostedAppLifecycleActionRequest }) =>
        appHostingApi.takeOffline(appId, input),
      onSuccess: (app) => invalidateHostedAppChange(queryClient, app.id),
    }),

  bringOnline: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'online'] as const,
      mutationFn: ({ appId, input }: { appId: string; input?: HostedAppLifecycleActionRequest }) =>
        appHostingApi.bringOnline(appId, input),
      onSuccess: (app) => invalidateHostedAppChange(queryClient, app.id),
    }),

  deleteApp: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'delete'] as const,
      mutationFn: ({ appId, input }: { appId: string; input: DeleteHostedAppRequest }) =>
        appHostingApi.deleteApp(appId, input),
      onSuccess: async (result) => {
        queryClient.removeQueries({ queryKey: appHostingKeys.appDetail(result.appId) })
        queryClient.removeQueries({ queryKey: appHostingKeys.revisionsAll(result.appId) })
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: appHostingKeys.myAppsLists() }),
          queryClient.invalidateQueries({ queryKey: appHostingKeys.portalAll() }),
          queryClient.invalidateQueries({ queryKey: appHostingKeys.approvalsAll() }),
        ])
      },
    }),

  rollbackApp: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'rollback'] as const,
      mutationFn: ({ appId, input }: { appId: string; input: HostedAppRollbackRequest }) =>
        appHostingApi.rollbackApp(appId, input),
      onSuccess: (receipt) => invalidateHostedAppChange(queryClient, receipt.app.id),
    }),

  deployFromWorkspace: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...appHostingKeys.all, 'mutation', 'workspace-deploy'] as const,
      mutationFn: (input: WorkspaceHostedAppDeploymentRequest) =>
        appHostingApi.deployFromWorkspace(input),
      onSuccess: (result) => invalidateHostedAppChange(queryClient, result.app.id),
    }),
}
