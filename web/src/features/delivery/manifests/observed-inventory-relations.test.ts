import { describe, expect, it } from 'vitest'
import type {
  ManifestDeployment,
  ManifestResourceInventory,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { projectObservedManifestInventory } from './observed-inventory-relations'

const scope = { clusterId: 'cluster-a', namespace: 'team-a' }

function inventory(overrides: Partial<ManifestResourceInventory> = {}): ManifestResourceInventory {
  return {
    deploymentId: 'deployment-1',
    generation: 7,
    apiVersion: 'v1',
    kind: 'Service',
    namespace: 'team-a',
    name: 'api',
    uid: 'uid-service',
    resourceVersion: '41',
    desiredObjectDigest: 'desired-digest-sensitive',
    observedObjectDigest: 'observed-digest-sensitive',
    health: 'healthy',
    lastObservedAt: '2026-11-23T08:00:00Z',
    ...overrides,
  }
}

function deployment(
  rows: ManifestResourceInventory[],
  overrides: Partial<ManifestDeployment['status']> = {},
): ManifestDeployment {
  return {
    id: 'deployment-1',
    packageId: 'package-1',
    bindingId: 'binding-1',
    generation: 9,
    spec: {
      desiredRevision: 3,
      desiredDigest: 'spec-digest-super-secret',
      reconcilePolicy: 'continuous',
      driftPolicy: 'report',
      deletionPolicy: 'orphan',
    },
    status: {
      observedGeneration: 7,
      phase: 'converged',
      conditions: [],
      inventory: rows,
      ...overrides,
    },
    createdAt: '2026-11-23T07:00:00Z',
    updatedAt: '2026-11-23T09:00:00Z',
  }
}

describe('projectObservedManifestInventory', () => {
  it('projects a synthetic manifest root, references, and allowlisted identity fields', () => {
    const secret = {
      ...inventory({
        kind: 'Secret',
        name: 'credentials',
        uid: 'uid-secret',
        finalizers: ['finalizer-sensitive'],
        lastObservedAt: '2026-11-23T08:05:00Z',
      }),
      data: { password: 'super-secret-value' },
    } as ManifestResourceInventory & { data: Record<string, string> }
    const result = projectObservedManifestInventory(
      deployment([
        inventory({ lastObservedAt: '2026-11-23T08:01:00Z' }),
        secret,
        inventory({
          kind: 'PersistentVolumeClaim',
          name: 'data',
          uid: 'uid-pvc',
          lastObservedAt: '2026-11-23T08:04:00Z',
        }),
      ]),
      scope,
    )

    expect(result).toBeDefined()
    expect(result?.root).toEqual({
      clusterId: 'cluster-a',
      namespace: 'team-a',
      apiVersion: '',
      kind: 'ManifestDeployment',
      name: 'deployment-1',
      uid: 'soha-manifest:cluster-a/team-a/deployment-1',
      scopeMode: 'namespace',
    })
    expect(result?.nodes.map((node) => [node.resource.kind, node.role, node.depth])).toEqual([
      ['ManifestDeployment', 'root', 0],
      ['PersistentVolumeClaim', 'reference', 1],
      ['Secret', 'reference', 1],
      ['Service', 'reference', 1],
    ])
    expect(result?.edges).toEqual([
      {
        fromUid: 'soha-manifest:cluster-a/team-a/deployment-1',
        toUid: 'uid-pvc',
        type: 'references',
        ruleId: 'observed inventory',
      },
      {
        fromUid: 'soha-manifest:cluster-a/team-a/deployment-1',
        toUid: 'uid-secret',
        type: 'references',
        ruleId: 'observed inventory',
      },
      {
        fromUid: 'soha-manifest:cluster-a/team-a/deployment-1',
        toUid: 'uid-service',
        type: 'references',
        ruleId: 'observed inventory',
      },
    ])
    expect(result?.observedAt).toBe('2026-11-23T08:05:00Z')
    expect(JSON.stringify(result)).not.toMatch(
      /desired-digest|observed-digest|finalizer-sensitive|super-secret|resourceVersion/,
    )
  })

  it('deduplicates exact identity and excludes ambiguous UID or name identities', () => {
    const rows = [
      inventory({ name: 'good', uid: 'uid-good', lastObservedAt: '2026-11-23T08:01:00Z' }),
      inventory({ name: 'good', uid: 'uid-good', lastObservedAt: '2026-11-23T10:00:00Z' }),
      inventory({ name: 'uid-conflict-a', uid: 'uid-shared' }),
      inventory({ kind: 'Secret', name: 'uid-conflict-b', uid: 'uid-shared' }),
      inventory({ name: 'name-conflict', uid: 'uid-name-a' }),
      inventory({ name: 'name-conflict', uid: 'uid-name-b' }),
      inventory({ kind: 'Service', name: 'shared', uid: 'uid-service-shared' }),
      inventory({ kind: 'Secret', name: 'shared', uid: 'uid-secret-shared' }),
    ]

    const result = projectObservedManifestInventory(deployment(rows), scope)
    const reversed = projectObservedManifestInventory(deployment([...rows].reverse()), scope)

    expect(result).toEqual(reversed)
    expect(result?.nodes.map((node) => [node.resource.kind, node.resource.name])).toEqual([
      ['ManifestDeployment', 'deployment-1'],
      ['Secret', 'shared'],
      ['Service', 'good'],
      ['Service', 'shared'],
    ])
    expect(result?.nodes).toHaveLength(4)
    expect(result?.edges).toHaveLength(3)
    expect(result?.observedAt).toBe('2026-11-23T10:00:00Z')
  })

  it('ignores stale, cross-namespace, deleting, foreign, and incomplete observations', () => {
    const rows = [
      inventory({ name: 'valid', uid: 'uid-valid', lastObservedAt: '2026-11-23T08:03:00Z' }),
      inventory({ name: 'invalid-time', uid: 'uid-invalid-time', lastObservedAt: 'not-a-date' }),
      inventory({ name: 'cross-namespace', uid: 'uid-team-b', namespace: 'team-b' }),
      inventory({ name: 'stale-generation', uid: 'uid-stale', generation: 6 }),
      inventory({ name: 'foreign', uid: 'uid-foreign', deploymentId: 'deployment-2' }),
      inventory({ name: 'deleting', uid: 'uid-deleting', deletingAt: '2026-11-23T08:02:00Z' }),
      inventory({ name: 'missing-uid', uid: '' }),
      inventory({ name: 'missing-kind', uid: 'uid-missing-kind', kind: '' }),
    ]

    const result = projectObservedManifestInventory(deployment(rows), scope)
    expect(result?.nodes.map((node) => node.resource.name)).toEqual([
      'deployment-1',
      'invalid-time',
      'valid',
    ])
    expect(result?.observedAt).toBe('2026-11-23T08:03:00Z')

    expect(
      projectObservedManifestInventory(deployment([inventory({ generation: 0 })]), scope),
    ).toBeUndefined()
    expect(
      projectObservedManifestInventory(deployment([inventory()], { observedGeneration: 0 }), scope),
    ).toBeUndefined()
    expect(projectObservedManifestInventory(deployment([]), scope)).toBeUndefined()
    expect(
      projectObservedManifestInventory(
        deployment([inventory()], { observedGeneration: -1 }),
        scope,
      ),
    ).toBeUndefined()
  })
})
