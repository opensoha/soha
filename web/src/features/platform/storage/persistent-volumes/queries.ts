import type { ScopeKey } from '@/types'
import { storageDetailQuery, storageListQuery, storageYAMLQuery } from '../shared/queries'
import {
  getPersistentVolumeDetail,
  getPersistentVolumeYAML,
  listPersistentVolumes,
  persistentVolumeKind,
} from './api'
import type { PersistentVolume, PersistentVolumeDetail } from './types'

export const persistentVolumeQueries = {
  list: (scope: ScopeKey) =>
    storageListQuery<PersistentVolume>(persistentVolumeKind, scope, listPersistentVolumes),
  detail: (scope: ScopeKey, name: string) =>
    storageDetailQuery<PersistentVolumeDetail>(
      persistentVolumeKind,
      scope,
      name,
      false,
      getPersistentVolumeDetail,
    ),
  yaml: (scope: ScopeKey, name: string) =>
    storageYAMLQuery(persistentVolumeKind, scope, name, false, getPersistentVolumeYAML),
}
