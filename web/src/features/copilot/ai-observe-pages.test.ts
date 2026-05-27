import { describe, expect, it } from 'vitest'
import { automationPolicyPayload, inspectionTaskPayload, policyFormValuesFromRecord } from './ai-observe-pages'

describe('AI operations helpers', () => {
  it('normalizes inspection task form values before persistence', () => {
    expect(inspectionTaskPayload({
      title: ' 支付命名空间巡检 ',
      scopeType: 'namespace',
      clusterId: ' local-k3s ',
      namespace: ' payments ',
      checks: ['cluster_health', 'alert_pressure'],
      enabled: true,
      intervalMinutes: 1,
      analysisProfileId: ' profile:inspection ',
    })).toEqual({
      title: '支付命名空间巡检',
      scopeType: 'namespace',
      clusterId: 'local-k3s',
      namespace: 'payments',
      checks: ['cluster_health', 'alert_pressure'],
      enabled: true,
      intervalMinutes: 5,
      metadata: {
        analysisProfileId: 'profile:inspection',
      },
    })
  })

  it('omits inspection profile metadata when no profile is selected', () => {
    expect(inspectionTaskPayload({
      title: '平台巡检',
      enabled: true,
    })).toMatchObject({
      title: '平台巡检',
      metadata: {},
    })
  })

  it('normalizes automation policy form values before persistence', () => {
    expect(automationPolicyPayload({
      name: ' P1 告警根因 ',
      triggerType: ' alert_webhook ',
      analysisKinds: ['', ' root_cause ', 'performance'],
      agentProviderId: ' hermes ',
      analysisProfileId: ' profile:critical ',
      remediationPolicy: ' require_approval ',
      dedupWindowSeconds: 1,
      cooldownSeconds: 0,
      enabled: true,
      triggerSeverity: [' critical ', ''],
      triggerStatus: ['firing'],
      triggerMinDurationSeconds: 30,
      triggerLabelKey: ' service ',
      triggerLabelValue: ' payment-api ',
      triggerTimeRangeMinutes: 15,
      approvalRequired: true,
      approvalRoles: [' sre ', ''],
    })).toEqual({
      name: 'P1 告警根因',
      triggerType: 'alert_webhook',
      analysisKinds: ['root_cause', 'performance'],
      agentProviderId: 'hermes',
      triggerConditions: {
        severity: ['critical'],
        status: ['firing'],
        min_duration_seconds: 30,
        time_range_minutes: 15,
        labels: { service: 'payment-api' },
      },
      dedupWindowSeconds: 60,
      analysisProfileId: 'profile:critical',
      remediationPolicy: 'require_approval',
      approvalPolicy: {
        required: true,
        approverRoles: ['sre'],
      },
      cooldownSeconds: 60,
      enabled: true,
    })
  })

  it('defaults automation policy values to the backend-supported alert webhook path', () => {
    expect(automationPolicyPayload({
      name: '自动触发',
      analysisKinds: [],
      enabled: false,
    })).toMatchObject({
      triggerType: 'alert_webhook',
      analysisKinds: ['root_cause'],
      agentProviderId: 'internal',
      analysisProfileId: 'default',
      remediationPolicy: 'suggest_only',
      triggerConditions: {
        severity: [],
        status: [],
        min_duration_seconds: 0,
        time_range_minutes: 0,
        labels: {},
      },
      approvalPolicy: {
        required: false,
        approverRoles: [],
      },
    })
  })

  it('keeps inspection-review automation analysis kinds before persistence', () => {
    expect(automationPolicyPayload({
      name: '旧策略',
      triggerType: 'manual',
      analysisKinds: [' inspection_review ', ' trace '],
      enabled: true,
    })).toMatchObject({
      triggerType: 'alert_webhook',
      analysisKinds: ['inspection_review', 'trace'],
    })

    expect(automationPolicyPayload({
      name: '旧巡检策略',
      analysisKinds: ['inspection_review'],
      enabled: true,
    })).toMatchObject({
      analysisKinds: ['inspection_review'],
    })
  })

  it('normalizes persisted automation policy records for editing', () => {
    expect(policyFormValuesFromRecord({
      id: 'policy:legacy',
      name: '旧策略',
      enabled: true,
      triggerType: 'manual',
      analysisKinds: ['inspection_review', ' trace '],
      agentProviderId: 'hermes',
      triggerConditions: {},
      analysisProfileId: 'profile:root',
      remediationPolicy: 'suggest_only',
    })).toMatchObject({
      triggerType: 'alert_webhook',
      analysisKinds: ['inspection_review', 'trace'],
      agentProviderId: 'hermes',
    })
  })
})
