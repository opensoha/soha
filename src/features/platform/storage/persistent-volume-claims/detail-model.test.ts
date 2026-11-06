import { describe, expect, it } from 'vitest'
import { stringify } from 'yaml'
import { pvcConfiguration } from './detail-model'

const identity = {
  apiVersion: 'v1',
  kind: 'PersistentVolumeClaim',
  metadata: { name: 'data', namespace: 'apps' },
}
const parse = (value: unknown) => pvcConfiguration(stringify(value), 'data', 'apps')

describe('PVC supplemental configuration', () => {
  it('extracts native conditions, selectors and both data source forms', () => {
    const spec = {
      volumeMode: 'Block',
      storageClassName: '',
      selector: {
        matchLabels: { tier: 'archive', empty: '' },
        matchExpressions: [
          { key: 'zone', operator: 'In', values: ['east', ''] },
          { key: 'ready', operator: 'Exists' },
        ],
      },
      dataSource: { apiGroup: 'snapshot.storage.k8s.io', kind: 'VolumeSnapshot', name: 'seed' },
      dataSourceRef: { kind: 'PersistentVolumeClaim', name: 'original', namespace: 'source' },
    }
    const conditions = [
      {
        type: 'FileSystemResizePending',
        status: 'True',
        reason: 'NodeExpansionPending',
        message: 'Waiting for node expansion',
        lastTransitionTime: '2026-11-03T00:00:00Z',
      },
    ]
    expect(parse({ ...identity, spec, status: { conditions } })).toEqual({ ...spec, conditions })
  })

  it('keeps absent fields absent instead of inventing defaults', () => {
    expect(parse(identity)).toEqual({
      conditions: [],
      storageClassName: undefined,
      volumeMode: undefined,
      selector: undefined,
      dataSource: undefined,
      dataSourceRef: undefined,
    })
  })

  it.each([
    { ...identity, apiVersion: 'apps/v1' },
    { ...identity, kind: 'PersistentVolume' },
    { ...identity, metadata: { name: 'other', namespace: 'apps' } },
    { ...identity, metadata: { name: 'data', namespace: 'other' } },
    { ...identity, metadata: { name: 'data' } },
    { ...identity, metadata: [] },
  ])('rejects a missing or mismatched resource identity %#', (value) => {
    expect(() => parse(value)).toThrow()
  })

  it.each([
    { spec: { volumeMode: 12 } },
    { spec: { selector: { matchLabels: { tier: false } } } },
    { spec: { selector: { matchExpressions: [{ key: 'zone', operator: 'In', values: [3] }] } } },
    { spec: { dataSource: { kind: 'VolumeSnapshot' } } },
    { spec: { dataSourceRef: [] } },
    { status: { conditions: 'Bound' } },
    { status: { conditions: [{ type: 'Resizing', status: true }] } },
  ])('rejects malformed typed fields %#', (fields) => {
    expect(() => parse({ ...identity, ...fields })).toThrow()
  })

  it('rejects malformed and multiple-document YAML', () => {
    expect(() => pvcConfiguration('metadata: [', 'data', 'apps')).toThrow()
    expect(() =>
      pvcConfiguration(stringify(identity) + '---\n' + stringify(identity), 'data', 'apps'),
    ).toThrow()
  })

  it('bounds UTF-8 bytes and expanded YAML aliases', () => {
    const oversized = stringify({
      ...identity,
      metadata: { ...identity.metadata, annotations: { large: '中'.repeat(1_400_000) } },
    })
    expect(() => pvcConfiguration(oversized, 'data', 'apps')).toThrow('budget')
    const aliases =
      stringify(identity) +
      'a: &a [x, x, x, x, x, x, x, x]\nb: &b [*a, *a, *a, *a, *a, *a, *a, *a]\nc: [*b, *b, *b, *b, *b, *b, *b, *b]\n'
    expect(() => pvcConfiguration(aliases, 'data', 'apps')).toThrow()
  })
})
