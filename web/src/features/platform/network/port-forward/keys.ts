import type { ScopeKey } from '@/types'
import { networkKeys } from '../shared/keys'
import { normalizeNetworkScope } from '../shared/scope'

export const portForwardKeys = {
  all: [...networkKeys.all, 'port-forwards'] as const,
  list: (scope: ScopeKey) => [...portForwardKeys.all, normalizeNetworkScope(scope)] as const,
}
