export const environmentKeys = {
  all: ['ai', 'environments'] as const,
  templates: () => ['ai', 'environments', 'templates'] as const,
  leases: () => ['ai', 'environments', 'leases'] as const,
}
export const environmentMutationKeys = {
  create: ['ai', 'environments', 'templates', 'create'] as const,
  release: ['ai', 'environments', 'leases', 'release'] as const,
  gc: ['ai', 'environments', 'gc'] as const,
}
