export const knowledgeKeys = {
  all: ['ai', 'knowledge'] as const,
  bases: () => ['ai', 'knowledge', 'bases'] as const,
  sources: (baseId?: string) => ['ai', 'knowledge', 'bases', baseId, 'sources'] as const,
  documents: (baseId?: string) => ['ai', 'knowledge', 'bases', baseId, 'documents'] as const,
  syncRuns: (baseId?: string) => ['ai', 'knowledge', 'bases', baseId, 'sync-runs'] as const,
  indexRevisions: (baseId?: string) =>
    ['ai', 'knowledge', 'bases', baseId, 'index-revisions'] as const,
}

export const knowledgeMutationKeys = {
  createBase: ['ai', 'knowledge', 'bases', 'create'] as const,
  deleteBase: ['ai', 'knowledge', 'bases', 'delete'] as const,
  createSource: ['ai', 'knowledge', 'sources', 'create'] as const,
  syncSource: ['ai', 'knowledge', 'sources', 'sync'] as const,
  search: ['ai', 'knowledge', 'search'] as const,
}
