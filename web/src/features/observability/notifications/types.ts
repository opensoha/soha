import type { ObservabilityPayloadMap } from '../shared/types'

export type NotificationMatchers = ObservabilityPayloadMap
export type NotificationChannelConfig = ObservabilityPayloadMap
export type NotificationTemplateHeaders = ObservabilityPayloadMap
export type NotificationTemplateQueryParams = ObservabilityPayloadMap
export type NotificationTemplateSamplePayload = ObservabilityPayloadMap
export type NotificationPreviewItem = ObservabilityPayloadMap

export interface NotificationChannel {
  id: string
  name: string
  channelType: string
  config?: NotificationChannelConfig
  enabled: boolean
  createdAt?: string
  updatedAt?: string
}

export interface NotificationRoute {
  id: string
  name: string
  matchers?: NotificationMatchers
  channelIds?: string[]
  enabled: boolean
  createdAt?: string
  updatedAt?: string
}

export interface NotificationSilence {
  id: string
  name: string
  matchers?: NotificationMatchers
  reason?: string
  startsAt: string
  endsAt: string
  enabled: boolean
  createdAt?: string
  updatedAt?: string
}

export interface NotificationPolicy {
  id: string
  name: string
  matchers?: NotificationMatchers
  processorChain?: string[]
  channelRefs?: string[]
  oncallRef?: string
  sendResolved: boolean
  cooldownSeconds: number
  enabled: boolean
}

export interface NotificationTemplate {
  id: string
  name: string
  templateType: string
  contentType: string
  bodyTemplate?: string
  headers?: NotificationTemplateHeaders
  queryParams?: NotificationTemplateQueryParams
  samplePayload?: NotificationTemplateSamplePayload
  enabled: boolean
}

export interface NotificationAlertEventOption {
  id: string
  title: string
  status: string
}

export interface NotificationOncallOption {
  id: string
  name: string
  enabled: boolean
}

export interface NotificationPolicyFormValues {
  name?: string
  matchers?: string
  processorChain?: unknown
  channelRefs?: unknown
  oncallRef?: string
  sendResolved?: boolean
  cooldownSeconds?: number
  enabled?: boolean
}

export interface NotificationTemplateFormValues {
  name?: string
  templateType?: string
  contentType?: string
  bodyTemplate?: string
  headers?: string
  queryParams?: string
  samplePayload?: string
  enabled?: boolean
}

export interface NotificationChannelFormValues {
  name?: string
  channelType?: string
  config?: string
  enabled?: boolean
}

export interface NotificationRouteFormValues {
  name?: string
  matchers?: string
  channelIds?: unknown
  enabled?: boolean
}

export interface NotificationSilenceFormValues {
  name?: string
  matchers?: string
  reason?: string
  startsAt?: string
  endsAt?: string
  enabled?: boolean
}

export interface NotificationPolicyPayload {
  name: string
  matchers: NotificationMatchers
  processorChain: string[]
  channelRefs: string[]
  oncallRef: string
  sendResolved: boolean
  cooldownSeconds: number
  enabled: boolean
}

export interface NotificationTemplatePayload {
  name: string
  templateType: string
  contentType: string
  bodyTemplate: string
  headers: NotificationTemplateHeaders
  queryParams: NotificationTemplateQueryParams
  samplePayload: NotificationTemplateSamplePayload
  enabled: boolean
}

export interface NotificationChannelPayload {
  name: string
  channelType: string
  config: NotificationChannelConfig
  enabled: boolean
}

export interface NotificationRoutePayload {
  name: string
  matchers: NotificationMatchers
  channelIds: string[]
  enabled: boolean
}

export interface NotificationSilencePayload {
  name: string
  matchers: NotificationMatchers
  reason: string
  startsAt: string
  endsAt: string
  enabled: boolean
}

export interface NotificationPreviewInput {
  policyId: string
  eventId: string
}

export interface NotificationUpdateInput<T> {
  id: string
  payload: T
}
