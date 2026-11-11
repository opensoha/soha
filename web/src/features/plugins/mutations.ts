import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { pluginApi } from './plugin-api'
import { pluginKeys, pluginMutationKeys } from './keys'
import type { PluginConfigRequest, PluginInstallRequest } from './plugin-model'

export interface PluginLifecycleVariables {
  pluginId: string
}

export interface UpgradePluginVariables extends PluginLifecycleVariables {
  input?: PluginInstallRequest
}

export interface ConfigurePluginVariables extends PluginLifecycleVariables {
  input: PluginConfigRequest
}

export function invalidatePluginQueries(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: pluginKeys.all })
}

export const pluginMutations = {
  install: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: pluginMutationKeys.install(),
      mutationFn: (input: PluginInstallRequest) => pluginApi.install(input),
      onSuccess: () => invalidatePluginQueries(queryClient),
    }),
  enable: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: pluginMutationKeys.lifecycle('enable'),
      mutationFn: ({ pluginId }: PluginLifecycleVariables) => pluginApi.enable(pluginId),
      onSuccess: () => invalidatePluginQueries(queryClient),
    }),
  disable: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: pluginMutationKeys.lifecycle('disable'),
      mutationFn: ({ pluginId }: PluginLifecycleVariables) => pluginApi.disable(pluginId),
      onSuccess: () => invalidatePluginQueries(queryClient),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: pluginMutationKeys.lifecycle('remove'),
      mutationFn: ({ pluginId }: PluginLifecycleVariables) => pluginApi.remove(pluginId),
      onSuccess: () => invalidatePluginQueries(queryClient),
    }),
  upgrade: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: pluginMutationKeys.lifecycle('upgrade'),
      mutationFn: ({ pluginId, input }: UpgradePluginVariables) =>
        pluginApi.upgrade(pluginId, input ?? { pluginId }),
      onSuccess: () => invalidatePluginQueries(queryClient),
    }),
  configure: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: pluginMutationKeys.configure(),
      mutationFn: ({ pluginId, input }: ConfigurePluginVariables) =>
        pluginApi.configure(pluginId, input),
      onSuccess: () => invalidatePluginQueries(queryClient),
    }),
}
