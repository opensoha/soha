import type { ScopeKey } from '@/types'
import { storageDetailQuery, storageListQuery, storageYAMLQuery } from '../shared/queries'
import {
  getPersistentVolumeClaimDetail,
  getPersistentVolumeClaimYAML,
  listPersistentVolumeClaims,
  persistentVolumeClaimKind,
} from './api'
import type { PersistentVolumeClaim, PersistentVolumeClaimDetail } from './types'

export const persistentVolumeClaimQueries = {
  list: (scope: ScopeKey) =>
    storageListQuery<PersistentVolumeClaim>(
      persistentVolumeClaimKind,
      scope,
      listPersistentVolumeClaims,
    ),
  detail: (scope: ScopeKey, name: string) =>
    storageDetailQuery<PersistentVolumeClaimDetail>(
      persistentVolumeClaimKind,
      scope,
      name,
      true,
      getPersistentVolumeClaimDetail,
    ),
  yaml: (scope: ScopeKey, name: string) =>
    storageYAMLQuery(persistentVolumeClaimKind, scope, name, true, getPersistentVolumeClaimYAML),
}
