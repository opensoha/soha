export interface EvaluationDatasetSample {
  id: string
  input: string
  expectedSources?: string[]
  expectedFacts?: string[]
  forbiddenActions?: string[]
}

export interface EvaluationDataset {
  schemaVersion: 'opensoha.dev/evaluation-dataset/v1'
  id: string
  name: string
  version: string
  samples: EvaluationDatasetSample[]
  createdAt: string
}

export type EvaluationRunStatus = 'queued' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface EvaluationRun {
  schemaVersion: 'opensoha.dev/evaluation-run/v1'
  id: string
  datasetId: string
  datasetVersion: string
  candidateRefs: Record<string, string>
  status: EvaluationRunStatus
  startedAt: string
  completedAt?: string
  aggregateScores?: Record<string, number>
}

export interface EvaluationResult {
  schemaVersion: 'opensoha.dev/evaluation-result/v1'
  sampleId: string
  retrievedSources?: string[]
  producedFacts?: string[]
  actions?: string[]
  scores: Record<string, number>
  passed: boolean
  failureReasons?: string[]
}

export interface EvaluationDatasetFormValues {
  id: string
  name: string
  version: string
  sampleId: string
  input: string
  expectedSources?: string
  expectedFacts?: string
  forbiddenActions?: string
}

export interface EvaluationRunFormValues {
  id: string
  datasetKey: string
  candidateKind: string
  candidateRef: string
}
