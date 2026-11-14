import { queryOptions } from '@tanstack/react-query'
import { knowledgeProductionApi } from './api'
import { knowledgeProductionKeys } from './keys'
export const knowledgeProductionQueries = {
  connectors: () =>
    queryOptions({
      queryKey: knowledgeProductionKeys.connectors(),
      queryFn: knowledgeProductionApi.connectors.list,
    }),
  jobs: () =>
    queryOptions({
      queryKey: knowledgeProductionKeys.jobs(),
      queryFn: knowledgeProductionApi.jobs.list,
      refetchInterval: 10_000,
    }),
}
