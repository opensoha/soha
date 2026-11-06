import { describe, expect, it } from 'vitest'
import { gatewayClassConfiguration } from './gatewayclass-configuration'

const manifest = (extra = '') => `apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: envoy
  generation: 3
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
${extra}`

describe('gatewayClassConfiguration', () => {
  it('retains the parameter kind, group and namespace without inventing a target', () => {
    const config = gatewayClassConfiguration(
      manifest(`  parametersRef:
    group: gateway.envoyproxy.io
    kind: EnvoyProxy
    name: custom-proxy
    namespace: gateway-system
status:
  conditions:
    - type: Accepted
      status: 'False'
      reason: InvalidParameters
      message: Parameters were rejected
      observedGeneration: 2
`),
      'envoy',
    )
    expect(config.parameters).toEqual({
      group: 'gateway.envoyproxy.io',
      kind: 'EnvoyProxy',
      name: 'custom-proxy',
      namespace: 'gateway-system',
    })
    expect(config.conditions?.[0]).toMatchObject({
      type: 'Accepted',
      status: 'False',
      reason: 'InvalidParameters',
      observedGeneration: 2,
    })
    expect(config.generation).toBe(3)
  })

  it('keeps cluster references cluster scoped and accepts the older supported API version', () => {
    const config = gatewayClassConfiguration(
      manifest(`  parametersRef:
    group: example.com
    kind: Settings
    name: shared
`).replace('/v1', '/v1beta1'),
      'envoy',
    )
    expect(config.parameters).toMatchObject({ name: 'shared', namespace: undefined })
  })

  it('distinguishes absent parameters and missing controller observations from zero', () => {
    const config = gatewayClassConfiguration(manifest(), 'envoy')
    expect(config.parameters).toBeNull()
    expect(config.conditions).toBeUndefined()
    const zero = gatewayClassConfiguration(
      manifest(`status:
  conditions:
    - type: Accepted
      status: Unknown
      observedGeneration: 0
`),
      'envoy',
    )
    expect(zero.conditions?.[0].observedGeneration).toBe(0)
  })

  it.each([
    manifest().replace('name: envoy', 'name: other'),
    manifest().replace('kind: GatewayClass', 'kind: Gateway'),
    manifest().replace('/v1', '/v9'),
    manifest().replace('  name: envoy', '  name: envoy\n  namespace: team-a'),
    manifest('  parametersRef:\n    group: ""\n    kind: ConfigMap\n'),
    manifest(
      '  parametersRef:\n    group: ""\n    kind: ConfigMap\n    name: settings\n    namespace: false\n',
    ),
    'not: [valid',
  ])('rejects invalid identity or parameters instead of building a guessed link', (content) => {
    expect(() => gatewayClassConfiguration(content, 'envoy')).toThrow()
  })
})
