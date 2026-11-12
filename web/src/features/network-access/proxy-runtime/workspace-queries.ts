import { queryOptions } from '@tanstack/react-query'
import { workspaceApi } from './workspace-api'
export const workspaceKeys = {
  all: ['network-access', 'proxy-workspace'] as const,
  instance: (id: string) => [...workspaceKeys.all, id] as const,
  sources: (id: string) => [...workspaceKeys.instance(id), 'sources'] as const,
  bundles: (id: string) => [...workspaceKeys.instance(id), 'bundles'] as const,
  rules: (id: string, personal: boolean) =>
    [...workspaceKeys.instance(id), 'rules', personal] as const,
  deployment: (id: string) => [...workspaceKeys.instance(id), 'deployment'] as const,
  command: (id: string, requestId: string) =>
    [...workspaceKeys.instance(id), 'command', requestId] as const,
}
export const workspaceQueries = {
  sources: (id: string, enabled: boolean) =>
    queryOptions({
      queryKey: workspaceKeys.sources(id),
      queryFn: () => workspaceApi.sources(id),
      enabled,
      retry: false,
    }),
  bundles: (id: string, enabled: boolean) =>
    queryOptions({
      queryKey: workspaceKeys.bundles(id),
      queryFn: () => workspaceApi.bundles(id),
      enabled,
      retry: false,
      refetchInterval: (q) => (q.state.error ? false : 5000),
    }),
  rules: (id: string, personal: boolean, enabled: boolean) =>
    queryOptions({
      queryKey: workspaceKeys.rules(id, personal),
      queryFn: () => workspaceApi.rules(id, personal),
      enabled,
      retry: false,
    }),
  deployment: (id: string, enabled: boolean) =>
    queryOptions({
      queryKey: workspaceKeys.deployment(id),
      queryFn: () => workspaceApi.deployment(id),
      enabled,
      retry: false,
      refetchInterval: (q) => (q.state.error ? false : 5000),
    }),
  command: (id: string, requestId: string, enabled: boolean) =>
    queryOptions({
      queryKey: workspaceKeys.command(id, requestId),
      queryFn: () => workspaceApi.command(id, requestId),
      enabled: enabled && !!requestId,
      retry: false,
      refetchInterval: (q) =>
        q.state.error || (q.state.data && !['pending', 'running'].includes(q.state.data.status))
          ? false
          : 1500,
    }),
}
