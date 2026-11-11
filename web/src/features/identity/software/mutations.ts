import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { deleteSoftwarePackage, publishSoftwarePackage } from './api'
import { softwarePackageKeys, softwarePackageMutationKeys, softwareStorageKeys } from './keys'

export const softwarePackageMutations = {
  publish: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: softwarePackageMutationKeys.publish,
      mutationFn: publishSoftwarePackage,
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: softwarePackageKeys.all }),
          queryClient.invalidateQueries({ queryKey: softwareStorageKeys.all }),
        ])
      },
    }),
  remove: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: softwarePackageMutationKeys.remove,
      mutationFn: deleteSoftwarePackage,
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: softwarePackageKeys.all }),
          queryClient.invalidateQueries({ queryKey: softwareStorageKeys.all }),
        ])
      },
    }),
}
