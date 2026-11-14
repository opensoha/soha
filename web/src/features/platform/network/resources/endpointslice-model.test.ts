import { describe, expect, it } from 'vitest'
import { sliceConfiguration, sliceEndpointKey } from './endpointslice-model'
import type { EndpointSliceDetail } from './types'
const detail: EndpointSliceDetail = {
  name: 'slice',
  namespace: 'apps',
  ageSeconds: 1,
  addressType: 'IPv6',
  endpoints: [{ address: '::1', targetRef: 'Pod/api' }, { address: '::2' }],
}
const source = `apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata: {name: slice, namespace: apps}
endpoints:
  - addresses: ['::1', '::2']
    targetRef: {apiVersion: v1, kind: Pod, namespace: actual-target, name: api}
`
describe('EndpointSlice scope supplement', () => {
  it('keeps native endpoint count distinct from expanded address records', () => {
    const result = sliceConfiguration(source, detail)
    expect(result.endpointCount).toBe(1)
    expect(result.targetNamespaces[sliceEndpointKey(detail.endpoints![0])]).toBe('actual-target')
    expect(result.targetNamespaces[sliceEndpointKey(detail.endpoints![1])]).toBeUndefined()
  })
  it('does not restore permission-filtered targets or guess an omitted namespace', () => {
    expect(
      sliceConfiguration(source, { ...detail, endpoints: [{ address: '::1' }] }).targetNamespaces,
    ).toEqual({})
    expect(
      sliceConfiguration(source.replace('namespace: actual-target, ', ''), detail).targetNamespaces,
    ).toEqual({})
  })
  it('rejects a mismatched resource identity', () => {
    expect(() =>
      sliceConfiguration(source.replace('name: slice', 'name: another'), detail),
    ).toThrow()
  })
})
