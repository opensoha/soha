import { emptyPayloadMap, parseObservabilityJson, toText } from '../shared/json'
import type {
  AlertIntegrationFormValues,
  AlertIntegrationSamplePayload,
  AlertIntegrationTestFormValues,
  AlertIntegrationTestPayload,
  AlertIntegrationType,
  AlertIntegrationUpsertPayload,
} from './types'

export const alertIntegrationTypeOptions = [
  { value: 'alertmanager_v1', label: 'Alertmanager v1' },
  { value: 'grafana_alerting_v1', label: 'Grafana Alerting' },
  { value: 'generic_json', label: 'Generic Webhook' },
] satisfies Array<{ value: AlertIntegrationType; label: string }>

export function alertIntegrationTypeLabel(value?: string) {
  return alertIntegrationTypeOptions.find((item) => item.value === value)?.label || value || '-'
}

export function prettyObservabilityJson(value: unknown) {
  if (value == null) return ''
  return JSON.stringify(value, null, 2)
}

export function buildAlertIntegrationPayload(
  values: AlertIntegrationFormValues,
): AlertIntegrationUpsertPayload {
  return {
    id: toText(values.id),
    name: toText(values.name),
    integrationType: toText(values.integrationType),
    description: toText(values.description),
    token: toText(values.token),
    labelMapping: parseObservabilityJson(toText(values.labelMapping || '{}'), emptyPayloadMap()),
    dedupeConfig: parseObservabilityJson(toText(values.dedupeConfig || '{}'), emptyPayloadMap()),
    enabled: Boolean(values.enabled),
  }
}

export function buildAlertIntegrationTestPayload(
  values: AlertIntegrationTestFormValues,
): AlertIntegrationTestPayload {
  return {
    integrationType: toText(values.integrationType),
    labelMapping: parseObservabilityJson(toText(values.labelMapping || '{}'), emptyPayloadMap()),
    dedupeConfig: parseObservabilityJson(toText(values.dedupeConfig || '{}'), emptyPayloadMap()),
    payload: parseObservabilityJson(toText(values.payload || '{}'), emptyPayloadMap()),
  }
}

function buildAlertIntegrationSamplePayload(
  type: AlertIntegrationType | string,
  now = new Date().toISOString(),
): AlertIntegrationSamplePayload {
  if (type === 'alertmanager_v1') {
    return {
      receiver: 'soha',
      status: 'firing',
      groupLabels: { alertname: 'HighCPU' },
      commonLabels: {
        severity: 'critical',
        cluster: 'prod-a',
        namespace: 'checkout',
        service: 'api',
      },
      commonAnnotations: { summary: 'CPU 使用率过高' },
      externalURL: 'https://alertmanager.example.com',
      alerts: [
        {
          status: 'firing',
          labels: { alertname: 'HighCPU', pod: 'checkout-api-0' },
          annotations: { description: 'checkout-api CPU 使用率超过阈值' },
          startsAt: now,
          generatorURL: 'https://prometheus.example.com/graph',
        },
      ],
    }
  }
  if (type === 'grafana_alerting_v1') {
    return {
      receiver: 'soha',
      status: 'firing',
      title: 'Grafana alert',
      message: 'Grafana rule entered alerting state',
      commonLabels: { severity: 'warning', cluster: 'prod-a', service: 'checkout' },
      commonAnnotations: { summary: 'Grafana 指标异常' },
      externalURL: 'https://grafana.example.com',
      alerts: [
        {
          status: 'firing',
          labels: {
            alertname: 'LatencyHigh',
            rule_uid: 'rule-001',
            namespace: 'checkout',
          },
          annotations: { description: 'p95 延迟超过阈值' },
          startsAt: now,
          dashboardURL: 'https://grafana.example.com/d/checkout',
        },
      ],
    }
  }
  return {
    source: 'external-system',
    alerts: [
      {
        title: 'External alert',
        summary: '第三方系统告警',
        severity: 'warning',
        status: 'firing',
        clusterId: 'prod-a',
        namespace: 'checkout',
        labels: { service: 'checkout', role: 'ops' },
        annotations: { summary: '第三方系统告警' },
        startsAt: now,
      },
    ],
  }
}

export function alertIntegrationSamplePayload(type: AlertIntegrationType | string) {
  return prettyObservabilityJson(buildAlertIntegrationSamplePayload(type))
}

export function buildWebhookURL(path?: string) {
  if (!path) return ''
  if (typeof window === 'undefined') return path
  return `${window.location.origin}${path}`
}
