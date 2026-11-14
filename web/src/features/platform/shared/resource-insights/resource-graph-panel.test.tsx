import { describe, expect, it } from 'vitest'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { buildResourceGraphFlow, focusResourceGraph } from './resource-graph-panel'

describe('buildResourceGraphFlow', () => {
  it('focuses a pod and its parent chain without including sibling replica sets', () => {
    const graph: KubernetesResourceGraph = {
      clusterId: 'cluster-a',
      generatedAt: '2026-10-30T10:00:00Z',
      rootId: 'pod',
      warnings: [],
      evidence: [],
      nodes: ['deployment', 'replicaset', 'sibling', 'pod', 'configmap'].map((id) => ({
        id,
        resource: {
          apiVersion: 'v1',
          kind: id,
          name: id,
          namespace: 'default',
          clusterId: 'cluster-a',
          scopeMode: 'namespace',
        },
      })),
      edges: [
        { id: 'owns-rs', sourceId: 'deployment', targetId: 'replicaset', relation: 'owns' },
        { id: 'owns-sibling', sourceId: 'deployment', targetId: 'sibling', relation: 'owns' },
        { id: 'owns-pod', sourceId: 'replicaset', targetId: 'pod', relation: 'owns' },
        { id: 'uses', sourceId: 'pod', targetId: 'configmap', relation: 'references' },
      ],
    }
    const focused = focusResourceGraph(graph)
    expect(focused.nodes.map((node) => node.id)).toEqual([
      'deployment',
      'replicaset',
      'pod',
      'configmap',
    ])
    expect(focused.edges).toHaveLength(3)
    expect(graph.nodes).toHaveLength(5)
    expect(focused.rootId).toBe('pod')
  })
  it('keeps backend direction and highlights the requested root', () => {
    const graph: KubernetesResourceGraph = {
      clusterId: 'cluster-a',
      generatedAt: '2026-08-23T10:00:00Z',
      rootId: 'deployment/default/api',
      nodes: [
        {
          id: 'deployment/default/api',
          resource: {
            clusterId: 'cluster-a',
            apiVersion: 'apps/v1',
            kind: 'Deployment',
            namespace: 'default',
            name: 'api',
            scopeMode: 'namespace',
          },
          status: 'healthy',
        },
        {
          id: 'pod/default/api-1',
          resource: {
            clusterId: 'cluster-a',
            apiVersion: 'v1',
            kind: 'Pod',
            namespace: 'default',
            name: 'api-1',
            scopeMode: 'namespace',
          },
        },
      ],
      edges: [
        {
          id: 'owns',
          sourceId: 'deployment/default/api',
          targetId: 'pod/default/api-1',
          relation: 'owns',
        },
      ],
      evidence: [],
      warnings: [],
    }

    const flow = buildResourceGraphFlow(graph)

    expect(flow.nodes).toHaveLength(2)
    for (const node of flow.nodes) {
      expect(node).toMatchObject({ width: 240, height: 112, style: { width: 240, height: 112 } })
    }
    expect(flow.nodes[0]?.className).toContain('is-root')
    expect(flow.edges[0]).toMatchObject({
      source: 'deployment/default/api',
      target: 'pod/default/api-1',
      label: 'owns',
    })
  })
})
