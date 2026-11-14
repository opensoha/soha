import { queryOptions } from '@tanstack/react-query'
import { runtimeConfigurationApi } from './api'
import { runtimeConfigurationKeys } from './keys'

export const runtimeConfigurationQueries = {
  snapshot: (enabled = true) =>
    queryOptions({
      queryKey: runtimeConfigurationKeys.snapshot(),
      queryFn: runtimeConfigurationApi.get,
      enabled,
    }),
  resources: (enabled = true) =>
    queryOptions({
      queryKey: runtimeConfigurationKeys.resources(),
      queryFn: runtimeConfigurationApi.resources,
      enabled,
      refetchInterval: (query) => (query.state.status === 'error' ? false : 5_000),
      refetchIntervalInBackground: false,
    }),
  history: (enabled = true) =>
    queryOptions({
      queryKey: runtimeConfigurationKeys.histories(),
      queryFn: runtimeConfigurationApi.history,
      enabled,
    }),
  application: (id?: string) =>
    queryOptions({
      queryKey: runtimeConfigurationKeys.application(id ?? ''),
      queryFn: () => runtimeConfigurationApi.application(id ?? ''),
      enabled: Boolean(id),
      refetchInterval: (query) => {
        if (query.state.status === 'error') return false
        const status = query.state.data?.status
        return status === 'pending' || status === 'applying' ? 1_000 : false
      },
    }),
}
