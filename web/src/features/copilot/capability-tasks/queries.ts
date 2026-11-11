import { queryOptions } from '@tanstack/react-query'
import { capabilityTasksApi } from './api'

export const capabilityTaskKeys = {
  all: ['copilot', 'capability-tasks'] as const,
  list: () => [...capabilityTaskKeys.all, 'list'] as const,
  detail: (id: string, version = 0) => [...capabilityTaskKeys.all, 'detail', id, version] as const,
  mutation: (action: string) => [...capabilityTaskKeys.all, action] as const,
}

export const taskCanResume = (status: string) =>
  ['completed', 'failed', 'canceled', 'blocked', 'inconclusive'].includes(status)
export const capabilityTaskQueries = {
  list: (enabled: boolean) =>
    queryOptions({
      queryKey: capabilityTaskKeys.list(),
      queryFn: capabilityTasksApi.list,
      enabled,
    }),
  detail: (id: string, version: number, enabled: boolean) =>
    queryOptions({
      queryKey: capabilityTaskKeys.detail(id, version),
      queryFn: () => capabilityTasksApi.get(id, version),
      enabled: enabled && Boolean(id),
      refetchInterval: (query) =>
        version || query.state.error || taskCanResume(query.state.data?.data.status ?? '')
          ? false
          : 3000,
    }),
}
