import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { deleteWorkload, updateWorkloadYAML } from './api'
import { workloadKeys } from './keys'
import type { UpdateWorkloadYAMLVariables, WorkloadKind, WorkloadReference } from './types'

export const workloadMutations = {
  updateYAML: (kind: WorkloadKind, queryClient: QueryClient) =>
    mutationOptions<unknown, Error, UpdateWorkloadYAMLVariables>({
      mutationKey: [...workloadKeys.resource(kind), 'update-yaml'] as const,
      mutationFn: ({ scope, name, content }) => updateWorkloadYAML(kind, scope, name, { content }),
      onSuccess: async (_data, target) => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: workloadKeys.lists(kind) }),
          queryClient.invalidateQueries({
            queryKey: workloadKeys.detail(kind, target.scope, target.name),
          }),
          queryClient.invalidateQueries({
            queryKey: workloadKeys.yaml(kind, target.scope, target.name),
          }),
          queryClient.invalidateQueries({ queryKey: workloadKeys.lists('pods') }),
        ])
      },
    }),
  remove: (kind: WorkloadKind, queryClient: QueryClient) =>
    mutationOptions<void, Error, WorkloadReference>({
      mutationKey: [...workloadKeys.resource(kind), 'delete'] as const,
      mutationFn: ({ scope, name }) => deleteWorkload(kind, scope, name),
      onSuccess: async (_data, target) => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: workloadKeys.lists(kind) }),
          queryClient.invalidateQueries({
            queryKey: workloadKeys.detail(kind, target.scope, target.name),
          }),
        ])
      },
    }),
}
