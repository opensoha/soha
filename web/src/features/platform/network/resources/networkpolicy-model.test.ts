import { describe, expect, it } from 'vitest'
import { policyConfiguration, policySelectorTerms } from './networkpolicy-model'
import type { NetworkPolicyDetail } from './types'
const detail: NetworkPolicyDetail = { name: 'policy', namespace: 'apps', ageSeconds: 1 }
const source = (spec: object) =>
  JSON.stringify({
    apiVersion: 'networking.k8s.io/v1',
    kind: 'NetworkPolicy',
    metadata: { name: 'policy', namespace: 'apps' },
    spec,
  })
describe('NetworkPolicy configuration semantics', () => {
  it('distinguishes isolated zero rules, an empty allow rule and an unisolated direction', () => {
    const deny = policyConfiguration(
      source({ podSelector: {}, policyTypes: ['Ingress'], ingress: [] }),
      detail,
    )
    expect(deny.directions[0]).toMatchObject({ isolated: true, rules: [] })
    expect(deny.directions[1].isolated).toBe(false)
    const allow = policyConfiguration(
      source({ podSelector: {}, policyTypes: ['Ingress'], ingress: [{}] }),
      detail,
    )
    expect(allow.directions[0].rules).toHaveLength(1)
    expect(allow.directions[0].rules[0]).toMatchObject({ peers: [], ports: [] })
  })
  it('keeps same-peer selectors together, separate peers separate, and full IP/port fields', () => {
    const config = policyConfiguration(
      source({
        podSelector: {},
        ingress: [
          {
            from: [
              { namespaceSelector: {}, podSelector: { matchLabels: { role: 'client' } } },
              { podSelector: {} },
            ],
            ports: [{ port: 'http' }],
          },
        ],
        egress: [
          {
            to: [{ ipBlock: { cidr: '203.0.113.0/24', except: ['203.0.113.128/25'] } }],
            ports: [{ protocol: 'TCP', port: 8000, endPort: 8010 }],
          },
        ],
      }),
      detail,
    )
    expect(config.directions.map((d) => d.isolated)).toEqual([true, true])
    expect(config.directions[0].rules[0].peers[0]).toEqual({
      namespaceSelector: {},
      podSelector: { matchLabels: { role: 'client' } },
    })
    expect(config.directions[0].rules[0].peers).toHaveLength(2)
    expect(config.directions[0].rules[0].ports[0].port).toBe('http')
    expect(config.directions[1].rules[0].peers[0].ipBlock).toEqual({
      cidr: '203.0.113.0/24',
      except: ['203.0.113.128/25'],
    })
    expect(config.directions[1].rules[0].ports[0].endPort).toBe(8010)
  })
  it('keeps empty selectors distinct from missing selector and preserves expressions', () => {
    expect(policySelectorTerms({})).toEqual([])
    expect(
      policySelectorTerms({
        matchExpressions: [
          { key: 'role', operator: 'In', values: ['client'] },
          { key: 'blocked', operator: 'DoesNotExist' },
        ],
      }),
    ).toEqual(['role In (client)', 'blocked DoesNotExist'])
    expect(() => policyConfiguration(source({ ingress: [] }), detail)).toThrow()
    expect(() =>
      policyConfiguration(
        source({ podSelector: {} }).replace('"name":"policy"', '"name":"other"'),
        detail,
      ),
    ).toThrow()
  })
})
