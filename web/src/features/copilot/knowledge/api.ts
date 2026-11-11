import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  CreateKnowledgeBaseInput,
  CreateKnowledgeSourceInput,
  KnowledgeBase,
  KnowledgeDocument,
  KnowledgeIndexRevision,
  KnowledgeSearchInput,
  KnowledgeSearchResult,
  KnowledgeSource,
  KnowledgeSyncRun,
} from './types'

export const knowledgeApi = {
  bases: {
    list: () => api.get<ApiResponse<KnowledgeBase[]>>('/ai/knowledge-bases'),
    create: (input: CreateKnowledgeBaseInput) =>
      api.post<ApiResponse<KnowledgeBase>>('/ai/knowledge-bases', input),
    delete: (baseId: string) => api.delete<void>(`/ai/knowledge-bases/${baseId}`),
  },
  sources: (baseId: string) =>
    api.get<ApiResponse<KnowledgeSource[]>>(`/ai/knowledge-bases/${baseId}/sources`),
  createSource: (baseId: string, input: CreateKnowledgeSourceInput) =>
    api.post<ApiResponse<KnowledgeSource>>(`/ai/knowledge-bases/${baseId}/sources`, input),
  syncSource: (baseId: string, sourceId: string) =>
    api.post<ApiResponse<KnowledgeSyncRun>>(
      `/ai/knowledge-bases/${baseId}/sources/${sourceId}/sync`,
    ),
  documents: (baseId: string) =>
    api.get<ApiResponse<KnowledgeDocument[]>>(`/ai/knowledge-bases/${baseId}/documents`),
  syncRuns: (baseId: string) =>
    api.get<ApiResponse<KnowledgeSyncRun[]>>(`/ai/knowledge-bases/${baseId}/sync-runs`),
  indexRevisions: (baseId: string) =>
    api.get<ApiResponse<KnowledgeIndexRevision[]>>(`/ai/knowledge-bases/${baseId}/index-revisions`),
  search: (input: KnowledgeSearchInput) =>
    api.post<ApiResponse<KnowledgeSearchResult>>('/ai/knowledge/search', input),
}
