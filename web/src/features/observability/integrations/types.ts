import type { ObservabilityPayloadMap } from '../shared/types'

export interface AlertIntegration {
  id: string
  name: string
  integrationType: string
  description?: string
  token?: string
  tokenPreview?: string
  webhookPath?: string
  labelMapping?: AlertIntegrationLabelMapping
  dedupeConfig?: AlertIntegrationDedupeConfig
  enabled: boolean
  status: string
  lastError?: string
  lastReceivedAt?: string
  createdAt?: string
  updatedAt?: string
}

export type AlertIntegrationLabelMapping = ObservabilityPayloadMap
export type AlertIntegrationDedupeConfig = ObservabilityPayloadMap
export type AlertIntegrationPayload = ObservabilityPayloadMap
export type AlertIntegrationType = 'alertmanager_v1' | 'grafana_alerting_v1' | 'generic_json'

type AlertIntegrationLabelSet = Record<string, string>
type AlertIntegrationAnnotationSet = Record<string, string>

interface AlertIntegrationSampleAlert {
  status: string
  labels: AlertIntegrationLabelSet
  annotations: AlertIntegrationAnnotationSet
  startsAt: string
  generatorURL?: string
  dashboardURL?: string
}

export interface AlertmanagerSamplePayload {
  receiver: string
  status: string
  groupLabels: AlertIntegrationLabelSet
  commonLabels: AlertIntegrationLabelSet
  commonAnnotations: AlertIntegrationAnnotationSet
  externalURL: string
  alerts: AlertIntegrationSampleAlert[]
}

export interface GrafanaSamplePayload {
  receiver: string
  status: string
  title: string
  message: string
  commonLabels: AlertIntegrationLabelSet
  commonAnnotations: AlertIntegrationAnnotationSet
  externalURL: string
  alerts: AlertIntegrationSampleAlert[]
}

export interface GenericSamplePayload {
  source: string
  alerts: Array<{
    title: string
    summary: string
    severity: string
    status: string
    clusterId: string
    namespace: string
    labels: AlertIntegrationLabelSet
    annotations: AlertIntegrationAnnotationSet
    startsAt: string
  }>
}

export type AlertIntegrationSamplePayload =
  | AlertmanagerSamplePayload
  | GrafanaSamplePayload
  | GenericSamplePayload

export interface AlertIntegrationFormValues {
  id?: string
  name?: string
  integrationType?: string
  description?: string
  token?: string
  labelMapping?: string
  dedupeConfig?: string
  enabled?: boolean
}

export interface AlertIntegrationTestFormValues {
  integrationType?: string
  labelMapping?: string
  dedupeConfig?: string
  payload?: string
}

export interface AlertIntegrationUpsertPayload {
  id: string
  name: string
  integrationType: string
  description: string
  token: string
  labelMapping: AlertIntegrationLabelMapping
  dedupeConfig: AlertIntegrationDedupeConfig
  enabled: boolean
}

export interface AlertIntegrationTestPayload {
  integrationType: string
  labelMapping: AlertIntegrationLabelMapping
  dedupeConfig: AlertIntegrationDedupeConfig
  payload: AlertIntegrationPayload
}

export interface AlertIntegrationTestResult {
  integrationType: string
  source: string
  acceptedCount: number
  alerts: AlertIntegrationPayload[]
  summary?: string
}

export interface UpdateAlertIntegrationInput {
  id: string
  payload: AlertIntegrationUpsertPayload
}
