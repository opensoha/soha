import { describe, expect, it } from 'vitest'
import { backendTLSConfiguration } from './backend-tls-configuration'
import type { BackendTLSPolicyDetail } from './types'
const detail: BackendTLSPolicyDetail = {
  name: 'tls',
  namespace: 'apps',
  ageSeconds: 1,
  targetRefs: ['apps:Service/backend'],
  caCertificateRefs: ['apps:ConfigMap/root-ca'],
}
const source = `apiVersion: gateway.networking.k8s.io/v1
kind: BackendTLSPolicy
metadata: {name: tls, namespace: apps, generation: 2}
spec:
  targetRefs:
    - {group: '', kind: Service, name: backend, sectionName: https}
    - {group: '', kind: Service, name: hidden}
  validation:
    hostname: backend.example.test
    caCertificateRefs:
      - {group: '', kind: ConfigMap, name: root-ca}
    subjectAltNames:
      - {type: Hostname, hostname: backend.example.test}
      - {type: URI, uri: 'spiffe://test/ns/apps/sa/backend'}
status:
  ancestors:
    - ancestorRef: {name: edge, namespace: gateways}
      controllerName: example.com/a
      conditions: [{type: Accepted, status: 'False', reason: InvalidCACertificateRef, observedGeneration: 0}]
    - ancestorRef: {name: edge, namespace: gateways}
      controllerName: example.com/b
      conditions: [{type: Accepted, status: 'True', observedGeneration: 2}]
`
describe('BackendTLSPolicy configuration', () => {
  it('preserves local targets, SectionName, CA identity and SAN alternatives', () => {
    const c = backendTLSConfiguration(source, detail)
    expect(c.targets.map((r) => r.name)).toEqual(['backend'])
    expect(c.targets[0].sectionName).toBe('https')
    expect(c.caRefs[0]).toMatchObject({
      group: '',
      kind: 'ConfigMap',
      name: 'root-ca',
      namespace: 'apps',
    })
    expect(c.sans).toEqual([
      'Hostname · backend.example.test',
      'URI · spiffe://test/ns/apps/sa/backend',
    ])
  })
  it('keeps per-ancestor controller observations independent and retains generation zero', () => {
    const c = backendTLSConfiguration(source, detail)
    expect(
      c.ancestors.map((a) => [
        a.controller,
        a.conditions[0].status,
        a.conditions[0].observedGeneration,
      ]),
    ).toEqual([
      ['example.com/a', 'False', 0],
      ['example.com/b', 'True', 2],
    ])
    expect(new Set(c.ancestors.map((a) => a.key)).size).toBe(2)
  })
  it('does not restore references removed by the server', () => {
    const c = backendTLSConfiguration(source, { ...detail, targetRefs: [], caCertificateRefs: [] })
    expect(c.targets).toEqual([])
    expect(c.caRefs).toEqual([])
  })
  it.each([
    source.replace('name: tls', 'name: other'),
    source.replace('namespace: apps', 'namespace: other'),
    source.replace('kind: BackendTLSPolicy', 'kind: Gateway'),
    source.replace('/v1\n', '/v9\n'),
    'bad: [yaml',
  ])('rejects mismatched or invalid configuration', (s) => {
    expect(() => backendTLSConfiguration(s, detail)).toThrow()
  })
})
