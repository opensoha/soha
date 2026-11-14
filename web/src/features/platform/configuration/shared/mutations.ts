import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  createConfigurationResource,
  deleteConfigurationResource,
  updateConfigurationData,
  updateConfigurationYAML,
} from './api'
import { configurationKeys } from './keys'
import type {
  ConfigurationKind,
  ConfigurationTarget,
  CreateConfigurationVariables,
  UpdateConfigurationDataVariables,
  UpdateConfigurationYAMLVariables,
} from './types'

export async function invalidateConfigurationCaches(
  queryClient: QueryClient,
  kind: ConfigurationKind,
  target?: ConfigurationTarget,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: configurationKeys.lists(kind) }),
    ...(target
      ? [
          queryClient.invalidateQueries({
            queryKey: configurationKeys.detail(kind, target.scope, target.name),
          }),
          queryClient.invalidateQueries({
            queryKey: configurationKeys.references(kind, target.scope, target.name),
          }),
          queryClient.invalidateQueries({
            queryKey: configurationKeys.yaml(kind, target.scope, target.name),
          }),
        ]
      : []),
  ])
}

export const configurationMutations = {
  create: (kind: ConfigurationKind, queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...configurationKeys.resource(kind), 'create'] as const,
      mutationFn: (variables: CreateConfigurationVariables) =>
        createConfigurationResource(kind, variables),
      onSuccess: () => invalidateConfigurationCaches(queryClient, kind),
    }),
  remove: (kind: ConfigurationKind, queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...configurationKeys.resource(kind), 'delete'] as const,
      mutationFn: (target: ConfigurationTarget) => deleteConfigurationResource(kind, target),
      onSuccess: (_data, target) => invalidateConfigurationCaches(queryClient, kind, target),
    }),
  updateData: <TDetail, TPayload>(kind: ConfigurationKind, queryClient: QueryClient) =>
    mutationOptions<TDetail, Error, UpdateConfigurationDataVariables<TPayload>>({
      mutationKey: [...configurationKeys.resource(kind), 'update-data'] as const,
      mutationFn: ({ target, payload }) =>
        updateConfigurationData<TDetail, TPayload>(kind, target, payload),
      onSuccess: (detail, { target }) => {
        queryClient.setQueryData(configurationKeys.detail(kind, target.scope, target.name), detail)
        return invalidateConfigurationCaches(queryClient, kind, target)
      },
    }),
  updateYAML: (kind: ConfigurationKind, queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...configurationKeys.resource(kind), 'update-yaml'] as const,
      mutationFn: (variables: UpdateConfigurationYAMLVariables) =>
        updateConfigurationYAML(kind, variables),
      onSuccess: (yaml, variables) => {
        queryClient.setQueryData(
          configurationKeys.yaml(kind, variables.scope, variables.name),
          yaml,
        )
        return invalidateConfigurationCaches(queryClient, kind, variables)
      },
    }),
}
