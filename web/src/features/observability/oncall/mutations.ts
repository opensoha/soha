import { mutationOptions, type QueryClient, type QueryKey } from '@tanstack/react-query'
import { observabilityKeys, observabilityMutationKeys } from '../keys'
import { observabilityOncallApi } from './api'

function writeOptions<TVariables>(
  queryClient: QueryClient,
  resource: string,
  action: 'create' | 'update',
  mutationFn: (variables: TVariables) => Promise<unknown>,
  queryKey: QueryKey,
) {
  return mutationOptions({
    mutationKey: observabilityMutationKeys.oncall(resource, action),
    mutationFn,
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  })
}

export const observabilityOncallMutations = {
  createSchedule: (queryClient: QueryClient) =>
    writeOptions(
      queryClient,
      'schedule',
      'create',
      observabilityOncallApi.createSchedule,
      observabilityKeys.oncall.schedules(),
    ),
  updateSchedule: (queryClient: QueryClient) =>
    writeOptions(
      queryClient,
      'schedule',
      'update',
      observabilityOncallApi.updateSchedule,
      observabilityKeys.oncall.schedules(),
    ),
  createRotation: (queryClient: QueryClient) =>
    writeOptions(
      queryClient,
      'rotation',
      'create',
      observabilityOncallApi.createRotation,
      observabilityKeys.oncall.rotations(),
    ),
  updateRotation: (queryClient: QueryClient) =>
    writeOptions(
      queryClient,
      'rotation',
      'update',
      observabilityOncallApi.updateRotation,
      observabilityKeys.oncall.rotations(),
    ),
  createEscalationPolicy: (queryClient: QueryClient) =>
    writeOptions(
      queryClient,
      'escalation-policy',
      'create',
      observabilityOncallApi.createEscalationPolicy,
      observabilityKeys.oncall.escalationPolicies(),
    ),
  updateEscalationPolicy: (queryClient: QueryClient) =>
    writeOptions(
      queryClient,
      'escalation-policy',
      'update',
      observabilityOncallApi.updateEscalationPolicy,
      observabilityKeys.oncall.escalationPolicies(),
    ),
  createRoute: (queryClient: QueryClient) =>
    writeOptions(
      queryClient,
      'route',
      'create',
      observabilityOncallApi.createRoute,
      observabilityKeys.oncall.routes(),
    ),
  updateRoute: (queryClient: QueryClient) =>
    writeOptions(
      queryClient,
      'route',
      'update',
      observabilityOncallApi.updateRoute,
      observabilityKeys.oncall.routes(),
    ),
}
