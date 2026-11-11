import type { QueryClient } from '@tanstack/react-query'
import { storageMutations } from '../shared/mutations'
import {
  createPersistentVolume,
  deletePersistentVolume,
  persistentVolumeKind,
  updatePersistentVolumeYAML,
} from './api'

export const persistentVolumeMutations = {
  create: (queryClient: QueryClient) =>
    storageMutations.create(persistentVolumeKind, queryClient, createPersistentVolume),
  updateYAML: (queryClient: QueryClient) =>
    storageMutations.updateYAML(persistentVolumeKind, queryClient, updatePersistentVolumeYAML),
  remove: (queryClient: QueryClient) =>
    storageMutations.remove(persistentVolumeKind, queryClient, deletePersistentVolume),
}
