import { describe, expect, it } from 'vitest'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import {
  cronJobRelationshipGraph,
  graphMatches,
  podRelationshipGraph,
  relationshipLabel,
  relationshipFacts,
  workloadKinds,
  configurationRelationshipKinds,
  networkRelationshipKinds,
  relationshipResourceDescriptors,
  relationshipResourceDescriptor,
} from './relationship-model'
const workloadApiVersion = (kind: string) =>
  ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'].includes(kind)
    ? 'apps/v1'
    : ['Job', 'CronJob'].includes(kind)
      ? 'batch/v1'
      : 'v1'
const graph: KubernetesResourceGraph = {
  clusterId: 'a',
  rootId: 'pod',
  generatedAt: '',
  warnings: [],
  evidence: [],
  nodes: [
    ['deploy', 'Deployment'],
    ['rs', 'ReplicaSet'],
    ['other', 'ReplicaSet'],
    ['pod', 'Pod'],
    ['service', 'Service'],
    ['job', 'Job'],
    ['other-job', 'Job'],
  ].map(([id, kind]) => ({
    id,
    resource: {
      clusterId: 'a',
      namespace: 'ns',
      name: id,
      kind,
      apiVersion: workloadApiVersion(kind),
      scopeMode: 'namespace' as const,
    },
  })),
  edges: [
    ['deploy', 'rs', 'controls'],
    ['deploy', 'other', 'controls'],
    ['rs', 'pod', 'controls'],
    ['service', 'pod', 'selects'],
    ['pod', 'job', 'protects'],
  ].map(([sourceId, targetId, relation], i) => ({ id: String(i), sourceId, targetId, relation })),
}
describe('server relationship model', () => {
  it.each(workloadKinds)('supports %s roots and requires an exact root identity', (kind) => {
    const rooted = {
      ...graph,
      rootId: 'root',
      nodes: [
        {
          ...graph.nodes[0],
          id: 'root',
          resource: {
            ...graph.nodes[0].resource,
            kind,
            name: 'api',
            apiVersion: workloadApiVersion(kind),
          },
        },
      ],
    }
    expect(relationshipResourceDescriptors[kind].permission).toBeTruthy()
    expect(graphMatches(rooted, kind, 'api', 'a', 'ns')).toBe(true)
    expect(graphMatches(rooted, kind, 'api', 'b', 'ns')).toBe(false)
    expect(graphMatches(rooted, kind, 'api', 'a', 'other')).toBe(false)
    expect(graphMatches({ ...rooted, clusterId: 'different' }, kind, 'api', 'a', 'ns')).toBe(false)
    const changeRoot = (patch: Partial<KubernetesResourceGraph['nodes'][number]['resource']>) => ({
      ...rooted,
      nodes: [{ ...rooted.nodes[0], resource: { ...rooted.nodes[0].resource, ...patch } }],
    })
    expect(
      graphMatches(changeRoot({ apiVersion: 'custom.example/v1' }), kind, 'api', 'a', 'ns'),
    ).toBe(false)
    expect(graphMatches(changeRoot({ scopeMode: 'cluster' }), kind, 'api', 'a', 'ns')).toBe(false)
    expect(graphMatches(changeRoot({ namespace: undefined }), kind, 'api', 'a', null)).toBe(false)
  })
  it.each([
    ['Pod', 'v1', 'namespace'],
    ['ReplicationController', 'v1', 'namespace'],
    ['Deployment', 'apps/v1', 'namespace'],
    ['StatefulSet', 'apps/v1', 'namespace'],
    ['DaemonSet', 'apps/v1', 'namespace'],
    ['ReplicaSet', 'apps/v1', 'namespace'],
    ['Job', 'batch/v1', 'namespace'],
    ['CronJob', 'batch/v1', 'namespace'],
    ['Node', 'v1', 'cluster'],
    ['Service', 'v1', 'namespace'],
    ['ConfigMap', 'v1', 'namespace'],
    ['Secret', 'v1', 'namespace'],
    ['ServiceAccount', 'v1', 'namespace'],
    ['PersistentVolumeClaim', 'v1', 'namespace'],
    ['Ingress', 'networking.k8s.io/v1', 'namespace'],
    ['HorizontalPodAutoscaler', 'autoscaling/v1', 'namespace'],
    ['HorizontalPodAutoscaler', 'autoscaling/v2', 'namespace'],
    ['PodDisruptionBudget', 'policy/v1', 'namespace'],
  ] as const)('recognizes only built-in GVK and scope for %s %s', (kind, apiVersion, scopeMode) => {
    const resource = {
      ...graph.nodes[0].resource,
      kind,
      apiVersion,
      scopeMode,
      namespace: scopeMode === 'namespace' ? 'ns' : undefined,
    }
    expect(relationshipResourceDescriptor(resource)?.permission).toBeTruthy()
    expect(
      relationshipResourceDescriptor({ ...resource, apiVersion: 'custom.example/v1' }),
    ).toBeUndefined()
    expect(
      relationshipResourceDescriptor({
        ...resource,
        scopeMode: scopeMode === 'namespace' ? 'cluster' : 'namespace',
      }),
    ).toBeUndefined()
    expect(
      relationshipResourceDescriptor({ ...resource, scopeMode: 'unknown' as typeof scopeMode }),
    ).toBeUndefined()
    expect(
      relationshipResourceDescriptor({
        ...resource,
        namespace: scopeMode === 'namespace' ? undefined : 'ns',
      }),
    ).toBeUndefined()
  })
  it.each(configurationRelationshipKinds)('accepts exact configuration root %s only', (kind) => {
    const descriptor = relationshipResourceDescriptors[kind]
    const namespace = descriptor.scope === 'namespace' ? 'ns' : undefined
    const root = {
      id: 'root',
      resource: {
        clusterId: 'a',
        kind,
        name: 'configuration',
        apiVersion: descriptor.apiVersions[0],
        scopeMode: descriptor.scope,
        namespace,
      },
    }
    const rooted = { ...graph, rootId: 'root', nodes: [root], edges: [] }
    expect(graphMatches(rooted, kind, 'configuration', 'a', namespace ?? null)).toBe(true)
    expect(graphMatches(rooted, kind, 'configuration', 'b', namespace ?? null)).toBe(false)
    expect(
      graphMatches(
        {
          ...rooted,
          nodes: [{ ...root, resource: { ...root.resource, apiVersion: 'custom.example/v1' } }],
        },
        kind,
        'configuration',
        'a',
        namespace ?? null,
      ),
    ).toBe(false)
  })
  it('keeps scope and configuration relationship directions explicit', () => {
    expect(relationshipLabel('applies-in', 'zh_CN')).toBe('作用于命名空间')
    expect(relationshipLabel('applies-in', 'zh_CN', false)).toBe('适用配置')
    expect(relationshipLabel('uses-priority-class', 'zh_CN')).toBe('使用优先级类')
    expect(relationshipLabel('uses-runtime-class', 'zh_CN', false)).toBe('运行时类用于')
    expect(relationshipLabel('webhook-service', 'en_US')).toBe('Declares webhook service')
  })
  it('rejects invalid namespaces and prototype property names without throwing', () => {
    const resource = { ...graph.nodes[0].resource, kind: 'Pod', apiVersion: 'v1' }
    for (const namespace of ['', '*', ' ns', 'UPPER', 'with/slash', 'x'.repeat(64)])
      expect(relationshipResourceDescriptor({ ...resource, namespace })).toBeUndefined()
    expect(relationshipResourceDescriptor({ ...resource, kind: 'toString' })).toBeUndefined()
  })
  it('describes CRD instance definition in both directions without implying ownership', () => {
    expect(relationshipLabel('defines', 'zh_CN')).toBe('定义实例')
    expect(relationshipLabel('defines', 'zh_CN', false)).toBe('由此定义')
    expect(relationshipLabel('defines', 'en_US')).toBe('Defines instance')
    expect(relationshipLabel('defines', 'en_US', false)).toBe('Defined by')
  })
  it('registers both directions and retains unknown wire tokens verbatim', () => {
    expect(relationshipLabel('controls', 'zh_CN')).toBe('控制')
    expect(relationshipLabel('controls', 'zh_CN', false)).toBe('受控于')
    expect(relationshipLabel('selects', 'zh_CN', false)).toBe('被选择器匹配')
    expect(relationshipLabel('custom-wire-token', 'zh_CN', false)).toBe('custom-wire-token')
    for (const token of [
      'controls',
      'owns',
      'scheduled-on',
      'uses-config',
      'uses-secret',
      'mounts',
      'uses-service-account',
      'selects',
      'routes-to',
      'scales',
      'protects',
    ])
      expect(relationshipLabel(token, 'zh_CN')).not.toBe(token)
  })
  it('filters only sibling ownership branches without rewriting incoming edge direction', () => {
    const filtered = podRelationshipGraph(graph, false)
    expect(filtered.nodes.some((node) => node.id === 'other')).toBe(false)
    expect(filtered.edges.find((edge) => edge.relation === 'selects')).toEqual(graph.edges[3])
    expect(podRelationshipGraph(graph, true)).toBe(graph)
    expect(graph.nodes).toHaveLength(7)
  })
  it('filters selected CronJob execution using server ownership, retaining references', () => {
    const jobs = {
      ...graph,
      rootId: 'deploy',
      edges: [
        ...graph.edges,
        { id: 'j1', sourceId: 'deploy', targetId: 'job', relation: 'controls' },
        { id: 'j2', sourceId: 'deploy', targetId: 'other-job', relation: 'controls' },
      ],
    }
    const selected = cronJobRelationshipGraph(jobs, 'job')
    expect(selected.nodes.some((node) => node.id === 'other-job')).toBe(false)
    expect(selected.nodes.some((node) => node.id === 'job')).toBe(true)
    expect(selected.edges.find((edge) => edge.relation === 'selects')).toEqual(graph.edges[3])
  })
})

