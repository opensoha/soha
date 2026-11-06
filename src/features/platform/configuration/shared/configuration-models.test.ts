import { describe, expect, it } from 'vitest'
import { priorityClassFromYAML } from '../priorityclasses/priority-model'
import { runtimeClassFromYAML } from '../runtimeclasses/runtime-model'
import { leaseFromYAML, leaseTiming } from '../leases/lease-model'
import { mutatingWebhookFromYAML } from '../mutatingwebhookconfigurations/mutating-model'
import { validatingWebhookFromYAML } from '../validatingwebhookconfigurations/validating-model'
import { quotaConfiguration } from '../resourcequotas/configuration'
import { quotaRows } from '../resourcequotas/quota-model'
import { limitGroups } from '../limitranges/limit-model'
import { hpaConfiguration } from '../hpas/hpa-model'
import { pdbConfiguration, pdbBudgetStatus } from '../poddisruptionbudgets/pdb-model'
import { secretYAMLProperties } from '../secrets/yaml-properties'

const metadata = { name: 'demo', namespace: 'team-a' }
const named = { ...metadata, ageSeconds: 0 }
const yaml = (kind: string, apiVersion: string, fields: object = {}, cluster = false) =>
  JSON.stringify({
    apiVersion,
    kind,
    metadata: cluster ? { name: 'demo' } : metadata,
    ...fields,
  })
const cluster = (kind: string, api: string, fields: object = {}) => yaml(kind, api, fields, true)

