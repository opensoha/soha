import type { ScopeKey } from '@/types'
import { networkKeys } from '../shared/keys'

export const ingressKeys = {
  all: networkKeys.resource('ingresses'),
  lists: () => networkKeys.lists('ingresses'),
  list: (scope: ScopeKey) => networkKeys.list('ingresses', scope),
  details: () => networkKeys.details('ingresses'),
  detail: (scope: ScopeKey, name: string) => networkKeys.detail('ingresses', scope, name),
  events: (scope: ScopeKey, name: string) =>
    [...ingressKeys.detail(scope, name), 'events'] as const,
  yaml: (scope: ScopeKey, name: string) => networkKeys.yaml('ingresses', scope, name),
}
