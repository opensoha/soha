import type { ScopeKey } from '@/types'
import { storageKeys } from '../shared/keys'
import { persistentVolumeClaimKind } from './api'

export const persistentVolumeClaimKeys = {
  all: () => storageKeys.resource(persistentVolumeClaimKind),
  lists: () => storageKeys.lists(persistentVolumeClaimKind),
  list: (scope: ScopeKey) => storageKeys.list(persistentVolumeClaimKind, scope),
  detail: (scope: ScopeKey, name: string) =>
    storageKeys.detail(persistentVolumeClaimKind, scope, name),
  yaml: (scope: ScopeKey, name: string) => storageKeys.yaml(persistentVolumeClaimKind, scope, name),
}
