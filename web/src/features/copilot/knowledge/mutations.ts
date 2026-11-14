import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { knowledgeApi } from './api'
import { knowledgeKeys, knowledgeMutationKeys } from './keys'

export const knowledgeMutations = {
  createBase: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: knowledgeMutationKeys.createBase,
      mutationFn: knowledgeApi.bases.create,
      onSuccess: () => queryClient.invalidateQueries({ queryKey: knowledgeKeys.bases() }),
    }),
  deleteBase: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: knowledgeMutationKeys.deleteBase,
      mutationFn: knowledgeApi.bases.delete,
      onSuccess: () => queryClient.invalidateQueries({ queryKey: knowledgeKeys.bases() }),
    }),
  createSource: (queryClient: QueryClient, baseId: string) =>
    mutationOptions({
      mutationKey: knowledgeMutationKeys.createSource,
      mutationFn: (input: Parameters<typeof knowledgeApi.createSource>[1]) =>
        knowledgeApi.createSource(baseId, input),
      onSuccess: () => queryClient.invalidateQueries({ queryKey: knowledgeKeys.sources(baseId) }),
    }),
  syncSource: (queryClient: QueryClient, baseId: string) =>
    mutationOptions({
      mutationKey: knowledgeMutationKeys.syncSource,
      mutationFn: (sourceId: string) => knowledgeApi.syncSource(baseId, sourceId),
      onSuccess: () =>
        Promise.all([
          queryClient.invalidateQueries({ queryKey: knowledgeKeys.sources(baseId) }),
          queryClient.invalidateQueries({ queryKey: knowledgeKeys.documents(baseId) }),
          queryClient.invalidateQueries({ queryKey: knowledgeKeys.syncRuns(baseId) }),
          queryClient.invalidateQueries({ queryKey: knowledgeKeys.indexRevisions(baseId) }),
        ]),
    }),
  search: () =>
    mutationOptions({
      mutationKey: knowledgeMutationKeys.search,
      mutationFn: knowledgeApi.search,
    }),
}
