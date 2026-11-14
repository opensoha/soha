import type { ScopeKey } from '@/types'
import { normalizeNetworkScope } from './scope'
import type { NetworkKind } from './types'

function normalizeName(name: string) {
  return name.trim()
}

export const networkKeys = {
  all: ['platform', 'network'] as const,
  resource: (kind: NetworkKind) => [...networkKeys.all, kind] as const,
  lists: (kind: NetworkKind) => [...networkKeys.resource(kind), 'list'] as const,
  list: (kind: NetworkKind, scope: ScopeKey) =>
    [...networkKeys.lists(kind), normalizeNetworkScope(scope)] as const,
  details: (kind: NetworkKind) => [...networkKeys.resource(kind), 'detail'] as const,
  detail: (kind: NetworkKind, scope: ScopeKey, name: string) =>
    [...networkKeys.details(kind), normalizeNetworkScope(scope), normalizeName(name)] as const,
  yaml: (kind: NetworkKind, scope: ScopeKey, name: string) =>
    [...networkKeys.detail(kind, scope, name), 'yaml'] as const,
}
