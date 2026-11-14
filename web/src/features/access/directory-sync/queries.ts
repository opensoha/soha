import { queryOptions } from '@tanstack/react-query'
import { directorySyncApi } from './api'

export const directorySyncKeys = {
  all: ['access', 'directory-sync'] as const,
  connections: () => [...directorySyncKeys.all, 'connections'] as const,
  runs: (connectionId: string) => [...directorySyncKeys.all, 'runs', connectionId] as const,
  runtimeStatus: (connectionId: string) =>
    [...directorySyncKeys.all, 'runtime-status', connectionId] as const,
  events: (connectionId: string) => [...directorySyncKeys.all, 'events', connectionId] as const,
  conflicts: () => [...directorySyncKeys.all, 'conflicts'] as const,
}

export const directorySyncQueries = {
  connections: (enabled = true) =>
    queryOptions({
      queryKey: directorySyncKeys.connections(),
      queryFn: directorySyncApi.listConnections,
      enabled,
    }),
  runs: (connectionId: string) =>
    queryOptions({
      queryKey: directorySyncKeys.runs(connectionId),
      queryFn: () => directorySyncApi.listRuns(connectionId),
      enabled: Boolean(connectionId),
      refetchInterval: (query) =>
        query.state.status !== 'error' &&
        query.state.data?.some((run) => run.status === 'queued' || run.status === 'running')
          ? 3000
          : false,
    }),
  runtimeStatus: (connectionId: string) =>
    queryOptions({
      queryKey: directorySyncKeys.runtimeStatus(connectionId),
      queryFn: () => directorySyncApi.getRuntimeStatus(connectionId),
      enabled: Boolean(connectionId),
      refetchInterval: (query) => (query.state.status === 'error' ? false : 5000),
    }),
  events: (connectionId: string) =>
    queryOptions({
      queryKey: directorySyncKeys.events(connectionId),
      queryFn: () => directorySyncApi.listEvents(connectionId),
      enabled: Boolean(connectionId),
      refetchInterval: (query) => (query.state.status === 'error' ? false : 5000),
    }),
  conflicts: (enabled = true) =>
    queryOptions({
      queryKey: directorySyncKeys.conflicts(),
      queryFn: directorySyncApi.listConflicts,
      enabled,
    }),
}
