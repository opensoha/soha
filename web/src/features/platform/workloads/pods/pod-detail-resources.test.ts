import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'

describe('workload relationship integration', () => {
  const root = new URL('../', import.meta.url)
  it.each([
    ['pods/pod-detail-resources.tsx', 'kind="Pod"'],
    ['deployments/deployment-detail-resources.tsx', 'kind="Deployment"'],
    ['statefulsets/statefulset-detail-resources.tsx', 'kind="StatefulSet"'],
    ['daemonsets/daemonset-detail-resources.tsx', 'kind="DaemonSet"'],
    ['jobs/detail-page.tsx', 'kind="Job"'],
    ['cronjobs/cronjob-related-resources.tsx', 'kind="CronJob"'],
    ['replicasets/detail-page.tsx', 'kind="ReplicaSet"'],
    ['replicationcontrollers/detail-page.tsx', 'kind="ReplicationController"'],
  ])('connects %s through a supported root', (file, marker) => {
    expect(readFileSync(new URL(file, root), 'utf8')).toContain(marker)
  })
  it('removes Pod/controller graph synthesis from detail fields', () => {
    for (const file of [
      'pods/pod-detail-resources.tsx',
      'shared/controller-related-resources.tsx',
    ]) {
      const source = readFileSync(new URL(file, root), 'utf8')
      expect(source).toContain('WorkloadRelationships')
      expect(source).not.toContain('detail.pods')
      expect(source).not.toContain('pod.relatedResources')
      expect(source).not.toContain('buildPodRelationships')
    }
  })
})
