import { queryOptions } from '@tanstack/react-query'
import { getSoftwarePackageDownloadRecords, getSoftwareStorage, listSoftwarePackages } from './api'
import { softwarePackageKeys, softwareStorageKeys } from './keys'
import type { SoftwarePackageFilters } from './types'

export const softwarePackageQueries = {
  list: (filters: SoftwarePackageFilters) =>
    queryOptions({
      queryKey: softwarePackageKeys.list(filters),
      queryFn: () => listSoftwarePackages(filters),
    }),
  storage: (storageIntegrationId = '') =>
    queryOptions({
      queryKey: softwareStorageKeys.detail(storageIntegrationId),
      queryFn: () => getSoftwareStorage(storageIntegrationId),
    }),
  downloadRecords: (packageId: string) =>
    queryOptions({
      queryKey: softwarePackageKeys.downloadRecords(packageId),
      queryFn: () => getSoftwarePackageDownloadRecords(packageId),
    }),
}
