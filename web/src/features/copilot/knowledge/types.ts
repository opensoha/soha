export interface KnowledgeBase {
  id: string
  name: string
  description?: string
  status?: string
  sourceCount?: number
  documentCount?: number
  updatedAt?: string
  createdAt?: string
}

export interface KnowledgeSource {
  id: string
  knowledgeBaseId: string
  name: string
  kind: string
  status: string
  lastError?: string
  lastSyncedAt?: string
}

export interface InlineKnowledgeDocumentInput {
  externalId: string
  title: string
  content: string
  uri?: string
  acl?: { visibility?: 'private' | 'restricted' | 'public' }
}

export interface CreateKnowledgeSourceInput {
  name: string
  kind: 'inline'
  syncPolicy: { mode: 'manual' }
  config: { documents: InlineKnowledgeDocumentInput[] }
}

export interface KnowledgeDocument {
  id: string
  title: string
  uri?: string
  version?: string
  status: string
  chunkCount: number
  updatedAt?: string
}

export interface KnowledgeSyncRun {
  id: string
  sourceId: string
  status: string
  documentsSeen: number
  documentsStored: number
  chunksStored: number
  error?: string
  startedAt?: string
  completedAt?: string
}

export interface KnowledgeIndexRevision {
  id: string
  revision: number
  status: string
  embeddingModel?: string
  chunkerVersion?: string
  documentCount: number
  chunkCount: number
  createdAt?: string
  activatedAt?: string
}

export interface KnowledgeSearchHit {
  id?: string
  documentId?: string
  title?: string
  content?: string
  score?: number
  source?: string
  metadata?: Record<string, unknown>
}

export interface KnowledgeCitation {
  id?: string
  documentId?: string
  title?: string
  location?: string
  url?: string
}

export interface KnowledgeSearchResult {
  query: string
  hits: KnowledgeSearchHit[]
  citations: KnowledgeCitation[]
  timingMs?: number
  noAnswer?: boolean
}

export interface CreateKnowledgeBaseInput {
  name: string
  description?: string
  scope: {
    visibility: 'private' | 'restricted' | 'public'
  }
  retrievalPolicy: {
    defaultTopK: number
    maxTopK: number
    lexicalWeight: number
    vectorWeight: number
    minScore: number
  }
}

export interface KnowledgeSearchInput {
  query: string
  knowledgeBaseIds?: string[]
  topK?: number
  filters?: {
    sourceIds?: string[]
    documentIds?: string[]
  }
}
