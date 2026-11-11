import { describe, expect, it } from 'vitest'
import { httpRouteConfiguration, httpRouteModel } from './httproute-configuration'
import type { HTTPRouteDetail } from './types'

const content = `apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: app
  namespace: team-a
  generation: 5
spec:
  parentRefs:
    - name: edge
      namespace: gateways
      sectionName: https
    - name: denied
  rules:
    - matches:
        - path: {type: Exact, value: /v1}
          method: GET
          headers: [{name: X-Version, value: v1}]
          queryParams: [{name: mode, type: RegularExpression, value: "v.*"}]
        - path: {value: /v2}
      backendRefs:
        - {name: api, namespace: backend-ns, port: 8080, weight: 0}
        - {name: api, namespace: backend-ns, port: 8081}
        - {name: denied, port: 8080}
    - filters:
        - type: RequestRedirect
          requestRedirect: {scheme: https, statusCode: 301}
status:
  parents:
    - parentRef: {name: edge, namespace: gateways, sectionName: https}
      controllerName: example.com/a
      conditions: [{type: Accepted, status: 'True', observedGeneration: 4}]
    - parentRef: {name: edge, namespace: gateways, sectionName: https}
      controllerName: example.com/b
      conditions: [{type: Accepted, status: 'False', reason: UnsupportedValue, observedGeneration: 5}]
    - parentRef: {name: denied}
      controllerName: example.com/denied
      conditions: [{type: Accepted, status: 'True'}]
`
const detail: HTTPRouteDetail = {
  name: 'app',
  namespace: 'team-a',
  ageSeconds: 1,
  parentRefs: ['gateways/edge'],
  parentStatuses: [
    { parentRef: 'gateways:edge', controllerName: 'example.com/a' },
    { parentRef: 'gateways:edge', controllerName: 'example.com/b' },
  ],
  rules: [
    {
      backends: [
        { kind: 'Service', namespace: 'backend-ns', name: 'api', port: 8080, weight: 0 },
        { kind: 'Service', namespace: 'backend-ns', name: 'api', port: 8081, weight: 0 },
      ],
    },
    { filters: ['RequestRedirect'] },
  ],
}
describe('HTTPRoute configuration model', () => {
  it('keeps match alternatives separate from simultaneous requirements', () => {
    const model = httpRouteModel(detail, httpRouteConfiguration(content, 'app', 'team-a'))
    expect(model.rules[0].matches).toEqual([
      [
        'Exact /v1',
        'Method GET',
        'Header Exact · X-Version = v1',
        'Query RegularExpression · mode = v.*',
      ],
      ['PathPrefix /v2'],
    ])
    expect(model.rules[1].matches).toEqual([['PathPrefix /']])
    expect(model.rules[1].hasBackendRefs).toBe(false)
    expect(model.rules[1].filters[0]).toMatchObject({ type: 'RequestRedirect' })
  })
  it('preserves explicit zero and recovers the omitted weight default without merging repeated backends', () => {
    const model = httpRouteModel(detail, httpRouteConfiguration(content, 'app', 'team-a'))
    expect(
      model.rules[0].backends.map((b) => [b.namespace, b.port, b.weight, b.weightDefaulted]),
    ).toEqual([
      ['backend-ns', 8080, 0, false],
      ['backend-ns', 8081, 1, true],
    ])
    expect(new Set(model.rules[0].backends.map((b) => b.key)).size).toBe(2)
  })
  it('enriches only permission-filtered references and keeps controller observations independent', () => {
    const model = httpRouteModel(detail, httpRouteConfiguration(content, 'app', 'team-a'))
    expect(model.parents.map((p) => p.name)).toEqual(['edge'])
    expect(model.parents[0].sectionName).toBe('https')
    expect(
      model.statuses.map((s) => [
        s.controllerName,
        s.conditions[0].status,
        s.conditions[0].observedGeneration,
      ]),
    ).toEqual([
      ['example.com/a', 'True', 4],
      ['example.com/b', 'False', 5],
    ])
    expect(model.rules[0].backends.some((b) => b.name === 'denied')).toBe(false)
  })
  it('keeps unsupported API groups and a missing scalar projection distinguishable', () => {
    const config = httpRouteConfiguration(
      content.replace(
        '{name: api, namespace: backend-ns, port: 8080, weight: 0}',
        '{group: example.com, kind: Service, name: api, namespace: backend-ns, port: 8080, weight: 0}',
      ),
      'app',
      'team-a',
    )
    expect(httpRouteModel(detail, config).rules[0].backends[0].group).toBe('example.com')
    expect(httpRouteModel(detail).rules[0].backends[0].weight).toBeUndefined()
  })

  it('keeps rule selection stable when the native rule name arrives after the DTO', () => {
    const named = content.replace('    - matches:', '    - name: named-rule\n      matches:')
    const config = httpRouteConfiguration(named, 'app', 'team-a')
    expect(httpRouteModel(detail, config).rules[0].key).toBe(httpRouteModel(detail).rules[0].key)
    expect(httpRouteModel(detail, config).rules[0].name).toBe('named-rule')
  })

  it('does not attach core Service endpoint enrichment to a different API group', () => {
    const enriched: HTTPRouteDetail = {
      ...detail,
      rules: [
        {
          backends: [
            { ...detail.rules![0].backends![0], endpoints: [{ address: '10.0.0.1', ready: true }] },
          ],
        },
      ],
    }
    const source = content.replace(
      '{name: api, namespace: backend-ns, port: 8080, weight: 0}',
      '{group: example.com, kind: Service, name: api, namespace: backend-ns, port: 8080, weight: 0}',
    )
    expect(
      httpRouteModel(enriched, httpRouteConfiguration(source, 'app', 'team-a')).rules[0].backends[0]
        .endpoints,
    ).toBeUndefined()
    expect(
      httpRouteModel(enriched, httpRouteConfiguration(content, 'app', 'team-a')).rules[0]
        .backends[0].endpoints,
    ).toHaveLength(1)
  })
  it.each([
    content.replace('name: app', 'name: other'),
    content.replace('namespace: team-a', 'namespace: team-b'),
    content.replace('kind: HTTPRoute', 'kind: Gateway'),
    content.replace('/v1\n', '/v9\n'),
    'invalid: [yaml',
  ])('rejects mismatched or invalid source configuration', (source) => {
    expect(() => httpRouteConfiguration(source, 'app', 'team-a')).toThrow()
  })
})
