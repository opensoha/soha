export const memoryKeys = {
  all: ['ai', 'memory'] as const,
  records: () => ['ai', 'memory', 'records'] as const,
  chatRecords: (ownerId: string | undefined) =>
    ['ai', 'memory', 'records', 'chat', ownerId] as const,
  chatPolicies: (ownerId: string | undefined) =>
    ['ai', 'memory', 'policies', 'chat', ownerId] as const,
  policies: () => ['ai', 'memory', 'policies'] as const,
}
export const memoryMutationKeys = {
  deleteRecord: ['ai', 'memory', 'records', 'delete'] as const,
  createPolicy: ['ai', 'memory', 'policies', 'create'] as const,
}
