import { describe, expect, it } from 'vitest'
import { mergeServiceManifest, serviceFormValuesFromManifest } from './service-edit-model'

const service = {
  apiVersion: 'v1',
  kind: 'Service',
  metadata: {
    annotations: { owner: 'platform' },
    labels: { app: 'api' },
    name: 'api',
    namespace: 'team-a',
    resourceVersion: '42',
  },
  spec: {
    clusterIP: '10.43.0.20',
    clusterIPs: ['10.43.0.20'],
    ipFamilies: ['IPv4'],
    ports: [
      {
        appProtocol: 'kubernetes.io/h2c',
        name: 'http',
        nodePort: 30080,
        port: 80,
        protocol: 'TCP',
        targetPort: 'web',
      },
    ],
    selector: { app: 'api' },
    sessionAffinity: 'ClientIP',
    type: 'NodePort',
  },
  status: { loadBalancer: {} },
}

describe('Service quick edit model', () => {
  it('loads named ports and preserves server-managed fields when merging edits', () => {
    expect(serviceFormValuesFromManifest(service)).toMatchObject({
      name: 'api',
      namespace: 'team-a',
      ports: [{ name: 'http', nodePort: 30080, port: 80, protocol: 'TCP', targetPort: 'web' }],
      selector: [{ key: 'app', value: 'api' }],
      type: 'NodePort',
    })

    const merged = mergeServiceManifest(service, {
      apiVersion: 'v1',
      kind: 'Service',
      metadata: {
        annotations: { owner: 'sre' },
        labels: { app: 'backend' },
        name: 'renamed',
        namespace: 'other',
      },
      spec: {
        ports: [{ name: 'http', nodePort: 30081, port: 8080, protocol: 'TCP', targetPort: 'web' }],
        selector: { app: 'backend' },
        type: 'NodePort',
      },
    })

    expect(merged).toMatchObject({
      metadata: {
        annotations: { owner: 'sre' },
        labels: { app: 'backend' },
        name: 'api',
        namespace: 'team-a',
        resourceVersion: '42',
      },
      spec: {
        clusterIP: '10.43.0.20',
        clusterIPs: ['10.43.0.20'],
        ipFamilies: ['IPv4'],
        ports: [
          { appProtocol: 'kubernetes.io/h2c', nodePort: 30081, port: 8080, targetPort: 'web' },
        ],
        selector: { app: 'backend' },
        sessionAffinity: 'ClientIP',
        type: 'NodePort',
      },
      status: { loadBalancer: {} },
    })
  })

  it('does not move appProtocol to a different transport protocol or duplicate it after moving a named port', () => {
    const ports = [
      { name: 'http', port: 8080, protocol: 'TCP' },
      { name: 'replacement', port: 80, protocol: 'TCP' },
      { name: 'dns', port: 80, protocol: 'UDP' },
    ]
    const edited = {
      apiVersion: 'v1',
      kind: 'Service' as const,
      metadata: { name: 'api', namespace: 'team-a' },
      spec: { type: 'NodePort', ports },
    }
    expect((mergeServiceManifest(service, edited).spec as typeof edited.spec).ports).toEqual([
      { ...ports[0], appProtocol: 'kubernetes.io/h2c' },
      ports[1],
      ports[2],
    ])
  })

  it('removes incompatible fields when changing to ExternalName', () => {
    expect(
      mergeServiceManifest(service, {
        apiVersion: 'v1',
        kind: 'Service',
        metadata: { name: 'api', namespace: 'team-a' },
        spec: { externalName: 'database.example.com', type: 'ExternalName' },
      }),
    ).toMatchObject({
      metadata: { name: 'api', namespace: 'team-a', resourceVersion: '42' },
      spec: { externalName: 'database.example.com', type: 'ExternalName' },
    })
  })
})
