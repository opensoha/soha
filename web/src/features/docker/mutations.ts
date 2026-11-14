import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { computeKeys } from '@/features/compute'
import { dockerApi } from './docker-api'
import { dockerKeys, dockerMutationKeys } from './keys'
import type {
  DockerContainerStartInput,
  DockerHostInput,
  DockerPortMappingInput,
  DockerProjectInput,
  DockerQuickCreateHostInput,
  DockerTemplateInput,
} from './docker-types'

export interface UpdateDockerHostVariables {
  id: string
  payload: DockerHostInput
}

export interface UpdateDockerProjectVariables {
  id: string
  payload: DockerProjectInput
}

export interface DeployDockerProjectVariables {
  idempotencyKey: string
  id: string
  action: string
}

export interface QuickCreateDockerHostVariables {
  idempotencyKey: string
  payload: DockerQuickCreateHostInput
}

export interface DockerServiceActionVariables {
  id: string
  action: string
}

export interface UpdateDockerPortVariables {
  id: string
  payload: DockerPortMappingInput
}

export interface UpdateDockerTemplateVariables {
  id: string
  payload: DockerTemplateInput
}

export function invalidateDockerQueries(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: dockerKeys.all }),
    queryClient.invalidateQueries({ queryKey: computeKeys.overview() }),
  ])
}

export const dockerMutations = {
  createHost: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.host('create'),
      mutationFn: (payload: DockerHostInput) => dockerApi.createHost(payload),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  updateHost: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.host('update'),
      mutationFn: ({ id, payload }: UpdateDockerHostVariables) => dockerApi.updateHost(id, payload),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  deleteHost: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.host('delete'),
      mutationFn: (id: string) => dockerApi.deleteHost(id),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  planQuickCreateHost: () =>
    mutationOptions({
      mutationKey: dockerMutationKeys.host('quick-create-plan'),
      mutationFn: (payload: DockerQuickCreateHostInput) => dockerApi.planQuickCreateHost(payload),
    }),
  quickCreateHost: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.host('quick-create'),
      mutationFn: ({ payload, idempotencyKey }: QuickCreateDockerHostVariables) =>
        dockerApi.quickCreateHost(payload, idempotencyKey),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  createProject: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.project('create'),
      mutationFn: (payload: DockerProjectInput) => dockerApi.createProject(payload),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  updateProject: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.project('update'),
      mutationFn: ({ id, payload }: UpdateDockerProjectVariables) =>
        dockerApi.updateProject(id, payload),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  deleteProject: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.project('delete'),
      mutationFn: (id: string) => dockerApi.deleteProject(id),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  planDeployProject: () =>
    mutationOptions({
      mutationKey: dockerMutationKeys.project('deploy-plan'),
      mutationFn: ({ id, action }: Omit<DeployDockerProjectVariables, 'idempotencyKey'>) =>
        dockerApi.planProjectDeploy(id, action),
    }),
  deployProject: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.project('deploy'),
      mutationFn: ({ id, action, idempotencyKey }: DeployDockerProjectVariables) =>
        dockerApi.deployProject(id, action, idempotencyKey),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  startContainer: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.container('start'),
      mutationFn: (payload: DockerContainerStartInput) => dockerApi.startContainer(payload),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  serviceAction: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.service('action'),
      mutationFn: ({ id, action }: DockerServiceActionVariables) =>
        dockerApi.serviceAction(id, action),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  createPort: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.port('create'),
      mutationFn: (payload: DockerPortMappingInput) => dockerApi.createPort(payload),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  updatePort: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.port('update'),
      mutationFn: ({ id, payload }: UpdateDockerPortVariables) => dockerApi.updatePort(id, payload),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  deletePort: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.port('delete'),
      mutationFn: (id: string) => dockerApi.deletePort(id),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  createTemplate: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.template('create'),
      mutationFn: (payload: DockerTemplateInput) => dockerApi.createTemplate(payload),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  updateTemplate: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.template('update'),
      mutationFn: ({ id, payload }: UpdateDockerTemplateVariables) =>
        dockerApi.updateTemplate(id, payload),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  deleteTemplate: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.template('delete'),
      mutationFn: (id: string) => dockerApi.deleteTemplate(id),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  cancelOperation: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.operation('cancel'),
      mutationFn: (id: string) => dockerApi.cancelOperation(id),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
  retryOperation: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: dockerMutationKeys.operation('retry'),
      mutationFn: (id: string) => dockerApi.retryOperation(id),
      onSuccess: () => invalidateDockerQueries(queryClient),
    }),
}
