import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import { observabilityOncallApi } from './api'

export const observabilityOncallQueries = {
  users: (enabled = true) =>
    queryOptions({
      queryKey: observabilityKeys.oncall.users(),
      queryFn: observabilityOncallApi.listUsers,
      enabled,
    }),
  schedules: () =>
    queryOptions({
      queryKey: observabilityKeys.oncall.schedules(),
      queryFn: observabilityOncallApi.listSchedules,
    }),
  rotations: () =>
    queryOptions({
      queryKey: observabilityKeys.oncall.rotations(),
      queryFn: observabilityOncallApi.listRotations,
    }),
  escalationPolicies: () =>
    queryOptions({
      queryKey: observabilityKeys.oncall.escalationPolicies(),
      queryFn: observabilityOncallApi.listEscalationPolicies,
    }),
  routes: () =>
    queryOptions({
      queryKey: observabilityKeys.oncall.routes(),
      queryFn: observabilityOncallApi.listRoutes,
    }),
  tasks: () =>
    queryOptions({
      queryKey: observabilityKeys.oncall.tasks(),
      queryFn: observabilityOncallApi.listTasks,
      refetchInterval: (query) => (query.state.status === 'error' ? false : 30_000),
    }),
}
