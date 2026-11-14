import type { QueryClient } from '@tanstack/react-query'
import { storageMutations } from '../shared/mutations'
import {
  createPersistentVolumeClaim,
  deletePersistentVolumeClaim,
  persistentVolumeClaimKind,
  updatePersistentVolumeClaimYAML,
} from './api'

export const persistentVolumeClaimMutations = {
  create: (queryClient: QueryClient) =>
    storageMutations.create(persistentVolumeClaimKind, queryClient, createPersistentVolumeClaim),
  updateYAML: (queryClient: QueryClient) =>
    storageMutations.updateYAML(
      persistentVolumeClaimKind,
      queryClient,
      updatePersistentVolumeClaimYAML,
    ),
  remove: (queryClient: QueryClient) =>
    storageMutations.remove(persistentVolumeClaimKind, queryClient, deletePersistentVolumeClaim),
}
