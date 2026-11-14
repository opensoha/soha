import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { knowledgeProductionApi } from './api'
import { knowledgeProductionKeys, knowledgeProductionMutationKeys } from './keys'
export const knowledgeProductionMutations = {
  createConnector: (client: QueryClient) =>
    mutationOptions({
      mutationKey: knowledgeProductionMutationKeys.createConnector,
      mutationFn: knowledgeProductionApi.connectors.create,
      onSuccess: () => client.invalidateQueries({ queryKey: knowledgeProductionKeys.connectors() }),
    }),
  validateConnector: (client: QueryClient) =>
    mutationOptions({
      mutationKey: knowledgeProductionMutationKeys.validateConnector,
      mutationFn: knowledgeProductionApi.connectors.validate,
      onSuccess: () => client.invalidateQueries({ queryKey: knowledgeProductionKeys.connectors() }),
    }),
  startSync: (client: QueryClient) =>
    mutationOptions({
      mutationKey: knowledgeProductionMutationKeys.startSync,
      mutationFn: knowledgeProductionApi.jobs.start,
      onSuccess: () => client.invalidateQueries({ queryKey: knowledgeProductionKeys.jobs() }),
    }),
  jobAction: (client: QueryClient) =>
    mutationOptions({
      mutationKey: knowledgeProductionMutationKeys.jobAction,
      mutationFn: ({ id, action }: { id: string; action: 'cancel' | 'retry' }) =>
        knowledgeProductionApi.jobs[action](id),
      onSuccess: () => client.invalidateQueries({ queryKey: knowledgeProductionKeys.jobs() }),
    }),
  rebuild: (client: QueryClient) =>
    mutationOptions({
      mutationKey: knowledgeProductionMutationKeys.rebuild,
      mutationFn: knowledgeProductionApi.rebuild,
      onSuccess: () => client.invalidateQueries({ queryKey: knowledgeProductionKeys.jobs() }),
    }),
}
