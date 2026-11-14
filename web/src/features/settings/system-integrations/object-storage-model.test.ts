import { describe, expect, it } from 'vitest'
import type { SystemIntegration } from './types'
import {
  createS3StorageIntegration,
  inferS3StoragePreset,
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
    expect(input.configuration?.map((field) => field.key)).not.toEqual(
      expect.arrayContaining(['preset', 'access_key_id', 'secret_access_key']),
    )
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
