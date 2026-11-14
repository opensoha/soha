import { describe, expect, it } from 'vitest'
import {
  edgeLabel,
  filterTopology,
  topologyResourcePath,
  topologyResourceEditPath,
  topologyEntrySummary,
} from './model'
import type { TopologyNode, TopologyEdge } from './types'
const nodes: TopologyNode[] = [
  {
    id: 'route',
    type: 'rule',
    label: 'route',
    provider: 'traefik',
    resolutionState: 'resolved',
    protocol: 'HTTP',
  },
  {
    id: 'service',
    type: 'service',
    label: 'web',
    provider: 'kubernetes',
    resolutionState: 'resolved',
  },
  { id: 'pod', type: 'pod', label: 'pod', provider: 'kubernetes', resolutionState: 'resolved' },
  { id: 'other', type: 'entry', label: 'other', provider: 'apisix', resolutionState: 'unknown' },
]
const edges: TopologyEdge[] = [
  {
    id: '1',
    source: 'route',
    target: 'service',
    relation: 'weights',
    resolutionState: 'resolved',
    weight: 0,
    totalWeight: 10,
    port: '80',
  },
  { id: '2', source: 'service', target: 'pod', relation: 'resolves', resolutionState: 'resolved' },
]
describe('rule topology model', () => {
  it('shows configured route hostnames and SNI without interpreting arbitrary text as DNS', () => {
    const entry: TopologyNode = {
      ...nodes[0],
      type: 'entry',
      protocol: 'TLS',
      properties: [
        { name: 'Hostnames', value: '*.example.test, secure.example.test' },
        { name: 'Other', value: 'never-a-domain.test' },
      ],
    }
    expect(topologyEntrySummary(entry, [entry], [])).toBe(
      'SNI · *.example.test, secure.example.test',
    )
    expect(topologyEntrySummary({ ...entry, protocol: 'HTTP' }, [entry], [], 'en_US')).toBe(
      'Hostnames · *.example.test, secure.example.test',
    )
    expect(topologyEntrySummary({ ...entry, resolutionState: 'not_visible' }, [entry], [])).toBe('')
  })
  it('collects distinct hosts only from attached visible rules for an Ingress entry', () => {
    const entry: TopologyNode = { ...nodes[0], id: 'ingress', type: 'entry' }
    const rule: TopologyNode = {
      ...nodes[0],
      rule: {
        index: 0,
        matchGroups: [
          [{ type: 'host', value: 'shop.test' }],
          [
            { type: 'host', value: 'shop.test' },
            { type: 'host', value: '' },
          ],
        ],
      },
    }
    const link: TopologyEdge = {
      ...edges[0],
      source: 'ingress',
      target: rule.id,
      relation: 'matches',
    }
    expect(
      topologyEntrySummary(
        entry,
        [
          entry,
          rule,
          {
            ...rule,
            id: 'other',
            rule: { index: 1, matchGroups: [[{ type: 'host', value: 'unrelated.test' }]] },
          },
        ],
        [link],
      ),
    ).toBe('域名 · shop.test')
    expect(
      topologyEntrySummary(
        entry,
        [entry, { ...rule, resolutionState: 'not_visible' }],
        [{ ...link, port: undefined }],
      ),
    ).toBe('')
  })
  it('uses listener ports for TCP and UDP without borrowing DNS or backend service ports', () => {
    const entry: TopologyNode = {
      ...nodes[0],
      type: 'entry',
      protocol: 'TCP',
      properties: [{ name: 'Hostnames', value: 'misleading.test' }],
    }
    const match: TopologyEdge = {
      ...edges[0],
      source: 'gateway',
      target: entry.id,
      relation: 'matches',
      port: '9000',
    }
    expect(topologyEntrySummary(entry, [entry], [match, ...edges])).toBe('监听端口 · :9000')
    expect(
      topologyEntrySummary(
        { ...entry, protocol: 'UDP' },
        [entry],
        [{ ...match, port: '9001' }],
        'en_US',
      ),
    ).toBe('Listener ports · :9001')
    expect(topologyEntrySummary({ ...entry, protocol: 'UDP' }, [entry], edges)).toBe('')
  })
  it('retains backend descendants for provider filters', () => {
    expect(
      filterTopology(nodes, edges, {
        provider: 'traefik',
        protocol: '',
        keyword: '',
        issues: false,
        focus: '',
      }).nodes.map((node) => node.id),
    ).toEqual(['route', 'service', 'pod'])
  })
  it('keeps ancestors and descendants without unrelated providers', () => {
    expect(
      filterTopology(nodes, edges, {
        provider: '',
        protocol: '',
        keyword: 'web',
        issues: false,
        focus: 'service',
      }).nodes.map((node) => node.id),
    ).toEqual(['route', 'service', 'pod'])
  })
  it('ignores a focus outside the snapshot while preserving the other filters', () => {
    const filter = {
      provider: 'traefik',
      protocol: 'HTTP',
      keyword: 'route',
      issues: false,
      focus: 'previous-cluster|previous-namespace|service',
    }
    expect(filterTopology(nodes, edges, filter)).toEqual(
      filterTopology(nodes, edges, { ...filter, focus: '' }),
    )
    expect(filterTopology(nodes, edges, filter).nodes.map((node) => node.id)).toEqual([
      'route',
      'service',
      'pod',
    ])
  })
  it.each(['API.EXAMPLE.TEST', '/orders', 'backup.example.test', 'x-route'])(
    'finds a structured rule without a summary using %s',
    (keyword) => {
      const rule: TopologyNode = {
        ...nodes[0],
        provider: 'kubernetes',
        rule: {
          index: 0,
          matchGroups: [
            [
              { type: 'host', value: 'api.example.test' },
              { type: 'path', operator: 'PathPrefix', value: '/orders' },
              { type: 'headers', name: 'x-route', value: '[redacted]' },
            ],
            [{ type: 'host', value: 'backup.example.test' }],
          ],
        },
      }
      expect(
        filterTopology([rule, ...nodes.slice(1)], edges, {
          provider: '',
          protocol: '',
          keyword,
          issues: false,
          focus: '',
        }).nodes.map((node) => node.id),
      ).toEqual(['route', 'service', 'pod'])
    },
  )
  it('finds Gateway hostnames in visible properties without a rule summary', () => {
    expect(
      filterTopology(
        [
          {
            ...nodes[0],
            type: 'entry',
            provider: 'gateway',
            properties: [{ name: 'Hostnames', value: 'gateway.example.test' }],
          },
          ...nodes.slice(1),
        ],
        edges,
        { provider: 'gateway', protocol: '', keyword: 'GATEWAY.EXAMPLE', issues: false, focus: '' },
      ).nodes.map((node) => node.id),
    ).toEqual(['route', 'service', 'pod'])
  })
  it('preserves configured zero weight and disallows invisible deep links', () => {
    expect(edgeLabel(edges[0])).toContain('权重 0/10')
    expect(
      topologyResourcePath({ ...nodes[1], resolutionState: 'not_visible' }, 'c'),
    ).toBeUndefined()
  })
  it('opens existing YAML configuration only for an explicit supported resource scope', () => {
    const node = {
      ...nodes[1],
      resource: {
        clusterId: 'c',
        kind: 'Service',
        name: 'web',
        namespace: 'apps',
        apiVersion: 'v1',
        scopeMode: 'namespace' as const,
      },
    }
    expect(topologyResourceEditPath(node, 'c')).toBe(
      '/network/services/web?clusterId=c&namespace=apps&tab=yaml',
    )
    expect(topologyResourcePath(node, 'other')).toBeUndefined()
    expect(topologyResourceEditPath(node, 'other')).toBeUndefined()
    expect(
      topologyResourceEditPath({ ...node, resolutionState: 'not_visible' }, 'c'),
    ).toBeUndefined()
    expect(
      topologyResourceEditPath(
        { ...node, resource: { ...node.resource, namespace: undefined } },
        'c',
      ),
    ).toBeUndefined()
    expect(
      topologyResourceEditPath({ ...node, resource: { ...node.resource, kind: 'Pod' } }, 'c'),
    ).toBeUndefined()
  })
})

it('preserves structured edge facts when viewing incoming relationships', () => {
  const edge = {
    id: 'multi-port',
    source: 'a',
    target: 'b',
    relation: 'selects' as const,
    resolutionState: 'resolved' as const,
    protocol: 'TCP',
    port: '9443',
    sectionName: 'https',
    weight: 0,
    totalWeight: 10,
    mirrorPercent: 25,
  }
  const label = edgeLabel(edge, 'zh_CN', false)
  expect(label).toContain('被选择器匹配')
  for (const fact of ['TCP', ':9443', 'https', '权重 0/10', '镜像 25%'])
    expect(label).toContain(fact)
})
