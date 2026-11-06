import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import type { Pod, ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { podQueries } from '../pods/queries'

/** The controller owns membership and order; the scoped Pod read only supplies observations. */
export function mergePodObservations(pods: readonly Pod[], observations: readonly Pod[]) {
  const byIdentity = new Map(observations.map((pod) => [`${pod.namespace}/${pod.name}`, pod]))
  return pods.map((pod) => {
    const observed = byIdentity.get(`${pod.namespace}/${pod.name}`)
    return observed
      ? {
          ...pod,
          cpu: observed.cpu,
          memory: observed.memory,
          requests: observed.requests ?? pod.requests,
          limits: observed.limits ?? pod.limits,
          containerStatuses: observed.containerStatuses ?? pod.containerStatuses,
          qosClass: observed.qosClass ?? pod.qosClass,
          allowedActions: observed.allowedActions,
        }
      : pod
  })
}

export function usePodObservations(scope: ScopeKey, pods: readonly Pod[], active: boolean) {
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = active && hasPermission(permissions, 'platform.pods.view')
  const options = podQueries.list(scope)
  const query = useQuery({ ...options, enabled: Boolean(options.enabled && canRead) })
  const observations = canRead ? readableQueryData(query) : undefined
  return { query, pods: mergePodObservations(pods, observations ?? []) }
}
