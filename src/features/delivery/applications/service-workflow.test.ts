import { describe, expect, it } from 'vitest'
import type {
  ApplicationRuntimeWorkload,
  ApplicationServiceComponent,
  DeliveryApplicationBindingSummary,
  WorkflowRun,
} from '../types'
import { serviceDeliveryKind, serviceReadiness, serviceWorkflow } from './service-workflow'

const service: ApplicationServiceComponent = {
  id: 'svc',
  applicationId: 'app',
  key: 'api',
  name: 'API',
  serviceKind: 'kubernetes_workload',
  enabled: true,
  createdAt: 'now',
  updatedAt: 'now',
}
const binding: DeliveryApplicationBindingSummary = {
  applicationEnvironmentId: 'test',
  environmentId: 'catalog-test',
  requiresApproval: false,
  targetCount: 0,
}
const workload: ApplicationRuntimeWorkload = {
  applicationEnvironmentId: 'test',
  serviceId: 'svc',
  clusterId: 'cluster',
  namespace: 'test-ns',
  workloadKind: 'Deployment',
  workloadName: 'api',
  desiredReplicas: 2,
  readyReplicas: 1,
  updatedReplicas: 1,
  availableReplicas: 1,
  healthStatus: 'progressing',
}
function run(overrides: Partial<WorkflowRun> = {}): WorkflowRun {
  return {
    id: 'run',
    applicationId: 'app',
    workflowName: 'Release',
    clusterId: 'cluster',
    namespace: 'test-ns',
    deploymentName: 'api',
    status: 'running',
    metadata: { bindingId: 'test' },
    steps: [],
    createdAt: '2026-10-25T00:00:00Z',
    updatedAt: '2026-10-25T00:00:00Z',
    ...overrides,
  }
}

describe('service workflow scope', () => {
  it('shows a run matching the exact environment workload', () => {
    const latest = run()
    expect(
      serviceWorkflow(service, 'test', { ...binding, latestWorkflow: latest }, [workload]),
    ).toBe(latest)
  })
  it.each([
    run({ applicationId: 'other' }),
    run({ metadata: { bindingId: 'production' } }),
    run({ metadata: { bindingId: 'test', applicationEnvironmentId: 'production' } }),
    run({ metadata: {} }),
    run({ metadata: { serviceId: 'other' } }),
    run({ metadata: { targetId: 'other' } }),
    run({ namespace: 'production' }),
    run({ scope: 'delivery_batch' }),
  ])('does not copy an unrelated latest environment run to this card (%j)', (latest) => {
    expect(
      serviceWorkflow(service, 'test', { ...binding, latestWorkflow: latest }, [workload]),
    ).toBeUndefined()
  })
  it('supports explicitly scoped runs before a service has runtime resources', () => {
    const latest = run({ deploymentName: '', metadata: { bindingId: 'test', serviceId: 'svc' } })
    expect(serviceWorkflow(service, 'test', { ...binding, latestWorkflow: latest }, [])).toBe(
      latest,
    )
    expect(
      serviceWorkflow(service, 'production', { ...binding, latestWorkflow: latest }, []),
    ).toBeUndefined()
  })
  it('keeps the latest matching execution snapshot without mixing services', () => {
    const old = run({ id: 'old' })
    const latest = run({ id: 'new', createdAt: '2026-10-25T01:00:00Z' })
    expect(
      serviceWorkflow(service, 'test', { ...binding, latestWorkflow: old }, [
        { ...workload, latestWorkflow: latest },
      ])?.id,
    ).toBe('new')
  })
  it('assigns concurrent environment records to each service even when the latest binding run belongs elsewhere', () => {
    const apiRun = run({ metadata: { bindingId: 'test', serviceId: 'svc' } })
    const worker = { ...service, id: 'worker' }
    const workerRun = run({
      id: 'worker-run',
      deploymentName: '',
      metadata: { bindingId: 'test', serviceId: 'worker' },
    })
    const latest = run({ id: 'elsewhere', metadata: { bindingId: 'test', serviceId: 'other' } })
    expect(
      serviceWorkflow(
        service,
        'test',
        { ...binding, latestWorkflow: latest },
        [],
        [apiRun, workerRun],
      ),
    ).toBe(apiRun)
    expect(
      serviceWorkflow(
        worker,
        'test',
        { ...binding, latestWorkflow: latest },
        [],
        [apiRun, workerRun],
      ),
    ).toBe(workerRun)
    expect(serviceWorkflow(worker, 'prod', binding, [], [workerRun])).toBeUndefined()
  })
})

describe('observed service replicas', () => {
  it('sums known native observations without inferring workflow success', () => {
    expect(serviceReadiness([workload, { ...workload, workloadKind: 'StatefulSet' }])).toEqual({
      ready: 2,
      desired: 4,
    })
  })
  it.each([
    { items: [] },
    { items: [{ ...workload, healthStatus: 'unknown' }] },
    { items: [{ ...workload, sourceStatus: 'forbidden' as const }] },
    { items: [{ ...workload, workloadKind: 'CustomResource' }] },
    { items: [{ ...workload, readyReplicas: -1 }] },
  ])('keeps missing or uncertain observations unknown', ({ items }) => {
    expect(serviceReadiness(items)).toBeUndefined()
  })
})

describe('delivery source labels', () => {
  it('uses the actual current-environment binding for each source type', () => {
    expect(
      serviceDeliveryKind(
        service,
        [
          {
            metadata: { serviceId: 'svc' },
            executorKind: 'manifest_ssa',
            targetKind: 'kustomize_overlay',
            workloadKind: 'Deployment',
          },
        ],
        [],
      ),
    ).toBe('Kustomize')
    expect(
      serviceDeliveryKind(
        service,
        [
          {
            metadata: { serviceId: 'svc' },
            executorKind: 'manifest_ssa',
            workloadKind: 'Deployment',
          },
        ],
        [],
      ),
    ).toBe('托管 YAML')
    expect(
      serviceDeliveryKind(
        service,
        [{ metadata: { serviceId: 'svc' }, workloadKind: 'CustomResource' }],
        [],
      ),
    ).toBe('Operator CR')
    expect(serviceDeliveryKind({ ...service, serviceKind: 'helm_release' }, [], [])).toBe(
      'Helm Release',
    )
    expect(
      serviceDeliveryKind(
        service,
        [
          {
            metadata: { serviceId: 'another' },
            executorKind: 'manifest_ssa',
            workloadKind: 'Deployment',
          },
        ],
        [workload],
      ),
    ).toBeUndefined()
  })
})