describe('network relationship identities', () => {
  it('accepts a ReferenceGrant v1 root returned by the served API', () => {
    const resource = {
      clusterId: 'a',
      kind: 'ReferenceGrant',
      name: 'allow-backend',
      namespace: 'ns',
      apiVersion: 'gateway.networking.k8s.io/v1',
      scopeMode: 'namespace' as const,
    }
    const actual: KubernetesResourceGraph = {
      ...graph,
      rootId: 'grant-v1',
      nodes: [{ id: 'grant-v1', resource }],
      edges: [],
    }
    expect(graphMatches(actual, 'ReferenceGrant', 'allow-backend', 'a', 'ns')).toBe(true)
    expect(relationshipResourceDescriptor(resource)?.permission).toBe(
      'platform.network.reference-grants.view',
    )
    expect(
      relationshipResourceDescriptor({ ...resource, apiVersion: 'custom.example/v1' }),
    ).toBeUndefined()
  })

  it.each(networkRelationshipKinds)(
    'accepts served %s versions, rejecting custom groups and scope mismatch',
    (kind) => {
      const descriptor = relationshipResourceDescriptors[kind]
      for (const apiVersion of descriptor.apiVersions) {
        const resource = {
          clusterId: 'a',
          kind,
          name: 'root',
          apiVersion,
          scopeMode: descriptor.scope,
          namespace: descriptor.scope === 'namespace' ? 'ns' : undefined,
        }
        const rooted = { ...graph, rootId: 'root', nodes: [{ id: 'root', resource }], edges: [] }
        expect(graphMatches(rooted, kind, 'root', 'a', resource.namespace || null)).toBe(true)
        expect(
          relationshipResourceDescriptor({ ...resource, apiVersion: 'custom.example/v1' }),
        ).toBeUndefined()
        expect(
          relationshipResourceDescriptor({
            ...resource,
            scopeMode: descriptor.scope === 'namespace' ? 'cluster' : 'namespace',
          }),
        ).toBeUndefined()
      }
    },
  )
  it('labels selector, endpoint and authorization declarations independently from traffic', () => {
    expect(relationshipLabel('selects', 'en')).toBe('Selector matches')
    expect(relationshipLabel('resolves', 'en')).toBe('Endpoint association')
    expect(relationshipLabel('authorizes-reference', 'en')).toBe('Declares reference authorization')
    expect(relationshipLabel('attaches-to', 'en', false)).toBe('References gateway parent')
  })
})

