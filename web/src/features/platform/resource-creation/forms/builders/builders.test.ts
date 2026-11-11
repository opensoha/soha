import { describe, expect, it } from 'vitest'
import { buildNamespaceManifest, buildServiceAccountManifest } from './access-control'
import { buildConfigMapManifest, buildSecretManifest } from './configuration'
import { buildIngressManifest, buildServiceManifest } from './network'
import { buildPersistentVolumeClaimManifest } from './storage'
import {
  buildControllerManifest,
  buildJobManifest,
  buildWorkloadSnapshotRequest,
} from './workloads'

const metadata = {
  name: 'demo',
  namespace: 'minio',
  labels: [{ key: 'team', value: 'platform' }],
}

const pod = {
  containerName: 'app',
  image: 'nginx:1.27',
  containerPort: 8080,
  env: [{ key: 'MODE', value: 'prod' }],
}

describe('workload manifest builders', () => {
  it.each(['Deployment', 'StatefulSet', 'DaemonSet'] as const)(
    'keeps %s selector and pod labels consistent',
    (kind) => {
      const result = buildControllerManifest(kind, {
        ...metadata,
        ...pod,
        replicas: 2,
        serviceName: 'demo-headless',
      })

      expect(result).toMatchObject({
        apiVersion: 'apps/v1',
        kind,
        metadata: { name: 'demo', namespace: 'minio', labels: { team: 'platform' } },
        spec: {
          selector: { matchLabels: { 'app.kubernetes.io/name': 'demo' } },
          template: {
            metadata: {
              labels: { 'app.kubernetes.io/name': 'demo', team: 'platform' },
            },
            spec: { containers: [{ name: 'app', image: 'nginx:1.27' }] },
          },
        },
      })
      if (kind === 'DaemonSet') expect(result.spec).not.toHaveProperty('replicas')
      if (kind === 'StatefulSet') expect(result.spec).toHaveProperty('serviceName', 'demo-headless')
    },
  )

  it('builds the CronJob jobTemplate nesting and restart policy', () => {
    const result = buildJobManifest('CronJob', {
      ...metadata,
      ...pod,
      runtimeSource: 'manual',
      sourceKind: 'Deployment',
      restartPolicy: 'OnFailure',
      schedule: '*/5 * * * *',
      suspend: true,
      backoffLimit: 3,
    })

    expect(result).toMatchObject({
      apiVersion: 'batch/v1',
      kind: 'CronJob',
      spec: {
        schedule: '*/5 * * * *',
        suspend: true,
        jobTemplate: {
          spec: {
            backoffLimit: 3,
            template: { spec: { restartPolicy: 'OnFailure' } },
          },
        },
      },
    })
  })

  it('builds an ordinary Job without CronJob fields', () => {
    const result = buildJobManifest('Job', {
      ...metadata,
      ...pod,
      runtimeSource: 'manual',
      sourceKind: 'Deployment',
      restartPolicy: 'Never',
      description: 'nightly cleanup',
      commandText: '/bin/sh\n-c',
      argsText: 'php artisan cleanup',
    })
    expect(result).toMatchObject({
      kind: 'Job',
      metadata: { annotations: { 'soha.io/description': 'nightly cleanup' } },
      spec: {
        template: {
          spec: {
            containers: [
              {
                command: ['/bin/sh', '-c'],
                args: ['php artisan cleanup'],
              },
            ],
            restartPolicy: 'Never',
          },
        },
      },
    })
    expect(result.spec).not.toHaveProperty('schedule')
  })

  it('builds a typed snapshot request without leaking blank form values', () => {
    expect(
      buildWorkloadSnapshotRequest('CronJob', {
        ...metadata,
        ...pod,
        imagePolicy: 'follow',
        runtimeSource: 'workload',
        sourceKind: 'StatefulSet',
        sourceName: ' api ',
        sourceContainer: ' worker ',
        inherit: ['environment', 'storage'],
        description: ' hourly billing ',
        commandText: 'php\nartisan',
        argsText: ' billing:run\n ',
        restartPolicy: 'OnFailure',
        schedule: ' */10 * * * * ',
        suspend: true,
      }),
    ).toMatchObject({
      namespace: 'minio',
      sourceKind: 'StatefulSet',
      sourceName: 'api',
      sourceContainer: 'worker',
      inherit: ['environment', 'storage'],
      targetKind: 'WorkloadCronJob',
      targetName: 'demo',
      description: 'hourly billing',
      labels: { team: 'platform' },
      command: ['php', 'artisan'],
      args: ['billing:run'],
      restartPolicy: 'OnFailure',
      schedule: '*/10 * * * *',
      suspend: true,
    })
  })

  it('keeps the temporary Job manifest renderable after manual fields are removed', () => {
    expect(
      buildJobManifest('Job', {
        ...metadata,
        containerName: undefined,
        image: undefined,
        runtimeSource: 'workload',
        sourceKind: 'Deployment',
        sourceName: 'api',
        restartPolicy: 'Never',
      } as unknown as Parameters<typeof buildJobManifest>[1]),
    ).toMatchObject({ spec: { template: { spec: { containers: [{ name: 'app' }] } } } })
  })
})

