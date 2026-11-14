import { describe, it, expect } from 'vitest'
import { stringify } from 'yaml'
import { ingressConfiguration, readIngressManifest, publicIngressURL } from './configuration'
import { ingressEditValues, mergeIngressEdit } from './edit-model'
const manifest = {
  apiVersion: 'networking.k8s.io/v1',
  kind: 'Ingress',
  metadata: {
    name: 'web',
    namespace: 'team',
    resourceVersion: '7',
    annotations: { 'nginx.ingress.kubernetes.io/rewrite-target': '/$1' },
    labels: { app: 'web' },
    managedFields: [{ manager: 'helm' }],
  },
  spec: {
    ingressClassName: 'nginx',
    extension: { keep: true },
    defaultBackend: { resource: { apiGroup: 'example.test', kind: 'Bucket', name: 'assets' } },
    rules: [
      {
        host: 'a.example.test',
        extension: 1,
        http: {
          paths: [
            {
              path: '/api',
              pathType: 'Exact',
              extension: 'first',
              backend: { service: { name: 'api', port: { name: 'http' } } },
            },
            {
              path: '/assets',
              pathType: 'ImplementationSpecific',
              extension: 'second',
              backend: { resource: { apiGroup: 'example.test', kind: 'Bucket', name: 'files' } },
            },
          ],
        },
      },
      {
        host: 'b.example.test',
        http: {
          paths: [
            {
              path: '/',
              pathType: 'Prefix',
              backend: { service: { name: 'web', port: { number: 80 } } },
            },
          ],
        },
      },
    ],
    tls: [
      { hosts: ['a.example.test'], secretName: 'tls-a', extension: 1 },
      { hosts: ['b.example.test'], secretName: 'tls-b' },
    ],
  },
  status: { loadBalancer: { ingress: [{ ip: '1.2.3.4' }] } },
}
const content = stringify(manifest)
describe('Ingress configuration and editing', () => {
  it('projects multi-host paths, named ports, TLS references and resource/default backends without guessing health', () => {
    const c = ingressConfiguration(content, 'web', 'team')
    expect(c.routes).toHaveLength(4)
    expect(c.routes[0]).toMatchObject({
      host: 'a.example.test',
      path: '/api',
      pathType: 'Exact',
      tls: true,
      servicePort: 'http',
    })
    expect(c.routes[1].resource?.name).toBe('files')
    expect(c.routes[3]).toMatchObject({ isDefault: true, resource: { name: 'assets' } })
    expect(c.routes[3].tls).toBeUndefined()
    expect(c.tls.map((t) => t.secretName)).toEqual(['tls-a', 'tls-b'])
  })
  it('leaves the public scheme unknown when TLS hosts are omitted', () => {
    const config = ingressConfiguration(
      stringify({ ...manifest, spec: { ...manifest.spec, tls: [{ secretName: 'default-tls' }] } }),
      'web',
      'team',
    )
    expect(config.routes[0].tls).toBeUndefined()
    expect(publicIngressURL(config.routes[0])).toBeUndefined()
  })
  it('preserves every unedited field including complex resource backends and server metadata', () => {
    expect(mergeIngressEdit(content, 'web', 'team', ingressEditValues(manifest))).toEqual(manifest)
  })
  it('keeps unknown fields associated with the correct surviving rule/path after removals and appends new rules', () => {
    const values = ingressEditValues(manifest)
    values.rules[0].paths.shift()
    values.rules[1].paths[0].servicePort = '8080'
    values.rules.push({
      host: 'new.example.test',
      paths: [{ path: '/', pathType: 'Prefix', serviceName: 'new', servicePort: 'https' }],
    })
    values.tls.shift()
    const merged = mergeIngressEdit(content, 'web', 'team', values) as typeof manifest
    expect(merged.spec.rules[0].http.paths[0]).toMatchObject({
      extension: 'second',
      backend: { resource: { name: 'files' } },
    })
    expect(merged.spec.rules[1].http.paths[0].backend).toMatchObject({
      service: { name: 'web', port: { number: 8080 } },
    })
    expect(merged.spec.rules[2].http.paths[0].backend).toMatchObject({
      service: { name: 'new', port: { name: 'https' } },
    })
    expect(merged.spec.tls).toEqual([manifest.spec.tls[1]])
    expect(merged.metadata).toEqual(manifest.metadata)
    expect(merged.status).toEqual(manifest.status)
  })
  it('supports explicit default backend removal and rejects incomplete Service edits', () => {
    const v = ingressEditValues(manifest)
    v.defaultEnabled = false
    expect(mergeIngressEdit(content, 'web', 'team', v).spec).not.toHaveProperty('defaultBackend')
    v.rules[0].paths[0].servicePort = '70000'
    expect(() => mergeIngressEdit(content, 'web', 'team', v)).toThrow('1–65535')
    v.rules[0].paths[0].servicePort = ''
    expect(() => mergeIngressEdit(content, 'web', 'team', v)).toThrow('required')
  })
  it('preserves optional field absence in default-only configuration', () => {
    const minimal = {
      apiVersion: 'networking.k8s.io/v1',
      kind: 'Ingress',
      metadata: { name: 'web', namespace: 'team' },
      spec: { defaultBackend: { service: { name: 'fallback', port: { number: 80 } } } },
    }
    expect(mergeIngressEdit(stringify(minimal), 'web', 'team', ingressEditValues(minimal))).toEqual(
      minimal,
    )
  })
  it('rejects invalid or mismatched YAML identities', () => {
    expect(() => readIngressManifest(content, 'another', 'team')).toThrow('identity')
    expect(() => readIngressManifest(content, 'web', 'other')).toThrow('identity')
    expect(() => readIngressManifest('x: [', 'web', 'team')).toThrow()
    expect(() => readIngressManifest('', 'web', 'team')).toThrow('empty')
  })
  it('links only concrete safe HTTP paths and leaves wildcard or regex routes as text', () => {
    expect(publicIngressURL({ host: 'a.example.test', path: '/api', tls: true })).toBe(
      'https://a.example.test/api',
    )
    for (const host of [
      '*.example.test',
      'evil.test/@other.test',
      'user@host.test',
      'javascript:alert(1)',
    ])
      expect(publicIngressURL({ host, path: '/', tls: false })).toBeUndefined()
    expect(publicIngressURL({ host: 'a.test', path: '/api/(.*)', tls: false })).toBeUndefined()
  })
})
