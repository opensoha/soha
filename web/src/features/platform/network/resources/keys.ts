import type { ScopeKey } from '@/types'
import { networkKeys } from '../shared/keys'
import type { NetworkCoreKind } from './types'

export const networkCoreKeys = {
  lists: (kind: NetworkCoreKind) => networkKeys.lists(kind),
  list: (kind: NetworkCoreKind, scope: ScopeKey) => networkKeys.list(kind, scope),
  detail: (kind: NetworkCoreKind, scope: ScopeKey, name: string) =>
    networkKeys.detail(kind, scope, name),
}
