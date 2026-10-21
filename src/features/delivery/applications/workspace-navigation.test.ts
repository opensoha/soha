/** @vitest-environment jsdom */
import { afterEach, describe, expect, it } from 'vitest'
import {
  applicationEnvironmentSearch,
  applicationWorkspacePath,
  serviceRuntimePath,
} from './workspace-navigation'
import { environmentRuntimeStatus, workloadRuntimeStatus } from './runtime-status'
import { usePreferencesStore } from '@/stores/preferences-store'
import type { ApplicationRuntimeEnvironment, ApplicationRuntimeWorkload } from '../types'

const workload: ApplicationRuntimeWorkload = {
  applicationEnvironmentId: 'test',
  clusterId: 'cluster',
  namespace: 'default',
  workloadKind: 'Deployment',
  workloadName: 'api',
  desiredReplicas: 2,
  readyReplicas: 2,
  updatedReplicas: 2,
  availableReplicas: 2,
}
const environment: ApplicationRuntimeEnvironment = {
  applicationEnvironmentId: 'test',
  environmentId: 'test',
  requiresApproval: false,
  workloads: [],
}

afterEach(() => usePreferencesStore.setState({ applicationShortcuts: {} }))

describe('application workspace context', () => {
  it('preserves the service and content tab while removing old environment targets', () => {
    const original = new URLSearchParams(
      'tab=services&serviceId=api&serviceTab=metrics&applicationEnvironmentId=test&pod=api-1&podCluster=old&tool=terminal&metricPod=api-1&metricCluster=old&metricNamespace=test&metricRange=15&releaseId=old&settings=application',
    )
    const next = applicationEnvironmentSearch(original, 'prod')
    expect(Object.fromEntries(next)).toEqual({
      tab: 'services',
      serviceId: 'api',
      serviceTab: 'metrics',
      applicationEnvironmentId: 'prod',
    })
    expect(original.get('applicationEnvironmentId')).toBe('test')
  })
  it('encodes identities and retains environment, service and view in shared links', () => {
    const url = new URL(
      applicationWorkspacePath('app/a', 'services', 'prod + 1', 'service/a', 'build'),
      'http://localhost',
    )
    expect(url.pathname).toBe('/applications/app%2Fa')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      tab: 'services',
      applicationEnvironmentId: 'prod + 1',
      serviceId: 'service/a',
      serviceTab: 'build',
    })
    expect(serviceRuntimePath('app', 'prod', 'api')).toBe(
      '/applications/app/application-environments/prod/workloads/api?tab=pods',
    )
    expect(serviceRuntimePath('app', 'prod', 'api', 'overview')).toBe(
      '/applications/app/application-environments/prod/workloads/api?tab=pods',
    )
    expect(serviceRuntimePath('app/a', 'prod', 'api#1', 'logs')).toBe(
      '/applications/app%2Fa/application-environments/prod/workloads/api%231?tab=logs',
    )
  })

  it('separates empty deployments, unavailable clusters and unhealthy workloads', () => {
    expect(environmentRuntimeStatus(environment).label).toBe('尚未部署')
    expect(environmentRuntimeStatus(environment).tone).toBe('warning')
    expect(
      environmentRuntimeStatus({
        ...environment,
        workloads: [{ ...workload, desiredReplicas: 0, readyReplicas: 0 }],
      }).tone,
    ).toBe('warning')
    expect(
      environmentRuntimeStatus({
        ...environment,
        workloads: [{ ...workload, healthStatus: 'pending', readyReplicas: 0 }],
      }).tone,
    ).toBe('warning')
    expect(environmentRuntimeStatus({ ...environment, status: 'unavailable' }).label).toBe(
      '集群不可用',
    )
    expect(environmentRuntimeStatus({ ...environment, workloads: [workload] }).value).toBe(
      'healthy',
    )
    expect(
      environmentRuntimeStatus({ ...environment, workloads: [{ ...workload, readyReplicas: 1 }] })
        .value,
    ).toBe('degraded')
    expect(workloadRuntimeStatus({ ...workload, healthStatus: 'failed' }).value).toBe('unavailable')
    expect(workloadRuntimeStatus({ ...workload, updatedReplicas: 1 }).label).toBe('滚动更新中')
    expect(workloadRuntimeStatus({ ...workload, desiredReplicas: 0, readyReplicas: 0 }).value).toBe(
      'unknown',
    )
    expect(workloadRuntimeStatus({ ...workload, healthStatus: 'unknown' }).value).toBe('unknown')
  })

  it('isolates bookmarks by account and bounds deduplicated recent visits', () => {
    const store = usePreferencesStore.getState()
    store.toggleFavoriteApplication('developer', 'app-1')
    store.toggleFavoriteApplication('tester', 'app-2')
    for (let index = 0; index < 15; index++) store.visitApplication('developer', `app-${index}`)
    store.visitApplication('developer', 'app-10')
    const shortcuts = usePreferencesStore.getState().applicationShortcuts
    expect(shortcuts.developer.favorites).toEqual(['app-1'])
    expect(shortcuts.tester).toEqual({ favorites: ['app-2'], recent: [] })
    expect(shortcuts.developer.recent).toHaveLength(12)
    expect(shortcuts.developer.recent[0]).toBe('app-10')
    expect(new Set(shortcuts.developer.recent).size).toBe(12)
    store.toggleFavoriteApplication('developer', 'app-1')
    expect(usePreferencesStore.getState().applicationShortcuts.developer.favorites).toEqual([])
  })
})
