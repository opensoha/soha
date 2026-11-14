import type { ScopeKey } from '@/types'
import { normalizeAccessControlScope } from './scope'
import type { AccessControlKind, AccessControlListFilter } from './types'

function normalizeName(name: string) {
  return name.trim()
}

export const accessControlKeys = {
  all: ['platform', 'access-control'] as const,
  resource: (kind: AccessControlKind) => [...accessControlKeys.all, kind] as const,
  lists: (kind: AccessControlKind) => [...accessControlKeys.resource(kind), 'list'] as const,
  list: (kind: AccessControlKind, scope: ScopeKey, filter?: AccessControlListFilter) =>
    [
      ...accessControlKeys.lists(kind),
      normalizeAccessControlScope(kind, scope),
      ...(filter
        ? [
            {
              subjectKind: filter.subjectKind?.trim(),
              subjectName: filter.subjectName?.trim(),
              subjectNamespace: filter.subjectNamespace?.trim(),
            },
          ]
        : []),
    ] as const,
  details: (kind: AccessControlKind) => [...accessControlKeys.resource(kind), 'detail'] as const,
  detail: (kind: AccessControlKind, scope: ScopeKey, name: string) =>
    [
      ...accessControlKeys.details(kind),
      normalizeAccessControlScope(kind, scope),
      normalizeName(name),
    ] as const,
  yaml: (kind: AccessControlKind, scope: ScopeKey, name: string) =>
    [...accessControlKeys.detail(kind, scope, name), 'yaml'] as const,
  accessReview: (scope: ScopeKey, name: string) =>
    [
      ...accessControlKeys.resource('serviceaccounts'),
      'access-review',
      normalizeAccessControlScope('serviceaccounts', scope),
      normalizeName(name),
    ] as const,
}
