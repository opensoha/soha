export const evaluationKeys = {
  all: ['ai', 'evaluations'] as const,
  datasets: () => ['ai', 'evaluations', 'datasets'] as const,
  runs: () => ['ai', 'evaluations', 'runs'] as const,
  run: (runId: string) => ['ai', 'evaluations', 'runs', runId] as const,
  results: (runId: string) => ['ai', 'evaluations', 'runs', runId, 'results'] as const,
}

export const evaluationMutationKeys = {
  createDataset: ['ai', 'evaluations', 'datasets', 'create'] as const,
  startRun: ['ai', 'evaluations', 'runs', 'start'] as const,
}
