export const knowledgeProductionKeys = {
  all: ['ai', 'knowledge-production'] as const,
  connectors: () => ['ai', 'knowledge-production', 'connectors'] as const,
  jobs: () => ['ai', 'knowledge-production', 'sync-jobs'] as const,
}
export const knowledgeProductionMutationKeys = {
  createConnector: ['ai', 'knowledge-production', 'connectors', 'create'] as const,
  validateConnector: ['ai', 'knowledge-production', 'connectors', 'validate'] as const,
  startSync: ['ai', 'knowledge-production', 'sync-jobs', 'start'] as const,
  jobAction: ['ai', 'knowledge-production', 'sync-jobs', 'action'] as const,
  rebuild: ['ai', 'knowledge-production', 'rebuild'] as const,
}
