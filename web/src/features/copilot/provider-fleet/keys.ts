export const providerFleetKeys = {
  all: ['ai', 'provider-fleet'] as const,
  rollouts: () => ['ai', 'provider-fleet', 'rollouts'] as const,
  conformance: () => ['ai', 'provider-fleet', 'conformance'] as const,
}
export const providerFleetMutationKeys = {
  rollout: ['ai', 'provider-fleet', 'rollouts', 'create'] as const,
  action: ['ai', 'provider-fleet', 'rollouts', 'action'] as const,
  conformance: ['ai', 'provider-fleet', 'conformance', 'create'] as const,
}
