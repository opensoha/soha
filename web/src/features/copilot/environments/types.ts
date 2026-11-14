import type { ProductionRecord } from '../production/operations-page'
export interface EnvironmentTemplate extends ProductionRecord {
  backend: string
  isolationMode: string
  status: string
}
export interface EnvironmentLease extends ProductionRecord {
  templateId: string
  expiresAt?: string
  status: string
}
export interface CreateEnvironmentTemplateInput {
  id: string
  name: string
  backend: 'container' | 'kubernetes'
  isolationMode: string
}