describe('network manifest builders', () => {
  it('preserves Service port mapping and selector', () => {
    expect(
      buildServiceManifest({
        ...metadata,
        type: 'ClusterIP',
        selector: [{ key: 'app', value: 'demo' }],
        ports: [{ name: 'http', port: 80, targetPort: 8080, protocol: 'TCP' }],
      }),
    ).toMatchObject({
      apiVersion: 'v1',
      kind: 'Service',
      spec: {
        type: 'ClusterIP',
        selector: { app: 'demo' },
        ports: [{ name: 'http', port: 80, targetPort: 8080, protocol: 'TCP' }],
      },
    })
  })

  it('preserves named target ports and normalizes numeric input', () => {
    expect(
      buildServiceManifest({
        ...metadata,
        type: 'ClusterIP',
        ports: [
          { name: 'named', port: 80, targetPort: 'http', protocol: 'TCP' },
          { name: 'numeric', port: 81, targetPort: '8081', protocol: 'TCP' },
        ],
      }),
    ).toMatchObject({ spec: { ports: [{ targetPort: 'http' }, { targetPort: 8081 }] } })
  })

  it('uses externalName instead of a selector for ExternalName services', () => {
    const result = buildServiceManifest({
      ...metadata,
      type: 'ExternalName',
      externalName: 'database.example.com',
      selector: [{ key: 'app', value: 'ignored' }],
      ports: [{ port: 5432, targetPort: 5432, protocol: 'TCP' }],
    })
    expect(result).toMatchObject({
      spec: { type: 'ExternalName', externalName: 'database.example.com' },
    })
    expect(result.spec).not.toHaveProperty('selector')
    expect(result.spec).not.toHaveProperty('ports')
  })

  it('includes nodePort only for Service types that expose it', () => {
    const port = {
      name: 'http',
      port: 80,
      targetPort: 8080,
      nodePort: 30080,
      protocol: 'TCP' as const,
    }
    expect(buildServiceManifest({ ...metadata, type: 'NodePort', ports: [port] })).toMatchObject({
      spec: { ports: [{ nodePort: 30080 }] },
    })
    expect(
      buildServiceManifest({ ...metadata, type: 'LoadBalancer', ports: [port] }),
    ).toMatchObject({ spec: { ports: [{ nodePort: 30080 }] } })
    expect(buildServiceManifest({ ...metadata, type: 'ClusterIP', ports: [port] }).spec).toEqual({
      type: 'ClusterIP',
      ports: [{ name: 'http', port: 80, targetPort: 8080, protocol: 'TCP' }],
    })
  })

  it('builds Ingress v1 service backends and TLS', () => {
    expect(
      buildIngressManifest({
        ...metadata,
        host: 'demo.example.com',
        tlsSecretName: 'demo-tls',
        paths: [{ path: '/api', serviceName: 'api', servicePort: 8080 }],
      }),
    ).toMatchObject({
      apiVersion: 'networking.k8s.io/v1',
      kind: 'Ingress',
      spec: {
        rules: [
          {
            host: 'demo.example.com',
            http: {
              paths: [
                { path: '/api', backend: { service: { name: 'api', port: { number: 8080 } } } },
              ],
            },
          },
        ],
        tls: [{ hosts: ['demo.example.com'], secretName: 'demo-tls' }],
      },
    })
  })
})

describe('configuration, storage, and access control builders', () => {
  it('builds ConfigMap data and UTF-8 base64 Secret data', () => {
    const data = [{ key: 'message', value: '你好 Soha' }]
    expect(buildConfigMapManifest({ ...metadata, data })).toMatchObject({
      data: { message: '你好 Soha' },
    })
    const secret = buildSecretManifest({ ...metadata, type: 'Opaque', data })
    const encoded = (secret.data as Record<string, string>).message
    expect(
      new TextDecoder().decode(Uint8Array.from(atob(encoded), (char) => char.charCodeAt(0))),
    ).toBe('你好 Soha')
  })

  it('preserves PVC access modes, volume mode, class, and request', () => {
    expect(
      buildPersistentVolumeClaimManifest({
        ...metadata,
        accessModes: ['ReadWriteOnce', 'ReadOnlyMany'],
        storage: '20Gi',
        storageClassName: 'fast',
        volumeMode: 'Filesystem',
      }),
    ).toMatchObject({
      kind: 'PersistentVolumeClaim',
      spec: {
        accessModes: ['ReadWriteOnce', 'ReadOnlyMany'],
        volumeMode: 'Filesystem',
        storageClassName: 'fast',
        resources: { requests: { storage: '20Gi' } },
      },
    })
  })

  it('keeps Namespace cluster-scoped and maps ServiceAccount pull secrets', () => {
    expect(buildNamespaceManifest({ name: 'team-a' })).toEqual({
      apiVersion: 'v1',
      kind: 'Namespace',
      metadata: { name: 'team-a' },
    })
    expect(
      buildServiceAccountManifest({
        ...metadata,
        automountServiceAccountToken: false,
        imagePullSecrets: ['registry-a', ' registry-b '],
      }),
    ).toMatchObject({
      kind: 'ServiceAccount',
      automountServiceAccountToken: false,
      imagePullSecrets: [{ name: 'registry-a' }, { name: 'registry-b' }],
    })
  })
})
