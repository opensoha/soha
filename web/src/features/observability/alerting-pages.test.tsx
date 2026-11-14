/** @vitest-environment jsdom */

import dayjs from 'dayjs'
import { describe, expect, it } from 'vitest'
import { createDefaultReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import { buildAlertRulePayload, type AlertRuleFormValues } from './rules'
import { buildHealingPolicyPayload, type HealingPolicyFormValues } from './healing'
import {
  buildOnCallAssignmentPayload,
  buildOnCallEscalationPolicyPayload,
  buildOnCallRotationPayload,
  buildOnCallSchedulePayload,
  buildRotationConfigWithOverride,
  type OnCallAssignmentFormValues,
  type OnCallEscalationPolicyFormValues,
  type OnCallEscalationStepPayload,
  type OnCallRotationFormValues,
  type OnCallScheduleFormValues,
} from './oncall'

describe('observability alerting payload builders', () => {
  it('builds alert rule payload from JSON text form fields', () => {
    const values = {
      name: 'CPU pressure',
      ruleType: 'metrics',
      datasourceSelector: '{"provider":"prometheus","clusterId":"prod-a"}',
      querySpec: '{"metricKey":"cpu_usage","windowMinutes":60}',
      thresholdSpec: '{"operator":">","value":90}',
      forSeconds: 120,
      groupBy: 'clusterId, namespace, service',
      labels: '{"severity":"critical"}',
      annotations: '{"summary":"CPU too high"}',
      notificationPolicyId: 'policy-primary',
      healingPolicyIds: ['heal-restart'],
      enabled: true,
    } satisfies AlertRuleFormValues

    expect(buildAlertRulePayload(values)).toEqual({
      id: undefined,
      name: 'CPU pressure',
      ruleType: 'metrics',
      datasourceSelector: { provider: 'prometheus', clusterId: 'prod-a' },
      querySpec: { metricKey: 'cpu_usage', windowMinutes: 60 },
      thresholdSpec: { operator: '>', value: 90 },
      forSeconds: 120,
      groupBy: ['clusterId', 'namespace', 'service'],
      labels: { severity: 'critical' },
      annotations: { summary: 'CPU too high' },
      notificationPolicyId: 'policy-primary',
      healingPolicyIds: ['heal-restart'],
      enabled: true,
    })
  })

  it('builds healing policy payload with DAG definition', () => {
    const definition = createDefaultReleaseDagDefinition()
    const values = {
      name: 'Restart checkout',
      triggerMode: 'approval_then_auto',
      workflowTemplateId: 'workflow-restart',
      approvalPolicyRef: 'approval-prod',
      cooldownSeconds: 300,
      concurrencyKey: 'checkout',
      safetyWindowSeconds: 600,
      enabled: true,
    } satisfies HealingPolicyFormValues

    expect(buildHealingPolicyPayload(values, definition)).toEqual({
      id: undefined,
      name: 'Restart checkout',
      triggerMode: 'approval_then_auto',
      workflowTemplateId: 'workflow-restart',
      approvalPolicyRef: 'approval-prod',
      cooldownSeconds: 300,
      concurrencyKey: 'checkout',
      safetyWindowSeconds: 600,
      definition,
      enabled: true,
    })
  })

  it('builds on-call schedule and rotation payloads with typed rotation config', () => {
    const scheduleValues = {
      name: 'Primary schedule',
      timeZone: 'Asia/Shanghai',
      description: 'platform team',
      enabled: true,
    } satisfies OnCallScheduleFormValues
    const rotationValues = {
      name: 'Daily primary',
      scheduleId: 'schedule-primary',
      participants: ['alice', 'bob'],
      rotationMode: 'custom',
      shiftHours: 6,
      startAt: dayjs('2026-05-06T10:00:00Z'),
      enabled: true,
    } satisfies OnCallRotationFormValues

    expect(buildOnCallSchedulePayload(scheduleValues)).toEqual({
      name: 'Primary schedule',
      timeZone: 'Asia/Shanghai',
      description: 'platform team',
      enabled: true,
    })
    expect(buildOnCallRotationPayload(rotationValues, { keepExisting: true })).toEqual({
      name: 'Daily primary',
      scheduleId: 'schedule-primary',
      participants: ['alice', 'bob'],
      rotationConfig: {
        keepExisting: true,
        shiftHours: 6,
        rotationMinutes: 360,
        startAt: '2026-05-06T10:00:00.000Z',
      },
      enabled: true,
    })
  })

  it('builds escalation policy payload preserving existing step metadata', () => {
    const currentSteps = [
      { scheduleId: 'old', delayMinutes: 0, role: 'dev', description: 'old', auditTag: 'keep' },
    ] satisfies OnCallEscalationStepPayload[]
    const values = {
      name: 'Critical escalation',
      steps: [
        { scheduleId: 'schedule-primary', delayMinutes: 5, role: 'sre', description: 'page SRE' },
      ],
      enabled: true,
    } satisfies OnCallEscalationPolicyFormValues

    expect(buildOnCallEscalationPolicyPayload(values, currentSteps)).toEqual({
      name: 'Critical escalation',
      steps: [
        {
          scheduleId: 'schedule-primary',
          delayMinutes: 5,
          role: 'sre',
          description: 'page SRE',
          auditTag: 'keep',
        },
      ],
      enabled: true,
    })
  })

  it('builds on-call assignment payload from matcher JSON text', () => {
    const values = {
      name: 'Checkout critical',
      integrationId: 'am-main',
      integrationType: 'alertmanager',
      businessLineId: 'payments',
      alertCategory: 'platform',
      alertName: 'CPU',
      severity: 'critical',
      service: 'checkout',
      role: 'sre',
      matchers: '{"clusterId":"prod-a","label:team":"payments"}',
      targetType: 'escalation',
      targetRef: 'policy-critical',
      routeOrder: 10,
      groupBy: ['alertName', 'service'],
      priority: 10,
      enabled: true,
    } satisfies OnCallAssignmentFormValues

    expect(buildOnCallAssignmentPayload(values)).toEqual({
      name: 'Checkout critical',
      integrationId: 'am-main',
      integrationType: 'alertmanager',
      businessLineId: 'payments',
      alertCategory: 'platform',
      alertName: 'CPU',
      severity: 'critical',
      service: 'checkout',
      role: 'sre',
      matchers: { clusterId: 'prod-a', 'label:team': 'payments' },
      targetType: 'escalation',
      targetRef: 'policy-critical',
      routeOrder: 10,
      groupBy: ['alertName', 'service'],
      priority: 10,
      enabled: true,
    })
  })

  it('updates and clears on-call rotation overrides as typed config', () => {
    const withOverride = buildRotationConfigWithOverride(
      { shiftHours: 24, overrides: { '2026-05-06': ['alice'] } },
      '2026-05-07',
      ['bob'],
    )

    expect(withOverride).toEqual({
      shiftHours: 24,
      overrides: {
        '2026-05-06': ['alice'],
        '2026-05-07': ['bob'],
      },
    })
    expect(buildRotationConfigWithOverride(withOverride, '2026-05-06', [])).toEqual({
      shiftHours: 24,
      overrides: {
        '2026-05-07': ['bob'],
      },
    })
  })
})
