import type { SoftwarePackageFilters } from './types'

export const softwarePackageKeys = {
  all: ['software-packages'] as const,
  list: (filters: SoftwarePackageFilters) => ['software-packages', 'list', filters] as const,
  downloadRecords: (packageId: string) =>
    ['software-packages', packageId, 'download-records'] as const,
}

export const softwareStorageKeys = {
  all: ['software-storage'] as const,
  detail: (storageIntegrationId = '') =>
    ['software-storage', 'detail', storageIntegrationId.trim()] as const,
}

export const softwarePackageMutationKeys = {
  publish: ['software-packages', 'publish'] as const,
  remove: ['software-packages', 'delete'] as const,
}
