import type { ProductionRecord } from '../production/operations-page'
export interface ProviderRollout extends ProductionRecord {
  desiredRevision: number
  previousRevision: number
  status: string
}
export interface ConformanceRun extends ProductionRecord {
  providerRef: string
  environmentRef: string
  suiteVersion: string
  status: string
}
export interface CreateRolloutInput {
  id: string
  desiredRevision: number
  environments: string[]
  canaryPercent: number
}
export interface CreateConformanceInput {
  id: string
  providerRef: string
  environmentRef: string
  suiteVersion: string
}
