import { lazy } from 'react'

export { systemIntegrationMutations } from './mutations'
export { systemIntegrationQueries } from './queries'
export {
  createS3StorageIntegration,
  inferS3StoragePreset,
  isValidS3StorageEndpoint,
  s3StorageFormValues,
  updateS3StorageIntegration,
} from './object-storage-model'
export type { S3StorageFormValues, S3StoragePreset } from './object-storage-model'
export type { SystemIntegration, SystemIntegrationCategory } from './types'

export const PluginStorageConnections = lazy(async () => {
  const module = await import('./plugin-storage-connections')
  return { default: module.PluginStorageConnections }
})
