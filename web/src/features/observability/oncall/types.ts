import type { Dayjs } from 'dayjs'
import type { ObservabilityPayloadMap } from '../shared/types'

export type OnCallRotationConfig = ObservabilityPayloadMap
export type OnCallAssignmentMatchers = ObservabilityPayloadMap
export type OnCallBoardView = 'calendar' | 'timeline' | 'list'
export type OnCallRotationMode = 'daily' | 'weekly' | 'custom'

export interface OnCallEscalationStepPayload extends ObservabilityPayloadMap {
  scheduleId: string
  delayMinutes: number
  role: string
  description: string
}

export interface OnCallSchedule {
  id: string
  name: string
  timeZone?: string
  description?: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface OnCallRotation {
  id: string
  scheduleId: string
  name: string
  participants?: string[]
  rotationConfig?: OnCallRotationConfig
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface OnCallEscalationPolicy {
  id: string
  name: string
  steps?: OnCallEscalationStepPayload[]
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface OnCallAssignmentRule {
  id: string
  name: string
  integrationId?: string
  integrationType?: string
  businessLineId?: string
  alertCategory?: string
  alertName?: string
  severity?: string
  service?: string
  role?: string
  matchers?: OnCallAssignmentMatchers
  targetType: 'schedule' | 'escalation'
  targetRef: string
  routeOrder: number
  groupBy?: string[]
  priority: number
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface OnCallTask {
  id: string
  eventId: string
  title: string
  summary?: string
  severity: string
  status: string
  integrationId?: string
  integrationType?: string
  clusterId?: string
  namespace?: string
  service?: string
  businessLineId?: string
  routeId?: string
  routeName?: string
  groupKey?: string
  groupBy?: string[]
  targetType?: 'schedule' | 'escalation'
  targetRef?: string
  currentParticipant?: string
  participants?: string[]
  resolutionStatus: string
  labels?: Record<string, string>
  lastSeenAt?: string
  createdAt: string
  updatedAt: string
}

export interface OnCallUser {
  id: string
  username: string
  displayName?: string
  email?: string
  status?: string
}

export interface OnCallScheduleFormValues {
  name?: string
  timeZone?: string
  description?: string
  enabled?: boolean
}

export interface OnCallSchedulePayload {
  name: string
  timeZone: string
  description: string
  enabled: boolean
}

export interface OnCallRotationFormValues {
  name: string
  scheduleId: string
  participants: string[]
  rotationMode: OnCallRotationMode
  shiftHours?: number
  startAt?: Dayjs | null
  enabled: boolean
}

export interface OnCallRotationPayload {
  name: string
  scheduleId: string
  participants: string[]
  rotationConfig: OnCallRotationConfig
  enabled: boolean
}

export interface OnCallEscalationStepFormValues {
  scheduleId?: string
  delayMinutes?: number
  role?: string
  description?: string
}

export interface OnCallEscalationPolicyFormValues {
  name: string
  steps: OnCallEscalationStepFormValues[]
  enabled: boolean
}

export interface OnCallEscalationPolicyPayload {
  name: string
  steps: OnCallEscalationStepPayload[]
  enabled: boolean
}

export interface OnCallAssignmentFormValues {
  name?: string
  integrationId?: string
  integrationType?: string
  businessLineId?: string
  alertCategory?: string
  alertName?: string
  severity?: string
  service?: string
  role?: string
  matchers?: string
  targetType?: 'schedule' | 'escalation'
  targetRef?: string
  routeOrder?: number
  groupBy?: unknown
  priority?: number
  enabled?: boolean
}

export interface OnCallAssignmentPayload {
  name: string
  integrationId: string
  integrationType: string
  businessLineId: string
  alertCategory: string
  alertName: string
  severity: string
  service: string
  role: string
  matchers: OnCallAssignmentMatchers
  targetType: 'schedule' | 'escalation'
  targetRef: string
  routeOrder: number
  groupBy: string[]
  priority: number
  enabled: boolean
}

export interface OnCallUpdateInput<T> {
  id: string
  payload: T
}

export interface OnCallDayAssignment {
  participants: string[]
  override: boolean
}

export interface ShiftSlot {
  participant: string
  start: string
  end: string
}
