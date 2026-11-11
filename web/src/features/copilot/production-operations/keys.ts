export const aiProductionOperationsKeys = {
  all: ['ai', 'production-operations'] as const,
  snapshots: () => ['ai', 'production-operations', 'snapshots'] as const,
  evidence: () => ['ai', 'production-operations', 'evidence'] as const,
}
export const aiProductionOperationsMutationKeys = {
  start: ['ai', 'production-operations', 'start'] as const,
}
