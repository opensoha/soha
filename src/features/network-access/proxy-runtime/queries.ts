import { queryOptions } from '@tanstack/react-query'
import type { InstanceFilters } from './api'
import {
  getCloseCommand,
  getConnections,
  getTraffic,
  listInstances,
  getConfigurationHistory,
} from './api'

export const proxyKeys = {
  all: ['network-access', 'proxy-instances'] as const,
  history: (id: string) => [...proxyKeys.all, id, 'history'] as const,
  list: (filters: InstanceFilters) =>
    [...proxyKeys.all, 'list', filters.search?.trim() ?? '', filters.engine ?? ''] as const,
  closeCommand: (id: string, commandId: string) =>
    [...proxyKeys.all, id, 'close-command', commandId] as const,
  traffic: (id: string) => [...proxyKeys.all, id, 'traffic'] as const,
  connections: (id: string) => [...proxyKeys.all, id, 'connections'] as const,
}

export const proxyQueries = {
  history: (id: string, enabled: boolean) =>
    queryOptions({
      queryKey: proxyKeys.history(id),
      queryFn: () => getConfigurationHistory(id),
      enabled,
      retry: false,
    }),
  closeCommand: (id: string | undefined, commandId: string | undefined, enabled = true) =>
    queryOptions({
      queryKey: proxyKeys.closeCommand(id ?? '', commandId ?? ''),
      queryFn: () => getCloseCommand(id!, commandId!),
      enabled: enabled && Boolean(id && commandId),
      refetchInterval: (query) =>
        query.state.error || query.state.data?.status !== 'pending' ? false : 2_000,
      retry: false,
    }),
  instances: (filters: InstanceFilters = {}, enabled = true) =>
    queryOptions({
      queryKey: proxyKeys.list(filters),
      queryFn: () => listInstances(filters),
      enabled,
      refetchInterval: (query) => (query.state.error ? false : 15_000),
      retry: false,
    }),
  traffic: (id: string | undefined, enabled = true) =>
    queryOptions({
      queryKey: proxyKeys.traffic(id ?? ''),
      queryFn: () => getTraffic(id!),
      enabled: enabled && Boolean(id),
      refetchInterval: (query) => (query.state.error ? false : 15_000),
      retry: false,
    }),
  connections: (id: string | undefined, enabled = true) =>
    queryOptions({
      queryKey: proxyKeys.connections(id ?? ''),
      queryFn: () => getConnections(id!),
      enabled: enabled && Boolean(id),
      refetchInterval: (query) => (query.state.error ? false : 10_000),
      retry: false,
    }),
}
