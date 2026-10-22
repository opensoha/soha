import { describe, expect, it } from 'vitest'
import { parsePodInspection, probeEndpoint } from './pod-inspection'

describe('Pod inspection YAML', () => {
  it('accepts typed Kubernetes YAML without TypeMeta and retains previous termination', () => {
    const pod = parsePodInspection(`metadata:
  name: api
  namespace: default
  uid: pod-instance
spec:
  containers:
    - name: api
status:
  containerStatuses:
    - name: api
      lastState:
        terminated:
          reason: OOMKilled
          exitCode: 137
`)
    expect(pod.status?.containerStatuses?.[0].lastState?.terminated).toEqual({
      reason: 'OOMKilled',
      exitCode: 137,
    })
    expect(pod.spec.containers[0].livenessProbe).toBeUndefined()
    expect(pod.metadata.uid).toBe('pod-instance')
  })
  it('rejects non-Pod documents and malformed probe fields', () => {
    expect(() => parsePodInspection('kind: Deployment')).toThrow()
    expect(() =>
      parsePodInspection(
        'metadata: {name: api}\nspec: {containers: [{name: api, livenessProbe: {httpGet: {port: []}}}]}',
      ),
    ).toThrow()
  })
  it('resolves named HTTP ports and handles IPv6 and gRPC endpoints', () => {
    const container = { name: 'api', ports: [{ name: 'web', containerPort: 8080 }] }
    expect(
      probeEndpoint(
        { httpGet: { scheme: 'HTTPS', port: 'web', path: '/ready' } },
        container,
        '2001:db8::1',
      ),
    ).toBe('HTTPS://[2001:db8::1]:8080/ready')
    expect(probeEndpoint({ grpc: { port: 9090, service: 'health' } }, container, '10.0.0.1')).toBe(
      'gRPC 10.0.0.1:9090 · health',
    )
  })
})
