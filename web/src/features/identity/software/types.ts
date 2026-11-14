export interface SoftwarePackage {
  id: string
  storageIntegrationId?: string
  softwareId: string
  name: string
  description?: string
  publisher: string
  category?: string
  version: string
  platform: string
  arch: string
  fileName: string
  sizeBytes: number
  sha256: string
  downloadPath: string
  downloadCount?: number
  createdAt: string
  updatedAt: string
}

export interface SoftwarePackageDownloadRecord {
  id: string
  actorId: string
  actorName?: string
  downloadedAt: string
  durationMs: number
  sourceIp?: string
}

export interface SoftwarePackageDownloadRecordListEnvelope {
  items: SoftwarePackageDownloadRecord[]
}

export interface SoftwarePackageListEnvelope {
  items: SoftwarePackage[]
  nextCursor?: string
}

export interface SoftwarePackageFilters {
  platform?: string
  arch?: string
  storageIntegrationId?: string
}

export interface SoftwarePackageMetadataInput {
  storageIntegrationId?: string
  softwareId: string
  name: string
  description?: string
  publisher: string
  category?: string
  version: string
  platform: string
  arch: string
}

export interface SoftwarePackageUploadInput extends SoftwarePackageMetadataInput {
  file: File
}

export interface SoftwarePackageURLImportInput extends SoftwarePackageMetadataInput {
  url: string
  fileName?: string
}

export type SoftwarePackagePublishInput =
  | ({ source: 'file' } & SoftwarePackageUploadInput)
  | ({ source: 'url' } & SoftwarePackageURLImportInput)

export interface SoftwareStorage {
  backend: string
  integrationId?: string
  providerType?: string
  endpoint?: string
  bucket?: string
  region?: string
  healthStatus?: 'unknown' | 'healthy' | 'degraded' | 'unhealthy'
  lastCheckedAt?: string
  objectCount: number
  totalBytes: number
  items: SoftwarePackage[]
  nextCursor?: string
}

export interface SoftwareStorageEnvelope {
  data: SoftwareStorage
}
