import { isValidElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { getResourceFormDefinition, resourceFormDefinitions } from './definitions'

describe('resource form registry', () => {
  it('registers the first supported resource families', () => {
    expect(Object.keys(resourceFormDefinitions)).toEqual([
      'Deployment',
      'StatefulSet',
      'DaemonSet',
      'Job',
      'CronJob',
      'Service',
      'Ingress',
      'ConfigMap',
      'Secret',
      'PersistentVolumeClaim',
      'Namespace',
      'ServiceAccount',
    ])
  })

  it('uses the selected namespace for namespaced defaults and no namespace for Namespace', () => {
    const deployment = getResourceFormDefinition('Deployment')
    const namespace = getResourceFormDefinition('Namespace')
    expect(deployment?.defaultValues({ namespace: 'minio' })).toMatchObject({ namespace: 'minio' })
    expect(namespace?.defaultValues({ namespace: 'minio' })).not.toHaveProperty('namespace')
    expect(namespace?.scopeMode).toBe('cluster')
  })

  it('uses safe workload snapshot defaults for Jobs and CronJobs', () => {
    for (const kind of ['Job', 'CronJob'] as const) {
      expect(getResourceFormDefinition(kind)?.defaultValues({ namespace: 'ops' })).toMatchObject({
        inherit: ['environment', 'storage', 'resources', 'securityContext', 'scheduling'],
      })
    }
  })

  it('returns undefined for kinds without a guided form', () => {
    expect(getResourceFormDefinition('ReplicaSet')).toBeUndefined()
  })

  it('returns a StepForm renderer without coupling it to submit transport', () => {
    const definition = getResourceFormDefinition('ConfigMap')
    const value = definition?.defaultValues({ namespace: 'minio' })
    const rendered = definition?.renderForm({
      value,
      onChange: vi.fn(),
      onSubmit: vi.fn(),
    })
    expect(isValidElement(rendered)).toBe(true)
  })
})
