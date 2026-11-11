import { describe, expect, it } from 'vitest'
import { stringify } from 'yaml'
import { readPodContainerConfiguration } from './pod-container-configuration'

function fixture() {
  return {
    apiVersion: 'v1',
    kind: 'Pod',
    metadata: { name: 'api-pod', namespace: 'team-a' },
    spec: {
      initContainers: [
        {
          name: 'init',
          image: 'init:v1',
          command: ['/bin/sh', '-c'],
          args: ['echo init'],
          resources: {},
        },
      ],
      containers: [
        {
          name: 'app',
          image: 'app:v1',
          imagePullPolicy: 'IfNotPresent',
          command: ['/bin/sh', '-c'],
          args: ['echo "a b"', ''],
          resources: {
            requests: { cpu: '25m', memory: '64Mi' },
            limits: { cpu: '2', memory: '256Mi', 'example.com/gpu': '1' },
          },
          env: [
            { name: 'EMPTY', value: '' },
            { name: 'POD_NAME', valueFrom: { fieldRef: { fieldPath: 'metadata.name' } } },
            {
              name: 'PASSWORD',
              valueFrom: { secretKeyRef: { name: 'app-secret', key: 'password', optional: true } },
            },
            {
              name: 'CPU',
              valueFrom: {
                resourceFieldRef: { resource: 'limits.cpu', divisor: '1m', containerName: 'app' },
              },
            },
          ],
          envFrom: [
            { configMapRef: { name: 'app-config' }, prefix: 'APP_' },
            { secretRef: { name: 'app-secret' } },
          ],
          volumeMounts: [
            { name: 'data', mountPath: '/var/lib/app', subPath: 'logs', readOnly: false },
          ],
          ports: [{ name: 'http', containerPort: 8080, protocol: 'TCP', hostPort: 18080 }],
          livenessProbe: {
            httpGet: { path: '/health', port: 'http', scheme: 'HTTP' },
            initialDelaySeconds: 0,
            timeoutSeconds: 1,
            periodSeconds: 10,
            failureThreshold: 3,
          },
          readinessProbe: { tcpSocket: { port: 8080 } },
          startupProbe: { exec: { command: ['test', '-f', '/ready'] } },
        },
      ],
    },
    status: {
      initContainerStatuses: [
        {
          name: 'init',
          state: {
            terminated: {
              reason: 'Completed',
              exitCode: 0,
              signal: 0,
              startedAt: '2026-11-01T00:00:00Z',
              finishedAt: '2026-11-01T00:00:01Z',
            },
          },
        },
      ],
      containerStatuses: [
        {
          name: 'app',
          state: { running: { startedAt: '2026-11-01T00:00:02Z' } },
          lastState: {
            terminated: {
              exitCode: 137,
              signal: 9,
              reason: 'OOMKilled',
              message: 'memory limit exceeded',
              startedAt: '2026-10-31T23:00:00Z',
              finishedAt: '2026-10-31T23:59:00Z',
            },
          },
        },
      ],
    },
  }
}
describe('Pod container configuration from server YAML', () => {
  it('retains container-level configuration, argument boundaries and reference sources', () => {
    const data = readPodContainerConfiguration(stringify(fixture()), 'api-pod', 'team-a')
    expect(data.containers.map((item) => item.name)).toEqual(['init', 'app'])
    const app = data.containers[1]
    expect(app.command).toEqual(['/bin/sh', '-c'])
    expect(app.args).toEqual(['echo "a b"', ''])
    expect(app.requests).toEqual({ cpu: '25m', memory: '64Mi' })
    expect(app.limits['example.com/gpu']).toBe('1')
    expect(app.env[0]).toMatchObject({ name: 'EMPTY', value: '' })
    expect(app.env[2]).toMatchObject({
      name: 'PASSWORD',
      value: undefined,
      source: { kind: 'Secret', name: 'app-secret', key: 'password', optional: true },
    })
    expect(app.envFrom).toMatchObject([
      { source: { kind: 'ConfigMap', name: 'app-config' }, prefix: 'APP_' },
      { source: { kind: 'Secret', name: 'app-secret' } },
    ])
    expect(app.mounts[0]).toMatchObject({ name: 'data', path: '/var/lib/app', subPath: 'logs' })
    expect(app.ports[0]).toMatchObject({ port: 8080, hostPort: 18080 })
    expect(app.probes[0].probe.settings.initialDelaySeconds).toBe('0')
    expect(app.probes.map((item) => item.probe.type)).toEqual(['HTTP GET', 'TCP', 'Exec'])
  })
  it('keeps current and prior exits separate, preserves zero and never supplies a missing exit code', () => {
    const data = readPodContainerConfiguration(stringify(fixture()), 'api-pod', 'team-a')
    expect(data.containers[0].current).toMatchObject({
      type: 'terminated',
      exitCode: 0,
      signal: 0,
      reason: 'Completed',
    })
    expect(data.containers[1].current).toMatchObject({ type: 'running', exitCode: undefined })
    expect(data.containers[1].previous).toMatchObject({
      type: 'terminated',
      exitCode: 137,
      signal: 9,
      reason: 'OOMKilled',
      message: 'memory limit exceeded',
    })
    expect(data.containers[0].previous.type).toBe('unknown')
  })
  it('rejects another Pod or namespace, invalid YAML and duplicate containers', () => {
    const value = fixture()
    expect(() => readPodContainerConfiguration(stringify(value), 'other', 'team-a')).toThrow(
      /identity/,
    )
    expect(() => readPodContainerConfiguration(stringify(value), 'api-pod', 'other')).toThrow(
      /identity/,
    )
    expect(() => readPodContainerConfiguration('kind: [', 'api-pod', 'team-a')).toThrow(/Invalid/)
    value.spec.containers.push(value.spec.containers[0])
    expect(() => readPodContainerConfiguration(stringify(value), 'api-pod', 'team-a')).toThrow(
      /duplicated/,
    )
  })
  it('bounds YAML input and alias expansion', () => {
    expect(() =>
      readPodContainerConfiguration('x'.repeat(4 * 1024 * 1024 + 1), 'api-pod', 'team-a'),
    ).toThrow(/size limit/)
    const alias = `apiVersion: v1\nkind: Pod\nmetadata: { name: api-pod, namespace: team-a }\nspec: { containers: [] }\na: &a [1, 2, 3]\nb: [${Array(150).fill('*a').join(', ')}]`
    expect(() => readPodContainerConfiguration(alias, 'api-pod', 'team-a')).toThrow(
      /alias|resource/i,
    )
  })
})
