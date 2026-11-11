import type { ScopeKey } from '@/types'
import { storageDetailQuery, storageListQuery, storageYAMLQuery } from '../shared/queries'
import {
  getStorageClassDetail,
  getStorageClassYAML,
  listStorageClasses,
  storageClassKind,
} from './api'
import type { StorageClass, StorageClassDetail } from './types'

export const storageClassQueries = {
  list: (scope: ScopeKey) =>
    storageListQuery<StorageClass>(storageClassKind, scope, listStorageClasses),
  detail: (scope: ScopeKey, name: string) =>
    storageDetailQuery<StorageClassDetail>(
      storageClassKind,
      scope,
      name,
      false,
      getStorageClassDetail,
    ),
  yaml: (scope: ScopeKey, name: string) =>
    storageYAMLQuery(storageClassKind, scope, name, false, getStorageClassYAML),
}
