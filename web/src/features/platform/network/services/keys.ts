import type { ScopeKey } from '@/types'
import { networkKeys } from '../shared/keys'
import { normalizeNetworkScope } from '../shared/scope'

export const serviceKeys = {
  all: networkKeys.resource('services'),
  lists: () => networkKeys.lists('services'),
  list: (scope: ScopeKey) => networkKeys.list('services', scope),
  details: () => networkKeys.details('services'),
  detail: (scope: ScopeKey, name: string) => networkKeys.detail('services', scope, name),
  metrics: (scope: ScopeKey, name: string) =>
    [...serviceKeys.detail(scope, name), 'metrics'] as const,
  events: (scope: ScopeKey, name: string, limit = 100) =>
    [...serviceKeys.detail(scope, name), 'events', { limit }] as const,
  yaml: (scope: ScopeKey, name: string) => networkKeys.yaml('services', scope, name),
  diagnosticPods: (scope: ScopeKey) =>
    [...serviceKeys.all, 'diagnostics', 'pods', normalizeNetworkScope(scope)] as const,
}
