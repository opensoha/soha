import { emptyPayloadMap, parseObservabilityJson, toText } from '../shared/json'
import type {
  NotificationChannelConfig,
  NotificationChannelFormValues,
  NotificationChannelPayload,
  NotificationMatchers,
  NotificationPolicyFormValues,
  NotificationPolicyPayload,
  NotificationRouteFormValues,
  NotificationRoutePayload,
  NotificationSilence,
  NotificationSilenceFormValues,
  NotificationSilencePayload,
  NotificationTemplateFormValues,
  NotificationTemplatePayload,
} from './types'

function splitList(value: unknown) {
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean)
  return String(value || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function parseIsoTime(value: unknown, fieldName: string) {
  const text = String(value || '').trim()
  if (!text) throw new Error(`${fieldName}不能为空`)
  const date = new Date(text)
  if (Number.isNaN(date.getTime())) throw new Error(`${fieldName}需要 ISO 时间格式`)
  return date.toISOString()
}

export function buildNotificationPolicyPayload(
  values: NotificationPolicyFormValues,
): NotificationPolicyPayload {
  return {
    name: toText(values.name),
    matchers: parseObservabilityJson(toText(values.matchers || '{}'), emptyPayloadMap()),
    processorChain: splitList(values.processorChain),
    channelRefs: splitList(values.channelRefs),
    oncallRef: toText(values.oncallRef),
    sendResolved: Boolean(values.sendResolved),
    cooldownSeconds: Number(values.cooldownSeconds || 0),
    enabled: Boolean(values.enabled),
  }
}

export function buildNotificationTemplatePayload(
  values: NotificationTemplateFormValues,
): NotificationTemplatePayload {
  return {
    name: toText(values.name),
    templateType: toText(values.templateType),
    contentType: toText(values.contentType),
    bodyTemplate: toText(values.bodyTemplate),
    headers: parseObservabilityJson(toText(values.headers || '{}'), emptyPayloadMap()),
    queryParams: parseObservabilityJson(toText(values.queryParams || '{}'), emptyPayloadMap()),
    samplePayload: parseObservabilityJson(toText(values.samplePayload || '{}'), emptyPayloadMap()),
    enabled: Boolean(values.enabled),
  }
}

export function buildNotificationChannelPayload(
  values: NotificationChannelFormValues,
): NotificationChannelPayload {
  return {
    name: toText(values.name),
    channelType: toText(values.channelType),
    config: parseObservabilityJson(toText(values.config || '{}'), emptyPayloadMap()),
    enabled: Boolean(values.enabled),
  }
}

export function buildNotificationRoutePayload(
  values: NotificationRouteFormValues,
): NotificationRoutePayload {
  return {
    name: toText(values.name),
    matchers: parseObservabilityJson(toText(values.matchers || '{}'), emptyPayloadMap()),
    channelIds: splitList(values.channelIds),
    enabled: Boolean(values.enabled),
  }
}

export function buildNotificationSilencePayload(
  values: NotificationSilenceFormValues,
): NotificationSilencePayload {
  return {
    name: toText(values.name),
    matchers: parseObservabilityJson(toText(values.matchers || '{}'), emptyPayloadMap()),
    reason: toText(values.reason),
    startsAt: parseIsoTime(values.startsAt, '开始时间'),
    endsAt: parseIsoTime(values.endsAt, '结束时间'),
    enabled: Boolean(values.enabled),
  }
}

export function resolveChannelEndpoint(config?: NotificationChannelConfig) {
  for (const key of ['url', 'webhookUrl', 'webhook_url', 'endpoint']) {
    const value = config?.[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return '-'
}

export function stringifyNotificationMatchers(matchers?: NotificationMatchers) {
  if (!matchers || Object.keys(matchers).length === 0) return '{}'
  return JSON.stringify(matchers)
}

export function prettyNotificationJson(value: unknown) {
  if (value == null) return ''
  return JSON.stringify(value, null, 2)
}

export function shortNotificationJson(value?: NotificationMatchers) {
  if (!value || Object.keys(value).length === 0) return '{}'
  return JSON.stringify(value)
}

export function formatNotificationSilenceStatus(item: NotificationSilence) {
  if (!item.enabled) return 'disabled'
  const now = Date.now()
  const starts = new Date(item.startsAt).getTime()
  const ends = new Date(item.endsAt).getTime()
  if (Number.isFinite(starts) && now < starts) return 'scheduled'
  if (Number.isFinite(ends) && now > ends) return 'expired'
  return 'active'
}
