import type { ObservabilityQuerySnapshot } from '@opensoha/contracts/gen/ts/sohaapi'
import type { ObservabilityJsonValue, ObservabilityPayloadMap } from '../shared/types'

export interface AlertEvent {
  id: string
  ruleId?: string
  sourceType: string
  sourceSystem?: string
  fingerprint: string
  title: string
  summary: string
  severity: string
  status: string
  clusterId?: string
  namespace?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  receiver?: string
  generatorUrl?: string
  currentState?: string
  querySnapshot?: ObservabilityQuerySnapshot
  lastNotificationAt?: string
  startsAt?: string
  endsAt?: string
  lastSeenAt?: string
  createdAt: string
  updatedAt: string
}

export interface HealingPolicyOption {
  id: string
  name: string
  enabled: boolean
}

export type HealingRunResult = ObservabilityPayloadMap

export interface HealingRun {
  id: string
  policyId: string
  eventId?: string
  status: string
  approvalStatus?: string
  approvalComment?: string
  requestedBy?: string
  approvedBy?: string
  workflowRunId?: string
  workflowStatus?: string
  workflowSummary?: string
  result?: HealingRunResult
  startedAt?: string
  completedAt?: string
  createdAt: string
  updatedAt: string
}

export interface AlertNotificationPreviewItem {
  [key: string]: ObservabilityJsonValue | undefined
  channelId?: string
  templateId?: string
  url?: string
  method?: string
  contentType?: string
  body?: string
}

export type AlertDeliveryMetadata = ObservabilityPayloadMap

export interface AlertDeliveryLog {
  id: string
  alertId: string
  channelId?: string
  status: string
  summary?: string
  metadata?: AlertDeliveryMetadata
  createdAt: string
}

export interface AlertPreviewInput {
  eventId: string
  policyId: string
}

export interface HealAlertInput {
  eventId: string
  policyId: string
}
