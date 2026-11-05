import { describe, expect, it } from 'vitest'
import { ingressClassParameters } from './ingressclass-configuration'
const source = (parameters: string) =>
  `apiVersion: networking.k8s.io/v1\nkind: IngressClass\nmetadata:\n  name: edge\nspec:\n${parameters}`
describe('IngressClass parameter identity and scope', () => {
  it('retains namespace and apiGroup rather than inferring a cluster resource from Kind/Name', () => {
    expect(
      ingressClassParameters(
        source(
          '  parameters:\n    apiGroup: example.io\n    kind: Config\n    name: edge-config\n    scope: Namespace\n    namespace: team-a\n',
        ),
        'edge',
      ),
    ).toEqual({
      apiGroup: 'example.io',
      kind: 'Config',
      name: 'edge-config',
      scope: 'Namespace',
      namespace: 'team-a',
    })
  })
  it('uses the API default cluster scope and distinguishes no parameters', () => {
    expect(
      ingressClassParameters(
        source('  parameters:\n    kind: Config\n    name: edge-config\n'),
        'edge',
      )?.scope,
    ).toBe('Cluster')
    expect(ingressClassParameters(source('  controller: example.io/edge\n'), 'edge')).toBeNull()
  })
  it('rejects another resource identity and malformed namespace references', () => {
    expect(() => ingressClassParameters(source(''), 'other')).toThrow('identity')
    expect(() =>
      ingressClassParameters(
        source('  parameters:\n    kind: Config\n    name: cfg\n    scope: Namespace\n'),
        'edge',
      ),
    ).toThrow('parameters')
  })
})
