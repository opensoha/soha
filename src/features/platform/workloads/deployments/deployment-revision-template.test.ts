import { describe, expect, it } from 'vitest'
import { stringify } from 'yaml'
import { readDeploymentRevisionTemplate } from './deployment-revision-template'
import { deploymentDetailState } from './deployment-detail-overview'
import type { DeploymentDetail, DeploymentRolloutStatus } from '@/types'

const record = {
  name: 'api-rs',
  namespace: 'team-a',
  revision: '12',
  replicas: 1,
  readyReplicas: 1,
}
function source() {
  return {
    kind: 'ReplicaSet',
    metadata: {
      name: 'api-rs',
      namespace: 'team-a',
      annotations: {
        'deployment.kubernetes.io/revision': '12',
        'kubernetes.io/change-cause': 'Upgrade image to v12',
      },
    },
    spec: {
      template: {
        metadata: { labels: { app: 'api', 'pod-template-hash': 'abc' } },
        spec: { containers: [{ name: 'app', image: 'api:v12' }] },
      },
    },
  }
}
describe('Deployment retained revision templates', () => {
  it('reads the actual template and change cause, excluding only the generated hash', () => {
    const result = readDeploymentRevisionTemplate(stringify(source()), record)
    expect(result.changeCause).toBe('Upgrade image to v12')
    expect(result.template).toContain('image: api:v12')
    expect(result.template).toContain('app: api')
    expect(result.template).not.toContain('pod-template-hash')
  })
  it.each(['kind', 'namespace', 'name', 'revision'])('rejects mismatched %s', (field) => {
    const value = source()
    if (field === 'kind') value.kind = 'Deployment'
    if (field === 'namespace') value.metadata.namespace = 'other'
    if (field === 'name') value.metadata.name = 'other'
    if (field === 'revision') value.metadata.annotations['deployment.kubernetes.io/revision'] = '13'
    expect(() => readDeploymentRevisionTemplate(stringify(value), record)).toThrow(
      /identity or revision/,
    )
  })
  it('rejects invalid YAML and absent templates instead of displaying an empty comparison', () => {
    expect(() => readDeploymentRevisionTemplate('kind: [', record)).toThrow()
    const value = source()
    expect(() => readDeploymentRevisionTemplate(stringify({ ...value, spec: {} }), record)).toThrow(
      /unavailable/,
    )
  })
  it('keeps an absent change cause unavailable', () => {
    const value = source()
    value.metadata.annotations['kubernetes.io/change-cause'] = ''
    expect(readDeploymentRevisionTemplate(stringify(value), record).changeCause).toBeUndefined()
  })
})
describe('Deployment observed health', () => {
  const detail = {
    desiredReplicas: 2,
    updatedReplicas: 2,
    readyReplicas: 2,
    availableReplicas: 2,
  } as DeploymentDetail
  it('does not label unavailable or incomplete replicas healthy', () => {
    expect(deploymentDetailState(detail)).toBe('healthy')
    expect(deploymentDetailState({ ...detail, availableReplicas: 1 })).toBe('degraded')
    expect(
      deploymentDetailState({ ...detail, availableReplicas: undefined as unknown as number }),
    ).toBe('unknown')
    expect(
      deploymentDetailState({ ...detail, availableReplicas: undefined as unknown as number }, {
        status: 'progressing',
      } as DeploymentRolloutStatus),
    ).toBe('unknown')
  })
  it('does not claim scale-to-zero completed while old Pods remain', () => {
    expect(
      deploymentDetailState({
        ...detail,
        desiredReplicas: 0,
        pods: [{ name: 'old' }] as DeploymentDetail['pods'],
      }),
    ).toBe('progressing')
  })
})
