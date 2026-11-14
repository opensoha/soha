import { describe, expect, it } from 'vitest'
import { httpRouteConfiguration, httpRouteModel } from './httproute-configuration'
import type { GRPCRouteDetail } from './types'

const source = `apiVersion: gateway.networking.k8s.io/v1
kind: GRPCRoute
metadata: {name: rpc, namespace: apps, generation: 4}
spec:
  parentRefs: [{name: edge, namespace: gateway-system, sectionName: grpc}]
  rules:
    - matches:
        - method: {service: example.User, method: Login}
          headers: [{name: version, value: v2}]
        - method: {type: RegularExpression, service: 'example.*'}
      backendRefs:
        - {name: rpc-server, namespace: backends, port: 9000, weight: 0}
        - {name: rpc-server, namespace: backends, port: 9001}
        - {name: hidden, port: 9090}
      filters:
        - type: RequestHeaderModifier
          requestHeaderModifier: {set: [{name: x-version, value: v2}]}
    - matches:
        - headers: [{name: tenant, type: RegularExpression, value: 'team-.*'}]
    - {}
status:
  parents:
    - parentRef: {name: edge, namespace: gateway-system, sectionName: grpc}
      controllerName: example.com/controller
      conditions: [{type: Accepted, status: 'False', reason: UnsupportedValue, observedGeneration: 3}]
`
const detail: GRPCRouteDetail = {
  name: 'rpc',
  namespace: 'apps',
  ageSeconds: 1,
  ruleCount: 3,
  parentRefs: ['gateway-system/edge'],
  parentStatuses: [{ parentRef: 'gateway-system:edge', controllerName: 'example.com/controller' }],
  rules: [
    {
      backends: [
        { name: 'rpc-server', kind: 'Service', namespace: 'backends', port: 9000, weight: 0 },
        { name: 'rpc-server', kind: 'Service', namespace: 'backends', port: 9001, weight: 0 },
      ],
    },
    {},
    {},
  ],
}
describe('GRPCRoute protocol adapter', () => {
  it('uses service/method and headers with independent OR groups and no HTTP path default', () => {
    const config = httpRouteConfiguration(source, 'rpc', 'apps', 'GRPCRoute')
    const model = httpRouteModel(detail, config)
    expect(model.rules[0].matches).toEqual([
      ['Service Exact · example.User', 'Method Exact · Login', 'Header Exact · version = v2'],
      ['Service RegularExpression · example.*', 'Method Any'],
    ])
    expect(model.rules[1].matches).toEqual([
      ['Any gRPC service / method', 'Header RegularExpression · tenant = team-.*'],
    ])
    expect(model.rules[2].matches).toEqual([['Any gRPC service / method']])
    expect(JSON.stringify(model.rules.map((r) => r.matches))).not.toContain('PathPrefix')
    expect(model.rules[0].filters[0].type).toBe('RequestHeaderModifier')
  })
  it('shares filtered references, per-controller conditions and zero/default weights', () => {
    const model = httpRouteModel(detail, httpRouteConfiguration(source, 'rpc', 'apps', 'GRPCRoute'))
    expect(model.parents[0]).toMatchObject({ namespace: 'gateway-system', sectionName: 'grpc' })
    expect(model.statuses[0].conditions[0]).toMatchObject({
      status: 'False',
      observedGeneration: 3,
    })
    expect(
      model.rules[0].backends.map((b) => [b.namespace, b.port, b.weight, b.weightDefaulted]),
    ).toEqual([
      ['backends', 9000, 0, false],
      ['backends', 9001, 1, true],
    ])
    expect(model.rules[0].backends.some((b) => b.name === 'hidden')).toBe(false)
  })
  it('supports the provider legacy gRPC API and rejects HTTP or wrong-scope sources', () => {
    expect(
      httpRouteConfiguration(source.replace('/v1', '/v1alpha2'), 'rpc', 'apps', 'GRPCRoute').kind,
    ).toBe('GRPCRoute')
    expect(() =>
      httpRouteConfiguration(
        source.replace('kind: GRPCRoute', 'kind: HTTPRoute'),
        'rpc',
        'apps',
        'GRPCRoute',
      ),
    ).toThrow()
    expect(() => httpRouteConfiguration(source, 'rpc', 'other', 'GRPCRoute')).toThrow()
    expect(() =>
      httpRouteConfiguration(source.replace('/v1', '/v1beta1'), 'rpc', 'apps', 'GRPCRoute'),
    ).toThrow()
    expect(() => httpRouteConfiguration(source, 'rpc', 'apps')).toThrow()
  })
})
