import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  CreateConnectorInput,
  IngestionJob,
  KnowledgeConnector,
  RebuildInput,
  StartSyncInput,
} from './types'

export const knowledgeProductionApi = {
  connectors: {
    list: () => api.get<ApiResponse<KnowledgeConnector[]>>('/ai/knowledge/connectors'),
    create: (input: CreateConnectorInput) =>
      api.post<ApiResponse<KnowledgeConnector>>('/ai/knowledge/connectors', input),
    validate: (id: string) =>
      api.post<ApiResponse<KnowledgeConnector>>(
        `/ai/knowledge/connectors/${encodeURIComponent(id)}/validate`,
      ),
  },
  jobs: {
    list: () => api.get<ApiResponse<IngestionJob[]>>('/ai/knowledge/sync-jobs'),
    start: ({ knowledgeBaseId, ...input }: StartSyncInput) =>
      api.post<ApiResponse<IngestionJob>>(
        `/ai/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/sync-jobs`,
        input,
      ),
    cancel: (id: string) =>
      api.post<ApiResponse<IngestionJob>>(
        `/ai/knowledge/sync-jobs/${encodeURIComponent(id)}/cancel`,
      ),
    retry: (id: string) =>
      api.post<ApiResponse<IngestionJob>>(
        `/ai/knowledge/sync-jobs/${encodeURIComponent(id)}/retry`,
      ),
  },
  rebuild: ({ knowledgeBaseId }: RebuildInput) =>
    api.post<ApiResponse<IngestionJob>>(
      `/ai/knowledge-bases/${encodeURIComponent(knowledgeBaseId)}/rebuild`,
    ),
}
