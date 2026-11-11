import { describe, expect, it } from 'vitest'
import {
  alertRuleConditionSummary,
  alertRuleDashboardDraft,
  alertRuleFormValues,
  buildAlertRulePayload,
} from './model'

describe('alert rule model', () => {
  it('parses JSON form fields and comma-separated group labels', () => {
    expect(
      buildAlertRulePayload({
        name: 'CPU High',
        ruleType: 'metrics',
        datasourceSelector: '{"source":"prometheus"}',
        querySpec: '{"metricKey":"cpu_usage"}',
        thresholdSpec: '{"operator":"gt","value":90}',
        forSeconds: 60,
        groupBy: 'cluster, namespace',
        labels: '{"severity":"critical"}',
        annotations: '{"summary":"CPU high"}',
        healingPolicyIds: ['heal-1'],
        enabled: true,
      }),
    ).toMatchObject({
      datasourceSelector: { source: 'prometheus' },
      groupBy: ['cluster', 'namespace'],
      labels: { severity: 'critical' },
    })
  })

  it('builds the guided metric threshold without JSON input', () => {
    expect(
      buildAlertRulePayload({
        mode: 'simple',
        name: 'CPU High',
        metricKey: 'cpu_usage',
        reducer: 'latest',
        operator: 'gte',
        thresholdValue: 85,
        windowMinutes: 15,
        stepSeconds: 60,
        clusterId: 'cluster-a',
        namespace: 'default',
        severity: 'critical',
        forSeconds: 120,
        enabled: true,
      }),
    ).toMatchObject({
      ruleType: 'metrics',
      datasourceSelector: {
        sourceKind: 'metrics',
        clusterId: 'cluster-a',
        namespace: 'default',
      },
      querySpec: { metricKey: 'cpu_usage', windowMinutes: 15, stepSeconds: 60 },
      thresholdSpec: { reducer: 'latest', operator: 'gte', value: 85 },
      labels: { severity: 'critical' },
      forSeconds: 120,
    })
  })

  it('explains a guided rule in plain language without changing its payload', () => {
    expect(
      alertRuleConditionSummary({
        metricKey: 'error_rate',
        reducer: 'average',
        operator: 'gte',
        thresholdValue: 5,
        windowMinutes: 10,
        clusterId: 'prod-a',
        namespace: 'checkout',
        workload: 'api',
        severity: 'critical',
        forSeconds: 120,
      }),
    ).toBe(
      '最近 10 分钟内，错误率的平均值大于等于 5；条件持续 120 秒后触发严重告警。范围：集群 prod-a、命名空间 checkout、工作负载 api。',
    )
  })

  it('keeps complex rules in advanced mode', () => {
    const values = alertRuleFormValues({
      id: 'rule-1',
      name: 'Custom PromQL',
      ruleType: 'metrics',
      datasourceSelector: { sourceKind: 'metrics', datasourceIds: ['prom-main'] },
      querySpec: { query: 'sum(rate(http_requests_total[5m]))' },
      thresholdSpec: { operator: 'gt', reducer: 'latest', value: 10 },
      forSeconds: 60,
      groupBy: [],
      labels: {},
      annotations: {},
      enabled: true,
      createdAt: '',
      updatedAt: '',
    })
    expect(values.mode).toBe('advanced')
    expect(values.querySpec).toContain('http_requests_total')
  })

  it('builds a bounded advanced draft from a Dashboard panel', () => {
    const draft = alertRuleDashboardDraft(
      new URLSearchParams(
        'create=dashboard-panel&dataSourceId=prom-main&name=HTTP+Errors&query=sum%28rate%28http_errors_total%5B5m%5D%29%29&windowMinutes=99999&stepSeconds=30',
      ),
    )
    expect(draft).toMatchObject({ name: 'HTTP Errors', mode: 'advanced', ruleType: 'metrics' })
    expect(JSON.parse(draft!.datasourceSelector)).toEqual({
      sourceKind: 'metrics',
      datasourceIds: ['prom-main'],
    })
    expect(JSON.parse(draft!.querySpec)).toMatchObject({ windowMinutes: 1440, stepSeconds: 30 })
  })
})
