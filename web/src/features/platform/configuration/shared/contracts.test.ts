import { describe, expect, it } from 'vitest'
import { configurationKeys } from './keys'
import {
  buildConfigurationDataPath,
  buildConfigurationDetailPath,
  buildConfigurationReferencesPath,
  buildConfigurationYAMLPath,
} from './paths'
import { configurationTargetFromRecord, resolveConfigurationNamespace } from './scope'

const scope = { clusterId: 'cluster-a', namespace: 'team/a' }

describe('configuration contracts', () => {
  it('uses store, URL, then record namespace priority', () => {
    expect(resolveConfigurationNamespace('store-ns', 'url-ns', 'record-ns')).toBe('store-ns')
    expect(resolveConfigurationNamespace(null, 'url-ns', 'record-ns')).toBe('url-ns')
    expect(resolveConfigurationNamespace(null, null, 'record-ns')).toBe('record-ns')
    expect(
      configurationTargetFromRecord('cluster-a', { name: 'app/config', namespace: 'record-ns' }),
    ).toEqual({
      scope: { clusterId: 'cluster-a', namespace: 'record-ns' },
      name: 'app/config',
    })
  })

  it('encodes resource names and keeps namespace in every detail subresource path', () => {
    expect(buildConfigurationDetailPath('configmaps', scope, 'app/config')).toBe(
      '/clusters/cluster-a/configuration/configmaps/app%2Fconfig/detail?namespace=team%2Fa',
    )
    expect(buildConfigurationDataPath('configmaps', scope, 'app/config')).toContain(
      '/app%2Fconfig/data?namespace=team%2Fa',
    )
    expect(buildConfigurationReferencesPath('configmaps', scope, 'app/config')).toContain(
      '/app%2Fconfig/references?namespace=team%2Fa',
    )
    expect(buildConfigurationYAMLPath('configmaps', scope, 'app/config')).toContain(
      '/app%2Fconfig/yaml?namespace=team%2Fa',
    )
  })

  it('uses canonical list, detail, reference, and YAML keys', () => {
    expect(configurationKeys.list('secrets', scope)).toEqual([
      'platform',
      'configuration',
      'secrets',
      'list',
      scope,
    ])
    expect(configurationKeys.references('secrets', scope, 'registry')).toEqual([
      ...configurationKeys.detail('secrets', scope, 'registry'),
      'references',
    ])
    expect(configurationKeys.yaml('secrets', scope, 'registry')).toEqual([
      ...configurationKeys.detail('secrets', scope, 'registry'),
      'yaml',
    ])
  })
})
