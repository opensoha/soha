import { describe, expect, it } from 'vitest'
import type { SystemIntegration } from './types'
import {
  createS3StorageIntegration,
  inferS3StoragePreset,
  isValidS3StorageEndpoint,
  s3StorageFormValues,
  updateS3StorageIntegration,
} from './object-storage-model'

const integration: SystemIntegration = {
  id: 'storage-1',
  category: 'storage' as SystemIntegration['category'],
  providerType: 's3',
  name: 'Software object storage',
  description: '',
  enabled: true,
  configuration: [
    { key: 'endpoint', value: 'https://minio.example.com' },
    { key: 'bucket', value: 'soha-software' },
    { key: 'region', value: 'us-east-1' },
    { key: 'path_style', value: 'true' },
    { key: 'insecure', value: 'false' },
    { key: 'prefix', value: 'managed' },
  ],
  credentialKeys: ['access_key_id', 'secret_access_key'],
  healthStatus: 'healthy',
  version: 3,
  createdAt: '2026-08-25T00:00:00Z',
  updatedAt: '2026-08-25T00:00:00Z',
}

describe('S3 object storage integration model', () => {
  it('creates an S3-compatible integration without exposing secrets as configuration', () => {
    const input = createS3StorageIntegration({
      ...s3StorageFormValues(),
      endpoint: 'https://minio.example.com/',
      bucket: 'soha-software',
      accessKeyId: 'access',
      secretAccessKey: 'secret',
    })

    expect(input).toMatchObject({ category: 'storage', providerType: 's3', enabled: true })
    expect(input.configuration).toEqual(
      expect.arrayContaining([
        { key: 'endpoint', value: 'https://minio.example.com' },
        { key: 'bucket', value: 'soha-software' },
      ]),
    )
    for (const key of ['preset', 'access_key_id', 'secret_access_key']) {
      expect(input.configuration?.map((field) => field.key)).not.toContain(key)
    }
    expect(input.credentials).toEqual([
      { key: 'access_key_id', value: 'access' },
      { key: 'secret_access_key', value: 'secret' },
    ])
  })

  it('recognizes OSS and COS as S3-compatible presets', () => {
    expect(
      inferS3StoragePreset({
        ...integration,
        configuration: [{ key: 'endpoint', value: 'https://oss-cn-hangzhou.aliyuncs.com' }],
      }),
    ).toBe('oss')
    expect(
      inferS3StoragePreset({
        ...integration,
        configuration: [{ key: 'endpoint', value: 'https://cos.ap-shanghai.myqcloud.com' }],
      }),
    ).toBe('cos')
  })

  it.each([
    {
      preset: 'oss',
      endpoint: 'https://s3.oss-ap-southeast-1.aliyuncs.com',
      region: 'ap-southeast-1',
      bucket: 'oss-apps',
    },
    {
      preset: 'cos',
      endpoint: 'https://cos.ap-shanghai.myqcloud.com',
      region: 'ap-shanghai',
      bucket: 'cos-apps-1250000000',
    },
  ] as const)('stores $preset using providerType s3 and only connection fields', (values) => {
    const input = createS3StorageIntegration({
      ...s3StorageFormValues(),
      ...values,
      pathStyle: false,
    })
    expect(input).toMatchObject({ category: 'storage', providerType: 's3' })
    expect(input.configuration).toEqual(
      expect.arrayContaining([
        { key: 'endpoint', value: values.endpoint },
        { key: 'region', value: values.region },
        { key: 'bucket', value: values.bucket },
        { key: 'path_style', value: 'false' },
      ]),
    )
    expect(input.configuration?.some(({ key }) => key === 'preset')).toBe(false)
  })

  it('allows an omitted optional endpoint but rejects empty required or malformed explicit endpoints', () => {
    expect(isValidS3StorageEndpoint('', false)).toBe(true)
    expect(isValidS3StorageEndpoint(undefined, true)).toBe(false)
    expect(isValidS3StorageEndpoint('   ', true)).toBe(false)
    expect(isValidS3StorageEndpoint('///', false)).toBe(false)
    expect(isValidS3StorageEndpoint('https:///', true)).toBe(false)
    expect(isValidS3StorageEndpoint('https:///objects.example.test', true)).toBe(false)
    expect(isValidS3StorageEndpoint('https://user@objects.example.test', true)).toBe(false)
    expect(isValidS3StorageEndpoint('https://objects.example.test/?bucket=app', true)).toBe(false)
    expect(isValidS3StorageEndpoint(' https://objects.example.test/ ', true)).toBe(true)
  })

  it('persists a bucket-bound endpoint through S3 configuration', () => {
    const { configuration = [] } = createS3StorageIntegration({
      ...s3StorageFormValues(),
      endpoint: 'https://objects.example.test',
      region: 'cn-hangzhou',
      pathStyle: false,
      bucketEndpoint: true,
    })
    expect(configuration).toContainEqual({ key: 'bucket_endpoint', value: 'true' })
    const saved = { ...integration, configuration }
    expect(s3StorageFormValues(saved).bucketEndpoint).toBe(true)
    expect(updateS3StorageIntegration(saved, s3StorageFormValues(saved)).configuration).toEqual(
      configuration,
    )
  })

  it('keeps immutable endpoint configuration and only rotates entered credentials', () => {
    const values = s3StorageFormValues(integration)
    expect(values.secretAccessKey).toBe('')
    const input = updateS3StorageIntegration(integration, {
      ...values,
      endpoint: 'https://ignored.example.com',
      accessKeyId: 'next-access',
      secretAccessKey: 'next-secret',
    })

    expect(input.expectedVersion).toBe(3)
    expect(input.configuration).toEqual(integration.configuration)
    expect(input.credentials).toEqual([
      { key: 'access_key_id', value: 'next-access' },
      { key: 'secret_access_key', value: 'next-secret' },
    ])
  })
})
