import type { ScopeKey } from '@/types'
import { storageKeys } from '../shared/keys'
import { storageClassKind } from './api'

export const storageClassKeys = {
  all: () => storageKeys.resource(storageClassKind),
  lists: () => storageKeys.lists(storageClassKind),
  list: (scope: ScopeKey) => storageKeys.list(storageClassKind, scope),
  detail: (scope: ScopeKey, name: string) => storageKeys.detail(storageClassKind, scope, name),
  yaml: (scope: ScopeKey, name: string) => storageKeys.yaml(storageClassKind, scope, name),
}