describe('configuration source boundaries', () => {
  it.each([-2147483648, -1, 0, 2000000000, 2147483647])(
    'preserves PriorityClass int32 %s and explicit/default facts',
    (value) => {
      const item = priorityClassFromYAML(
        cluster('PriorityClass', 'scheduling.k8s.io/v1', { value }),
        'demo',
      )
      expect(item.value).toBe(value)
      expect(item.globalDefault).toBe(false)
      expect(item.globalDefaultDefaulted).toBe(true)
      expect(item.preemptionPolicy).toBe('PreemptLowerPriority')
      const never = priorityClassFromYAML(
        cluster('PriorityClass', 'scheduling.k8s.io/v1', {
          value,
          globalDefault: true,
          preemptionPolicy: 'Never',
        }),
        'demo',
      )
      expect(never.globalDefault).toBe(true)
      expect(never.preemptionPolicy).toBe('Never')
      expect(never.policyDefaulted).toBe(false)
    },
  )
  it.each([-2147483649, 2147483648, 1.5, '1'])(
    'rejects invalid PriorityClass value %s',
    (value) => {
      expect(() =>
        priorityClassFromYAML(cluster('PriorityClass', 'scheduling.k8s.io/v1', { value }), 'demo'),
      ).toThrow()
    },
  )
  it('preserves RuntimeClass quantities, empty selector values and toleration duration distinctions', () => {
    const item = runtimeClassFromYAML(
      cluster('RuntimeClass', 'node.k8s.io/v1', {
        handler: 'sandbox',
        overhead: { podFixed: { cpu: '0', memory: '64Mi', 'vendor.test/device': '2' } },
        scheduling: {
          nodeSelector: { dedicated: '' },
          tolerations: [
            { key: 'x', effect: 'NoExecute', operator: 'Exists' },
            { key: 'x', effect: 'NoExecute', tolerationSeconds: 0 },
            { key: 'x', effect: 'NoExecute', tolerationSeconds: -1 },
          ],
        },
      }),
      'demo',
    )
    expect(item.podFixed).toEqual({ cpu: '0', memory: '64Mi', 'vendor.test/device': '2' })
    expect(item.nodeSelector.dedicated).toBe('')
    expect(item.tolerations.map((term) => term.tolerationSeconds)).toEqual([undefined, 0, -1])
    expect(item.tolerations[1].operator).toBe('Equal')
  })
  it('keeps absent Lease counters distinct from zero, and estimates only from renewal records', () => {
    const bare = leaseFromYAML(
      yaml('Lease', 'coordination.k8s.io/v1', {
        spec: {
          holderIdentity: 'holder',
          acquireTime: '2026-01-01T00:00:00Z',
          leaseDurationSeconds: 60,
        },
      }),
      'demo',
      'team-a',
    )
    expect(bare.leaseTransitions).toBeUndefined()
    expect(leaseTiming(bare, Date.parse('2026-01-01T00:00:30Z')).state).toBe('unknown')
    const item = { ...bare, renewTime: '2026-01-01T00:00:00Z', leaseTransitions: 0 }
    expect(leaseTiming(item, Date.parse('2026-01-01T00:00:30Z')).state).toBe('within')
    expect(leaseTiming(item, Date.parse('2026-01-01T00:01:00Z')).state).toBe('elapsed')
    expect(leaseTiming(item, Date.parse('2025-12-31T23:59:00Z')).state).toBe('clock')
    expect(leaseTiming({ ...item, holderIdentity: '' }, Date.now()).state).toBe('unheld')
  })
  it('does not confuse quota configuration with observed usage', () => {
    const configured = quotaConfiguration(
      yaml('ResourceQuota', 'v1', { spec: { hard: { pods: '5', 'requests.cpu': '2' } } }),
      named,
    )
    const rows = quotaRows({ ...named, hard: { pods: '5' }, used: { pods: '0' } }, configured.hard)
    expect(rows.find((row) => row.resource === 'pods')).toMatchObject({ percent: 0, remaining: 5 })
    expect(rows.find((row) => row.resource === 'requests.cpu')).toMatchObject({
      configuredRaw: '2',
      hard: null,
      used: null,
      percent: null,
    })
    expect(quotaRows({ ...named, hard: { pods: '0' }, used: { pods: '1' } })[0]).toMatchObject({
      percent: null,
      remaining: -1,
      over: true,
    })
  })
  it('preserves duplicate same-type LimitRange rules as distinct resource rows', () => {
    const groups = limitGroups([
      { type: 'Container', min: { cpu: '0' } },
      { type: 'Container', max: { cpu: '2' } },
      { type: 'Pod', max: { memory: '1Gi' } },
    ])
    expect(groups.map((group) => group.type)).toEqual(['Container', 'Pod'])
    expect(new Set(groups[0].rows.map((row) => row.key)).size).toBe(2)
    expect(groups[0].rows[0].rule.min?.cpu).toBe('0')
  })
  it('matches HPA metric status by identity rather than array position and never invents zero', () => {
    const spec = {
      maxReplicas: 5,
      scaleTargetRef: { kind: 'Deployment', name: 'api' },
      metrics: ['cpu', 'memory'].map((name) => ({
        type: 'Resource',
        resource: { name, target: { type: 'Utilization', averageUtilization: 75 } },
      })),
    }
    const detail = {
      ...named,
      targetRef: 'Deployment/api',
      minReplicas: 1,
      maxReplicas: 5,
      currentReplicas: 1,
      desiredReplicas: 1,
    }
    const configuration = hpaConfiguration(
      yaml('HorizontalPodAutoscaler', 'autoscaling/v2', {
        spec,
        status: {
          currentMetrics: [
            { type: 'Resource', resource: { name: 'memory', current: { averageUtilization: 0 } } },
          ],
        },
      }),
      detail,
    )
    expect(configuration.metrics.map((metric) => metric.current)).toEqual([undefined, '0'])
    expect(configuration.currentReplicas).toBeUndefined()
    expect(configuration.minDefaulted).toBe(true)
  })
  it('distinguishes PDB nil and versioned empty selectors and unobserved budgets', () => {
    const detail = { ...named, currentHealthy: 0, desiredHealthy: 0, disruptionsAllowed: 0 }
    expect(
      pdbConfiguration(yaml('PodDisruptionBudget', 'policy/v1', { spec: {} }), detail).selectorMode,
    ).toBe('none')
    expect(
      pdbConfiguration(yaml('PodDisruptionBudget', 'policy/v1', { spec: { selector: {} } }), detail)
        .selectorMode,
    ).toBe('all')
    expect(
      pdbConfiguration(
        yaml('PodDisruptionBudget', 'policy/v1beta1', { spec: { selector: {} } }),
        detail,
      ).selectorMode,
    ).toBe('none')
    const stale = pdbConfiguration(
      yaml('PodDisruptionBudget', 'policy/v1', {
        metadata: { ...metadata, generation: 2 },
        spec: { selector: {} },
        status: { observedGeneration: 1, disruptionsAllowed: 10 },
      }),
      detail,
    )
    expect(pdbBudgetStatus(stale, false).value).toBe('checking')
  })
  it.each([mutatingWebhookFromYAML, validatingWebhookFromYAML])(
    'rejects cross-kind admission data and dual targets',
    (parse) => {
      const kind =
        parse === mutatingWebhookFromYAML
          ? 'MutatingWebhookConfiguration'
          : 'ValidatingWebhookConfiguration'
      const other =
        kind === 'MutatingWebhookConfiguration'
          ? 'ValidatingWebhookConfiguration'
          : 'MutatingWebhookConfiguration'
      expect(() =>
        parse(cluster(other, 'admissionregistration.k8s.io/v1', { webhooks: [] }), 'demo'),
      ).toThrow(/identity/)
      expect(() =>
        parse(
          cluster(kind, 'admissionregistration.k8s.io/v1', {
            webhooks: [
              {
                name: 'hook.test',
                clientConfig: {
                  url: 'https://example.test',
                  service: { name: 'api', namespace: 'team-a' },
                },
              },
            ],
          }),
          'demo',
        ),
      ).toThrow(/exactly one/)
    },
  )
  it('keeps CEL and selectors as data, excludes validating reinvocation and does not retain CA contents', () => {
    const hook = {
      name: 'hook.test',
      clientConfig: {
        service: { name: 'admission', namespace: 'team-a' },
        caBundle: 'public-ca-canary',
      },
      reinvocationPolicy: 'IfNeeded',
      namespaceSelector: {
        matchLabels: { blank: '' },
        matchExpressions: [{ key: 'env', operator: 'NotIn', values: ['test'] }],
      },
      matchConditions: [{ name: 'skip', expression: 'request.userInfo.username != "test"' }],
    }
    const mutate = mutatingWebhookFromYAML(
      cluster('MutatingWebhookConfiguration', 'admissionregistration.k8s.io/v1', {
        webhooks: [hook],
      }),
      'demo',
    )
    const validate = validatingWebhookFromYAML(
      cluster('ValidatingWebhookConfiguration', 'admissionregistration.k8s.io/v1', {
        webhooks: [hook],
      }),
      'demo',
    )
    expect(mutate.webhooks[0].reinvocationPolicy).toBe('IfNeeded')
    expect(validate.webhooks[0].reinvocationPolicy).toBeUndefined()
    expect(validate.webhooks[0].matchConditions).toEqual(hook.matchConditions)
    expect(validate.webhooks[0].namespaceSelector?.matchLabels.blank).toBe('')
    expect(JSON.stringify(validate)).not.toContain('public-ca-canary')
  })
  it('reads Secret immutability from identity-checked authorized YAML', () => {
    expect(secretYAMLProperties(yaml('Secret', 'v1'), 'demo', 'team-a').immutable).toBe(false)
    expect(secretYAMLProperties(yaml('Secret', 'v1', { immutable: true }), 'demo', 'team-a').immutable).toBe(true)
    expect(() => secretYAMLProperties(yaml('Secret', 'v1'), 'demo', 'other')).toThrow(/identity/)
    expect(() => secretYAMLProperties(yaml('ConfigMap', 'v1'), 'demo', 'team-a')).toThrow(/identity/)
    expect(() =>
      secretYAMLProperties(yaml('Secret', 'v1', { immutable: 'false' }), 'demo', 'team-a'),
    ).toThrow()
  })
  it.each([
    () => priorityClassFromYAML('x'.repeat(4 * 1024 * 1024 + 1), 'demo'),
    () => runtimeClassFromYAML('x'.repeat(4 * 1024 * 1024 + 1), 'demo'),
    () => leaseFromYAML('x'.repeat(4 * 1024 * 1024 + 1), 'demo', 'team-a'),
    () => validatingWebhookFromYAML('x'.repeat(4 * 1024 * 1024 + 1), 'demo'),
  ])('rejects oversized YAML before parsing', (parse) => expect(parse).toThrow(/budget/))
})