describe('storage relationship identities', () => {
  it.each([
    ['PersistentVolumeClaim', 'v1', 'namespace', 'persistent-volume-claims'],
    ['PersistentVolume', 'v1', 'cluster', 'persistent-volumes'],
    ['StorageClass', 'storage.k8s.io/v1', 'cluster', 'storage-classes'],
  ] as const)(
    'accepts only the exact built-in %s identity',
    (kind, apiVersion, scopeMode, menu) => {
      const namespace = scopeMode === 'namespace' ? 'ns' : undefined
      const resource = { clusterId: 'a', kind, name: 'disk', apiVersion, scopeMode, namespace }
      const rooted = { ...graph, rootId: 'root', nodes: [{ id: 'root', resource }], edges: [] }
      const changeRoot = (
        patch: Partial<KubernetesResourceGraph['nodes'][number]['resource']>,
      ) => ({
        ...rooted,
        nodes: [{ id: 'root', resource: { ...resource, ...patch } }],
      })
      expect(relationshipResourceDescriptor(resource)?.permission).toBe(
        `platform.storage.${menu}.view`,
      )
      expect(graphMatches(rooted, kind, 'disk', 'a', namespace ?? null)).toBe(true)
      expect(graphMatches(rooted, kind, 'disk', 'a', 'another-ns')).toBe(false)
      expect(graphMatches(rooted, kind, 'another-disk', 'a', namespace ?? null)).toBe(false)
      expect(
        graphMatches(
          changeRoot({ apiVersion: 'custom.example/v1' }),
          kind,
          'disk',
          'a',
          namespace ?? null,
        ),
      ).toBe(false)
      expect(
        graphMatches(
          changeRoot({ namespace: namespace ? undefined : 'selected-ns' }),
          kind,
          'disk',
          'a',
          namespace ? null : 'selected-ns',
        ),
      ).toBe(false)
    },
  )

  it('distinguishes a volume reference from binding and storage class references', () => {
    expect(relationshipLabel('mounts', 'en')).toBe('Declares volume reference')
    expect(relationshipLabel('mounts', 'en', false)).toBe('Declared as volume by')
    expect(relationshipLabel('binds-to', 'en')).toBe('Binds to')
    expect(relationshipLabel('binds-to', 'en', false)).toBe('Bound from')
    expect(relationshipLabel('uses-storage-class', 'en')).toBe('References storage class')
    expect(relationshipLabel('uses-storage-class', 'en', false)).toBe('Storage class referenced by')
  })

  it('uses declarative wording for unresolved bindings only', () => {
    expect(relationshipLabel('binds-to', 'zh_CN', true, 'unresolved', 'spec.volumeName')).toBe(
      '声明绑定到',
    )
    expect(relationshipLabel('binds-to', 'zh_CN', false, 'unresolved', 'spec.volumeName')).toBe(
      '被声明为绑定卷',
    )
    expect(relationshipLabel('binds-to', 'en', true, 'unresolved', 'spec.volumeName')).toBe(
      'Declares binding to',
    )
    expect(relationshipLabel('binds-to', 'en', false, 'unresolved', 'spec.volumeName')).toBe(
      'Declared as the bound volume by',
    )
    expect(relationshipLabel('binds-to', 'en', true, 'resolved')).toBe('Binds to')
    expect(relationshipLabel('uses-storage-class', 'en', true, 'unresolved')).toBe(
      'References storage class',
    )
  })

  it('does not attribute unresolved bindings without a visible field path', () => {
    expect(relationshipLabel('binds-to', 'zh_CN', true, 'unresolved')).toBe('绑定待确认')
    expect(relationshipLabel('binds-to', 'en', false, 'unresolved')).toBe('Binding unconfirmed')
  })

  it('attributes an unresolved claimRef to the PV, including when viewed from the claim', () => {
    expect(relationshipLabel('binds-to', 'zh_CN', true, 'unresolved', 'spec.claimRef')).toBe(
      '被声明为卷申领',
    )
    expect(relationshipLabel('binds-to', 'zh_CN', false, 'unresolved', 'spec.claimRef')).toBe(
      '声明引用申领',
    )
    expect(relationshipLabel('binds-to', 'en', true, 'unresolved', 'spec.claimRef')).toBe(
      'Claim referenced by',
    )
    expect(relationshipLabel('binds-to', 'en', false, 'unresolved', 'spec.claimRef')).toBe(
      'Declares claim reference to',
    )
    expect(relationshipLabel('binds-to', 'zh_CN', true, 'unresolved', 'spec.volumeName')).toBe(
      '声明绑定到',
    )
    expect(relationshipLabel('binds-to', 'zh_CN', true, 'resolved', 'spec.claimRef')).toBe('绑定到')
  })
})

