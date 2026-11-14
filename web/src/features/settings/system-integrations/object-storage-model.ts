import type {
  SystemIntegration,
  SystemIntegrationCategory,
  SystemIntegrationCreateRequest,
  SystemIntegrationUpdateRequest,
} from './types'

export interface S3StorageFormValues {
  preset: S3StoragePreset
  name: string
  description?: string
  enabled: boolean
  endpoint: string
  bucket: string
  region: string
  prefix?: string
  pathStyle: boolean
  insecure: boolean
  allowPrivate: boolean
  accessKeyId?: string
  secretAccessKey?: string
  sessionToken?: string
}

export type S3StoragePreset = 'aws' | 'minio' | 'oss' | 'cos' | 'custom'

function configurationValue(item: SystemIntegration, key: string) {
  return item.configuration.find((field) => field.key === key)?.value ?? ''
}

export function s3StorageFormValues(item?: SystemIntegration): S3StorageFormValues {
  return {
    preset: inferS3StoragePreset(item),
    name: item?.name ?? 'Software object storage',
    description: item?.description ?? '',
    enabled: item?.enabled ?? true,
    endpoint: item ? configurationValue(item, 'endpoint') : '',
    bucket: item ? configurationValue(item, 'bucket') : 'soha-software',
    region: item ? configurationValue(item, 'region') : 'us-east-1',
    prefix: item ? configurationValue(item, 'prefix') : '',
    pathStyle: item ? configurationValue(item, 'path_style') === 'true' : true,
    insecure: item ? configurationValue(item, 'insecure') === 'true' : false,
    allowPrivate: item ? configurationValue(item, 'allow_private') === 'true' : false,
    accessKeyId: '',
    secretAccessKey: '',
    sessionToken: '',
  }
}

export function inferS3StoragePreset(item?: SystemIntegration): S3StoragePreset {
  if (!item) return 'minio'
  const endpoint = configurationValue(item, 'endpoint').toLowerCase()
  if (!endpoint || endpoint.includes('amazonaws.com')) return 'aws'
  if (endpoint.includes('aliyuncs.com')) return 'oss'
  if (endpoint.includes('myqcloud.com')) return 'cos'
  if (endpoint.includes('minio')) return 'minio'
  return 'custom'
}

function configuration(values: S3StorageFormValues) {
  return [
    { key: 'endpoint', value: values.endpoint.trim().replace(/\/+$/, '') },
    { key: 'bucket', value: values.bucket.trim() },
    { key: 'region', value: values.region.trim() },
    { key: 'prefix', value: values.prefix?.trim().replace(/^\/+|\/+$/g, '') ?? '' },
    { key: 'path_style', value: String(values.pathStyle) },
    { key: 'insecure', value: String(values.insecure) },
    { key: 'allow_private', value: String(values.allowPrivate) },
  ]
}

function credentials(values: S3StorageFormValues) {
  return [
    values.accessKeyId?.trim()
      ? { key: 'access_key_id', value: values.accessKeyId.trim() }
      : undefined,
    values.secretAccessKey?.trim()
      ? { key: 'secret_access_key', value: values.secretAccessKey.trim() }
      : undefined,
    values.sessionToken?.trim()
      ? { key: 'session_token', value: values.sessionToken.trim() }
      : undefined,
  ].filter((value): value is { key: string; value: string } => Boolean(value))
}

export function createS3StorageIntegration(
  values: S3StorageFormValues,
): SystemIntegrationCreateRequest {
  return {
    category: 'storage' as SystemIntegrationCategory,
    providerType: 's3',
    name: values.name.trim(),
    description: values.description?.trim() || undefined,
    enabled: values.enabled,
    configuration: configuration(values),
    credentials: credentials(values),
  }
}

export function updateS3StorageIntegration(
  item: SystemIntegration,
  values: S3StorageFormValues,
): SystemIntegrationUpdateRequest {
  const nextCredentials = credentials(values)
  return {
    expectedVersion: item.version,
    name: values.name.trim(),
    description: values.description?.trim() ?? '',
    enabled: values.enabled,
    configuration: item.configuration,
    credentials: nextCredentials.length > 0 ? nextCredentials : undefined,
  }
}
