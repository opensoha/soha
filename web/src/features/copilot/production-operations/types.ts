import type { ProductionRecord } from '../production/operations-page'
export interface AIOperationSnapshot extends ProductionRecord {
  category: 'capacity' | 'slo' | 'backup' | 'recovery'
  status: string
  evidenceRefs?: string[]
}
export interface RunbookEvidence extends ProductionRecord {
  runbookId: string
  outcome: string
  status: string
}
export interface StartAIOperationInput {
  kind: 'backup' | 'restore' | 'index_rebuild' | 'drill'
  targetRef: string
  runbookId: string
}
