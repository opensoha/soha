import { describe, expect, it } from 'vitest'
import {
  buildWorkloadActionPath,
  buildWorkloadDetailPath,
  buildWorkloadEventsPath,
  buildWorkloadItemPath,
  buildWorkloadListPath,
  buildWorkloadMetricsPath,
  buildWorkloadYAMLPath,
} from './paths'

const scope = { clusterId: ' cluster-a ', namespace: ' team/a ' }

describe('workload paths', () => {
  it('builds scoped list and event paths through the shared scope encoder', () => {
    expect(buildWorkloadListPath('deployments', scope)).toBe(
      '/clusters/cluster-a/workloads/deployments?namespace=team%2Fa',
    )
    expect(buildWorkloadEventsPath(scope, 100)).toBe(
      '/clusters/cluster-a/events?namespace=team%2Fa&limit=100',
    )
  })

  it('encodes names for item subresources without changing the namespace', () => {
    expect(buildWorkloadItemPath('deployments', scope, 'api/server')).toBe(
      '/clusters/cluster-a/workloads/deployments/api%2Fserver?namespace=team%2Fa',
    )
    expect(buildWorkloadDetailPath('deployments', scope, 'api/server')).toContain(
      '/api%2Fserver/detail?',
    )
    expect(buildWorkloadYAMLPath('deployments', scope, 'api/server')).toContain(
      '/api%2Fserver/yaml?',
    )
    expect(buildWorkloadMetricsPath('deployments', scope, 'api/server')).toContain(
      '/api%2Fserver/metrics?',
    )
  })

  it('keeps action paths cluster scoped and validates blank segments', () => {
    expect(buildWorkloadActionPath('deployments', scope, 'restart')).toBe(
      '/clusters/cluster-a/workloads/deployments/restart',
    )
    expect(() => buildWorkloadDetailPath('deployments', scope, ' ')).toThrow(
      'A workload name is required',
    )
  })
})
