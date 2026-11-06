import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { hasAllowedAction } from '@/features/auth'
import { ApiError } from '@/services/api-error'
import {
  deleteHelmRelease,
  installHelmChart,
  rollbackHelmRelease,
  updateHelmReleaseValues,
} from './api'
import { helmKeys } from './keys'
import { helmQueries } from './queries'
import type { HelmReleaseRollbackVariables, UpdateHelmValuesVariables } from './types'

export const helmMutations = {
  removeRelease: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...helmKeys.all, 'delete-release'] as const,
      mutationFn: deleteHelmRelease,
      onSuccess: (_data, target) =>
        queryClient.invalidateQueries({ queryKey: helmKeys.releases(target.clusterId) }),
    }),
  updateValues: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...helmKeys.all, 'update-values'] as const,
      mutationFn: async (variables: UpdateHelmValuesVariables) => {
        const valuesKey = helmKeys.releaseValues(variables)
        await queryClient.cancelQueries({ queryKey: valuesKey })
        const latest = await queryClient.fetchQuery({
          ...helmQueries.releaseValues(variables),
          staleTime: 0,
          retry: false,
        })
        if (latest?.name !== variables.name || latest.namespace !== variables.namespace)
          throw new Error('Helm release values no longer match the selected release.')
        if (
          Number(latest.revision) !== variables.expectedRevision ||
          latest.content !== variables.expectedContent
        )
          throw new Error('Helm release values changed. Compare the latest values before applying.')
        if (!latest.editable || !hasAllowedAction(latest.allowedActions, 'update'))
          throw new Error('Updating these Helm release values is no longer allowed.')
        return updateHelmReleaseValues(variables)
      },
      onError: (error, variables) => {
        if (error instanceof ApiError && error.status === 409)
          return queryClient.invalidateQueries({ queryKey: helmKeys.releaseValues(variables) })
      },
      onSuccess: (_data, variables) =>
        Promise.all([
          queryClient.invalidateQueries({ queryKey: helmKeys.releaseValues(variables) }),
          queryClient.invalidateQueries({ queryKey: helmKeys.releaseDetail(variables) }),
          queryClient.invalidateQueries({ queryKey: helmKeys.releaseHistory(variables) }),
          queryClient.invalidateQueries({ queryKey: helmKeys.releases(variables.clusterId) }),
        ]),
    }),
  rollbackRelease: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...helmKeys.all, 'rollback-release'] as const,
      mutationFn: async (variables: HelmReleaseRollbackVariables & { currentRevision: string }) => {
        const detail = helmQueries.releaseDetail(variables)
        const history = helmQueries.releaseHistory(variables)
        await Promise.all([
          queryClient.cancelQueries({ queryKey: detail.queryKey, exact: true }),
          queryClient.cancelQueries({ queryKey: history.queryKey, exact: true }),
        ])
        const [latest, revisions] = await Promise.all([
          queryClient.fetchQuery({ ...detail, staleTime: 0, retry: false }),
          queryClient.fetchQuery({ ...history, staleTime: 0, retry: false }),
        ])
        if (
          latest?.name !== variables.name ||
          latest.namespace !== variables.namespace ||
          latest.revision !== variables.currentRevision ||
          !revisions.some(
            (record) =>
              Number(record.revision) === variables.revision &&
              record.revision !== latest.revision &&
              hasAllowedAction(record.allowedActions, 'rollback'),
          )
        )
          throw new Error(
            'Helm release revisions or rollback permissions changed. Run preflight again.',
          )
        return rollbackHelmRelease(variables)
      },
      onSuccess: (_data, variables) =>
        Promise.all([
          queryClient.invalidateQueries({ queryKey: helmKeys.releaseValues(variables) }),
          queryClient.invalidateQueries({ queryKey: helmKeys.releaseDetail(variables) }),
          queryClient.invalidateQueries({ queryKey: helmKeys.releaseHistory(variables) }),
          queryClient.invalidateQueries({ queryKey: helmKeys.releases(variables.clusterId) }),
          queryClient.invalidateQueries({
            queryKey: helmKeys.releaseManifest(variables),
          }),
        ]),
    }),
  installChart: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...helmKeys.all, 'install-chart'] as const,
      mutationFn: installHelmChart,
      onSuccess: (_data, variables) =>
        queryClient.invalidateQueries({ queryKey: helmKeys.releases(variables.clusterId) }),
    }),
}
