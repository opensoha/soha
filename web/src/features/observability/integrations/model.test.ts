import { describe, expect, it } from 'vitest'
import {
  alertIntegrationSamplePayload,
  buildAlertIntegrationPayload,
  buildAlertIntegrationTestPayload,
} from './model'

describe('alert integration model', () => {
  it('parses form-only JSON without changing empty edit token behavior', () => {
    expect(
      buildAlertIntegrationPayload({
        id: 'am-main',
        name: 'Alertmanager Main',
        integrationType: 'alertmanager_v1',
        description: 'primary source',
        token: '',
        labelMapping: '{"clusterId":"cluster"}',
        dedupeConfig: '{"fingerprintLabels":["alertname","cluster"]}',
        enabled: true,
      }),
    ).toEqual({
      id: 'am-main',
      name: 'Alertmanager Main',
      integrationType: 'alertmanager_v1',
      description: 'primary source',
      token: '',
      labelMapping: { clusterId: 'cluster' },
      dedupeConfig: { fingerprintLabels: ['alertname', 'cluster'] },
      enabled: true,
    })
  })

  it('parses test payloads and keeps source-specific samples', () => {
    const payload = buildAlertIntegrationTestPayload({
      integrationType: 'grafana_alerting_v1',
      labelMapping: '{}',
      dedupeConfig: '{}',
      payload: alertIntegrationSamplePayload('grafana_alerting_v1'),
    })

    expect(payload).toMatchObject({
      integrationType: 'grafana_alerting_v1',
      payload: {
        receiver: 'soha',
        title: 'Grafana alert',
        alerts: [
          {
            labels: {
              alertname: 'LatencyHigh',
              rule_uid: 'rule-001',
              namespace: 'checkout',
            },
            dashboardURL: 'https://grafana.example.com/d/checkout',
          },
        ],
      },
    })
  })
})
