import { describe, expect, it } from 'vitest'
import {
  buildResourceCreateRequest,
  isPreflightCurrent,
  resolveCreateEntryAvailability,
  resolveResourceCreateDefaultNamespace,
  resourceCreateRequestFingerprint,
} from './model'

const scope = {
  clusterIds: ['cluster-a'],
  namespaces: ['minio'],
  resourceGroups: ['configuration'],
  resourceKinds: ['ConfigMap'],
}

describe('resource creation model', () => {
  it('uses a prepared form manifest target for preflight and execution', () => {
    expect(
      buildResourceCreateRequest(
        {
          clusterId: 'cluster-a',
          source: 'list',
          resourceGroup: 'workloads',
          expectedApiVersion: 'batch/v1',
          expectedKind: 'CronJob',
        },
        'kind: WorkloadCronJob',
        'form',
        'ops',
        {
          resourceGroup: 'extensions',
          expectedApiVersion: 'workloads.soha.io/v1alpha1',
          expectedKind: 'WorkloadCronJob',
        },
      ),
    ).toEqual({
      source: 'form',
      defaultNamespace: 'ops',
      resourceGroup: 'extensions',
      expectedApiVersion: 'workloads.soha.io/v1alpha1',
      expectedKind: 'WorkloadCronJob',
      content: 'kind: WorkloadCronJob',
    })
  })

  it('uses the namespace selected in a registry form as the request target', () => {
    expect(
      resolveResourceCreateDefaultNamespace({
        contextNamespace: undefined,
        formNamespace: ' infra ',
        mode: 'form',
      }),
    ).toBe('infra')
    expect(
      resolveResourceCreateDefaultNamespace({
        contextNamespace: 'platform',
        formNamespace: 'infra',
        mode: 'yaml',
      }),
    ).toBe('platform')
  })

  it('keeps authorization denial distinct from capability unsupported', () => {
    const denied = resolveCreateEntryAvailability({
      clusterId: 'cluster-a',
      decision: {
        allowed: false,
        reason: 'No create permission in minio',
        allowedActions: ['view'],
        resourceScope: scope,
        capability: { key: 'resource.create', status: 'available', mode: 'direct' },
      },
      isLoading: false,
      localeCode: 'en_US',
    })
    const unsupported = resolveCreateEntryAvailability({
      clusterId: 'cluster-a',
      decision: {
        allowed: true,
        allowedActions: ['create'],
        resourceScope: scope,
        capability: {
          key: 'resource.create',
          status: 'unsupported',
          mode: 'agent',
          reason: 'Agent create is unavailable',
        },
      },
      isLoading: false,
      localeCode: 'en_US',
    })

    expect(denied).toEqual({ disabled: true, reason: 'No create permission in minio' })
    expect(unsupported).toEqual({ disabled: true, reason: 'Agent create is unavailable' })
  })

  it('invalidates a successful preflight when content or target scope changes', () => {
    const preflight = { ready: true, contentHash: 'abc', items: [] }
    const request = {
      source: 'list' as const,
      defaultNamespace: 'minio',
      resourceGroup: 'configuration',
      expectedKind: 'ConfigMap',
      content: 'kind: ConfigMap',
    }
    const fingerprint = resourceCreateRequestFingerprint('cluster-a', request)

    expect(isPreflightCurrent(fingerprint, fingerprint, preflight)).toBe(true)
    expect(
      isPreflightCurrent(
        resourceCreateRequestFingerprint('cluster-b', request),
        fingerprint,
        preflight,
      ),
    ).toBe(false)
    expect(
      isPreflightCurrent(
        resourceCreateRequestFingerprint('cluster-a', {
          ...request,
          defaultNamespace: 'ops',
        }),
        fingerprint,
        preflight,
      ),
    ).toBe(false)
    expect(
      isPreflightCurrent(
        resourceCreateRequestFingerprint('cluster-a', { ...request, content: 'kind: Secret' }),
        fingerprint,
        preflight,
      ),
    ).toBe(false)
  })
})
