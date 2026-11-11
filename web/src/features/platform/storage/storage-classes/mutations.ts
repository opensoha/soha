import type { QueryClient } from '@tanstack/react-query'
import { storageMutations } from '../shared/mutations'
import {
  createStorageClass,
  deleteStorageClass,
  storageClassKind,
  updateStorageClassYAML,
} from './api'

export const storageClassMutations = {
  create: (queryClient: QueryClient) =>
    storageMutations.create(storageClassKind, queryClient, createStorageClass),
  updateYAML: (queryClient: QueryClient) =>
    storageMutations.updateYAML(storageClassKind, queryClient, updateStorageClassYAML),
  remove: (queryClient: QueryClient) =>
    storageMutations.remove(storageClassKind, queryClient, deleteStorageClass),
}
