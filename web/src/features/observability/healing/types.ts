import type { ReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import type { ObservabilityPayloadMap } from '../shared/types'

export type HealingRunResult = ObservabilityPayloadMap

export interface HealingPolicy {
  id: string
  name: string
  triggerMode: string
  workflowTemplateId: string
  approvalPolicyRef?: string
  cooldownSeconds: number
  concurrencyKey?: string
  safetyWindowSeconds: number
  definition?: ReleaseDagDefinition
  enabled: boolean
}

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

export interface HealingPolicyFormValues {
  id?: string
  name: string
  triggerMode: string
  workflowTemplateId: string
  approvalPolicyRef?: string
  cooldownSeconds: number
  concurrencyKey?: string
  safetyWindowSeconds: number
  enabled: boolean
}

export interface HealingPolicyPayload {
  id?: string
  name: string
  triggerMode: string
  workflowTemplateId: string
  approvalPolicyRef: string
  cooldownSeconds: number
  concurrencyKey: string
  safetyWindowSeconds: number
  definition: ReleaseDagDefinition
  enabled: boolean
}

export interface UpdateHealingPolicyInput {
  id: string
  payload: HealingPolicyPayload
}

export interface ReviewHealingRunInput {
  id: string
  comment: string
}
