import type { ProductionRecord } from '../production/operations-page'

export interface KnowledgeConnector extends ProductionRecord {
  knowledgeBaseId: string
  kind: 'http' | 'git' | 'object' | string
  version: string
  secretRef: string
  config: Record<string, unknown>
  syncPolicy: { mode: string }
  status: string
}

export interface IngestionJob extends ProductionRecord {
  sourceId?: string
  targetRevision?: string
  stage?: string
  status: string
}

export interface CreateConnectorInput {
  knowledgeBaseId: string
  name: string
  kind: 'http' | 'git' | 'object'
  version?: string
  secretRef: string
  config: Record<string, unknown>
  syncPolicy: { mode: string }
}

export interface ConnectorFormValues {
  knowledgeBaseId: string
  name: string
  kind: 'http' | 'git' | 'object'
  version?: string
  secretRef: string
  configJson: string
}

export interface StartSyncInput {
  knowledgeBaseId: string
  sourceId: string
}
export interface RebuildInput {
  knowledgeBaseId: string
}
