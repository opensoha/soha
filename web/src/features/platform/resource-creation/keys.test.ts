import { describe, expect, it } from 'vitest'
import { resourceCreationKeys } from './keys'

describe('resource creation keys', () => {
  it('normalizes scope decisions and fingerprints content without storing YAML in the key', () => {
    expect(resourceCreationKeys.namespaces(' cluster-a ')).toEqual([
      'platform',
      'resource-creation',
      'namespaces',
      'cluster-a',
    ])

    const decision = resourceCreationKeys.scopeDecision(' cluster-a ', {
      namespace: ' minio ',
      resourceGroup: ' configuration ',
      kind: ' ConfigMap ',
      action: 'create',
    })
    const preflight = resourceCreationKeys.preflight('cluster-a', {
      source: 'list',
      defaultNamespace: 'minio',
      content: 'kind: ConfigMap\nmetadata:\n  name: app',
    })

    expect(decision).toEqual([
      'platform',
      'resource-creation',
      'scope-decision',
      'cluster-a',
      {
        namespace: 'minio',
        resourceGroup: 'configuration',
        apiVersion: null,
        kind: 'ConfigMap',
        action: 'create',
      },
    ])
    expect(JSON.stringify(preflight)).not.toContain('metadata')
  })
})
