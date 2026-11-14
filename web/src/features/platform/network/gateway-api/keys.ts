import type { ScopeKey } from '@/types'
import { networkKeys } from '../shared/keys'
import type { GatewayAPIKind } from './types'

export const gatewayAPIKeys = {
  all: [...networkKeys.all, 'gateway-api'] as const,
  lists: (kind: GatewayAPIKind) => networkKeys.lists(kind),
  list: (kind: GatewayAPIKind, scope: ScopeKey) => networkKeys.list(kind, scope),
  detail: (kind: GatewayAPIKind, scope: ScopeKey, name: string) =>
    networkKeys.detail(kind, scope, name),
}