describe('access-control relationship identities', () => {
  it.each([
    ['ServiceAccount', 'v1', 'namespace', 'service-accounts'],
    ['ClusterRole', 'rbac.authorization.k8s.io/v1', 'cluster', 'cluster-roles'],
    ['Role', 'rbac.authorization.k8s.io/v1', 'namespace', 'roles'],
    ['ClusterRoleBinding', 'rbac.authorization.k8s.io/v1', 'cluster', 'cluster-role-bindings'],
    ['RoleBinding', 'rbac.authorization.k8s.io/v1', 'namespace', 'role-bindings'],
  ] as const)('recognizes %s by its exact API and scope', (kind, apiVersion, scopeMode, menu) => {
    const namespace = scopeMode === 'namespace' ? 'ns' : undefined
    const resource = { clusterId: 'a', kind, name: 'reader', apiVersion, scopeMode, namespace }
    const rooted = (
      candidate: KubernetesResourceGraph['nodes'][number]['resource'] = resource,
    ) => ({
      ...graph,
      rootId: 'root',
      nodes: [{ id: 'root', resource: candidate }],
      edges: [],
    })
    expect(relationshipResourceDescriptor(resource)?.permission).toBe(
      `platform.access-control.${menu}.view`,
    )
    expect(graphMatches(rooted(), kind, 'reader', 'a', namespace ?? null)).toBe(true)
    expect(graphMatches(rooted(), kind, 'reader', 'b', namespace ?? null)).toBe(false)
    expect(graphMatches(rooted(), kind, 'reader', 'a', 'other-ns')).toBe(false)
    expect(
      graphMatches(
        rooted({ ...resource, apiVersion: 'custom.example/v1' }),
        kind,
        'reader',
        'a',
        namespace ?? null,
      ),
    ).toBe(false)
    const wrongScope = { ...resource, namespace: namespace ? undefined : 'ns' }
    expect(relationshipResourceDescriptor(wrongScope)).toBeUndefined()
  })

  it.each(['User', 'Group'])('has no built-in Kubernetes resource descriptor for %s', (kind) => {
    expect(
      relationshipResourceDescriptor({
        kind,
        apiVersion: 'rbac.authorization.k8s.io/v1',
        clusterId: 'a',
        name: 'reader',
        scopeMode: 'cluster',
      }),
    ).toBeUndefined()
  })

  it.each([
    ['binds-role', 'Declares role binding', 'Referenced as the bound role by'],
    ['binds-subject', 'Declares service account subject', 'Referenced as a binding subject by'],
    ['aggregates-role', 'Aggregation selector matches', 'Matched by aggregation selector of'],
    ['uses-service-account', 'Uses service account', 'Service account used by'],
  ])('labels %s in both directions as a declaration or match', (token, outgoing, incoming) => {
    expect(relationshipLabel(token, 'en')).toBe(outgoing)
    expect(relationshipLabel(token, 'en', false)).toBe(incoming)
  })
})

it('keeps structured edge facts independent from resource health and preserves unknown resolution', () => {
  const edge = {
    id: 'a',
    sourceId: 'a',
    targetId: 'b',
    relation: 'resolves',
    resolutionState: 'no_ready_endpoints',
    protocol: 'TCP',
    port: '443',
    sectionName: 'tls',
    fieldPath: 'spec.rules[2].backendRefs[0]',
    note: 'UID differs',
  }
  expect(relationshipFacts(edge, 'en')).toEqual([
    ['Relationship resolution', 'No ready endpoints observed'],
    ['Protocol', 'TCP'],
    ['Port', '443'],
    ['SectionName', 'tls'],
    ['Declaration field', 'spec.rules[2].backendRefs[0]'],
    ['Note', 'UID differs'],
  ])
  expect(relationshipFacts({ ...edge, resolutionState: 'future' }, 'en')[0]).toEqual([
    'Relationship resolution',
    'future',
  ])
})
