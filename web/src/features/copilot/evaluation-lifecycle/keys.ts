export const evaluationLifecycleKeys = {
  all: ['ai', 'evaluation-lifecycle'] as const,
  replays: () => ['ai', 'evaluation-lifecycle', 'replays'] as const,
  policies: () => ['ai', 'evaluation-lifecycle', 'gate-policies'] as const,
  feedback: () => ['ai', 'evaluation-lifecycle', 'feedback'] as const,
}
export const evaluationLifecycleMutationKeys = {
  execute: ['ai', 'evaluation-lifecycle', 'execute'] as const,
  replay: ['ai', 'evaluation-lifecycle', 'replay'] as const,
  policy: ['ai', 'evaluation-lifecycle', 'gate-policy'] as const,
  gate: ['ai', 'evaluation-lifecycle', 'gate'] as const,
  feedback: ['ai', 'evaluation-lifecycle', 'feedback', 'create'] as const,
}
