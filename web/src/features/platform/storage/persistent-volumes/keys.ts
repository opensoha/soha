import type { ScopeKey } from '@/types'
import { storageKeys } from '../shared/keys'
import { persistentVolumeKind } from './api'

export const persistentVolumeKeys = {
  all: () => storageKeys.resource(persistentVolumeKind),
  lists: () => storageKeys.lists(persistentVolumeKind),
  list: (scope: ScopeKey) => storageKeys.list(persistentVolumeKind, scope),
  detail: (scope: ScopeKey, name: string) => storageKeys.detail(persistentVolumeKind, scope, name),
  yaml: (scope: ScopeKey, name: string) => storageKeys.yaml(persistentVolumeKind, scope, name),
}
