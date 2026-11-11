import { queryOptions } from '@tanstack/react-query'
import { knowledgeApi } from './api'
import { knowledgeKeys } from './keys'

export const knowledgeQueries = {
  bases: (enabled = true) =>
    queryOptions({
      queryKey: knowledgeKeys.bases(),
      queryFn: knowledgeApi.bases.list,
      enabled,
    }),
  sources: (baseId?: string) =>
    queryOptions({
      queryKey: knowledgeKeys.sources(baseId),
      queryFn: () => knowledgeApi.sources(baseId!),
      enabled: Boolean(baseId),
    }),
  documents: (baseId?: string) =>
    queryOptions({
      queryKey: knowledgeKeys.documents(baseId),
      queryFn: () => knowledgeApi.documents(baseId!),
      enabled: Boolean(baseId),
    }),
  syncRuns: (baseId?: string) =>
    queryOptions({
      queryKey: knowledgeKeys.syncRuns(baseId),
      queryFn: () => knowledgeApi.syncRuns(baseId!),
      enabled: Boolean(baseId),
    }),
  indexRevisions: (baseId?: string) =>
    queryOptions({
      queryKey: knowledgeKeys.indexRevisions(baseId),
      queryFn: () => knowledgeApi.indexRevisions(baseId!),
      enabled: Boolean(baseId),
    }),
}
