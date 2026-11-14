import { queryOptions } from '@tanstack/react-query'
import { api } from '@/services/api-client'
import { toScopeKey, type ApiResponse } from '@/types'
import { accessControlKeys } from '../shared/keys'

export interface AccessCheck {
  verb: string
  group?: string
  resource: string
  namespace?: string
  name?: string
}

export interface AccessDecision {
  check: AccessCheck
  allowed: boolean
  denied: boolean
  reason?: string
  evaluationError?: string
}

export interface EffectiveAccessRow extends AccessDecision {
  key: string
  label: string
  risk: 'read' | 'high' | 'unknown'
}

function serviceAccountChecks(namespace: string) {
  return [
    { label: 'List Pods', risk: 'read' as const, verb: 'list', resource: 'pods', namespace },
    { label: 'Read Pod logs', risk: 'read' as const, verb: 'get', resource: 'pods/log', namespace },
    {
      label: 'Create Deployments',
      risk: 'high' as const,
      verb: 'create',
      group: 'apps',
      resource: 'deployments',
      namespace,
    },
    {
      label: 'Update Deployments',
      risk: 'high' as const,
      verb: 'update',
      group: 'apps',
      resource: 'deployments',
      namespace,
    },
    {
      label: 'Delete Deployments',
      risk: 'high' as const,
      verb: 'delete',
      group: 'apps',
      resource: 'deployments',
      namespace,
    },
    { label: 'Read Secrets', risk: 'high' as const, verb: 'get', resource: 'secrets', namespace },
    {
      label: 'Create RoleBindings',
      risk: 'high' as const,
      verb: 'create',
      group: 'rbac.authorization.k8s.io',
      resource: 'rolebindings',
      namespace,
    },
    {
      label: 'Create ClusterRoleBindings',
      risk: 'high' as const,
      verb: 'create',
      group: 'rbac.authorization.k8s.io',
      resource: 'clusterrolebindings',
    },
    { label: 'Impersonate Users', risk: 'high' as const, verb: 'impersonate', resource: 'users' },
  ]
}

export function checkIdentity(check: AccessCheck) {
  return JSON.stringify([
    check.verb,
    check.group || '',
    check.resource,
    check.namespace || '',
    check.name || '',
  ])
}

export function accessDecisionState(decision: AccessDecision) {
  if (decision.evaluationError || (decision.allowed === true && decision.denied === true))
    return 'error'
  if (decision.allowed === true) return 'allowed'
  if (decision.denied === true) return 'denied'
  return 'unknown'
}

export function serviceAccountAccessReviewOptions(
  clusterId: string | undefined,
  namespace: string,
  name: string,
  canReview: boolean,
) {
  const checks = serviceAccountChecks(namespace)
  const definitions = new Map(checks.map((check) => [checkIdentity(check), check]))
  return queryOptions({
    queryKey: accessControlKeys.accessReview(toScopeKey(clusterId, namespace), name),
    queryFn: async () => {
      const response = await api.post<ApiResponse<{ decisions: AccessDecision[] }>>(
        `/clusters/${encodeURIComponent(clusterId!)}/access-control/access-reviews`,
        {
          subject: { kind: 'ServiceAccount', namespace, name },
          checks: checks.map(({ label: _label, risk: _risk, ...check }) => check),
        },
      )
      return response.data.decisions.map((decision): EffectiveAccessRow => {
        const key = checkIdentity(decision.check)
        const definition = definitions.get(key)
        return {
          ...decision,
          key,
          label: definition?.label ?? decision.check.verb,
          risk: definition?.risk ?? 'unknown',
        }
      })
    },
    enabled: canReview && Boolean(clusterId && namespace && name),
  })
}
