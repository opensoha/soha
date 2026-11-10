import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { manifestApi } from './api'
import { manifestKeys } from './keys'
import type { ManifestPackageInput, ManifestSource } from './types'

export const manifestMutations = {
  create: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'create'],
      mutationFn: manifestApi.create,
      onSuccess: () => queryClient.invalidateQueries({ queryKey: manifestKeys.all }),
    }),
  update: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'update'],
      mutationFn: ({ id, input }: { id: string; input: ManifestPackageInput }) =>
        manifestApi.update(id, input),
      onSuccess: () => queryClient.invalidateQueries({ queryKey: manifestKeys.all }),
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'delete'],
      mutationFn: manifestApi.remove,
      onSuccess: () => queryClient.invalidateQueries({ queryKey: manifestKeys.all }),
    }),
  saveRevision: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'save-revision'],
      mutationFn: ({ id, expectedUpdatedAt }: { id: string; expectedUpdatedAt: string }) =>
        manifestApi.saveRevision(id, expectedUpdatedAt),
      onSuccess: () => queryClient.invalidateQueries({ queryKey: manifestKeys.all }),
    }),
  publish: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'publish'],
      mutationFn: ({ id, note }: { id: string; note: string }) => manifestApi.publish(id, note),
      onSuccess: () => queryClient.invalidateQueries({ queryKey: manifestKeys.all }),
    }),
  updateSource: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'source', 'update'],
      mutationFn: ({
        id,
        input,
      }: {
        id: string
        input: Parameters<typeof manifestApi.updateSource>[1]
      }) => manifestApi.updateSource(id, input),
      onSuccess: (_item: ManifestSource, variables) =>
        queryClient.invalidateQueries({ queryKey: manifestKeys.source(variables.id) }),
    }),
  updateBinding: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'binding', 'update'],
      mutationFn: ({
        id,
        input,
      }: {
        id: string
        packageId: string
        input: Parameters<typeof manifestApi.updateBinding>[1]
      }) => manifestApi.updateBinding(id, input),
      onSuccess: (_item, variables) => {
        queryClient.invalidateQueries({ queryKey: manifestKeys.bindings(variables.packageId) })
        queryClient.invalidateQueries({ queryKey: manifestKeys.deployments(variables.packageId) })
      },
    }),
  sync: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'sync'],
      mutationFn: ({ id, generation }: { id: string; generation: number }) =>
        manifestApi.sync(id, generation),
      onSuccess: (_item, variables) => {
        queryClient.invalidateQueries({ queryKey: manifestKeys.syncRuns(variables.id) })
        queryClient.invalidateQueries({ queryKey: manifestKeys.source(variables.id) })
      },
    }),
  preflight: () =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'preflight'],
      mutationFn: ({
        id,
        bindingId,
        revision,
      }: {
        id: string
        bindingId: string
        revision: number
      }) => manifestApi.preflight(id, bindingId, revision),
    }),
  setDesiredRevision: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'desired-revision'],
      mutationFn: ({
        bindingId,
        revision,
        generation,
      }: {
        bindingId: string
        revision: number
        generation: number
      }) =>
        manifestApi.setDesiredRevision(bindingId, {
          desiredRevision: revision,
          expectedGeneration: generation,
          reconcilePolicy: 'continuous',
        }),
      onSuccess: () => queryClient.invalidateQueries({ queryKey: manifestKeys.all }),
    }),
  deploymentAction: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'deployment-action'],
      mutationFn: ({
        id,
        action,
        input,
      }: {
        id: string
        action: 'reconcile' | 'repair' | 'adopt' | 'rollback'
        input: Record<string, unknown>
      }) => manifestApi.deploymentAction(id, action, input),
      onSuccess: () => queryClient.invalidateQueries({ queryKey: manifestKeys.all }),
    }),
  decideIntent: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...manifestKeys.all, 'intent-decision'],
      mutationFn: ({
        id,
        decision,
        currentRevision,
        updatedAt,
      }: {
        id: string
        decision: 'accept' | 'reject'
        currentRevision: number
        updatedAt: string
      }) =>
        manifestApi.decideIntent(id, decision, {
          expectedCurrentRevision: currentRevision,
          expectedPackageUpdatedAt: updatedAt,
        }),
      onSuccess: () => queryClient.invalidateQueries({ queryKey: manifestKeys.all }),
    }),
}
