import { describe, expect, it } from 'vitest'
import { serviceConfiguration } from './detail-model'

const yaml = `apiVersion: v1
kind: Service
metadata: { name: api, namespace: team }
spec:
  type: LoadBalancer
  clusterIP: None
  clusterIPs: [None]
  externalIPs: [192.0.2.10]
  ipFamilies: [IPv4, IPv6]
  internalTrafficPolicy: Local
  externalTrafficPolicy: Cluster
  publishNotReadyAddresses: false
status:
  loadBalancer:
    ingress: [{ip: 192.0.2.20}, {hostname: lb.example.test}]
`
describe('Service configuration projection', () => {
  it('reads native addresses and policies and preserves false and missing values', () => {
    const result = serviceConfiguration(yaml, 'api', 'team')
    expect(result).toMatchObject({
      headless: true,
      clusterIPs: ['None'],
      externalIPs: ['192.0.2.10'],
      loadBalancerAddresses: ['192.0.2.20', 'lb.example.test'],
      ipFamilies: ['IPv4', 'IPv6'],
      publishNotReadyAddresses: false,
      internalTrafficPolicy: 'Local',
    })
    expect(result.sessionAffinity).toBeUndefined()
  })
  it('reads an ExternalName without inventing a cluster IP', () => {
    expect(
      serviceConfiguration(
        'apiVersion: v1\nkind: Service\nmetadata: {name: api, namespace: team}\nspec: {type: ExternalName, externalName: database.example.test}',
        'api',
        'team',
      ),
    ).toMatchObject({ externalName: 'database.example.test', clusterIPs: [] })
  })
  it.each([
    yaml.replace('v1', 'v2'),
    yaml.replace('kind: Service', 'kind: Secret'),
    yaml.replace('name: api', 'name: other'),
    yaml.replace('namespace: team', 'namespace: other'),
    '',
    'x'.repeat(4 * 1024 * 1024 + 1),
  ])('rejects invalid or out-of-scope configuration %#', (content) => {
    expect(() => serviceConfiguration(content, 'api', 'team')).toThrow()
  })
})
