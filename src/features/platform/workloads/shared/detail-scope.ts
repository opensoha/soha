import { useEffect, useLayoutEffect, useMemo, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'

export function useIsCurrentDetailTarget(targetIdentity: string) {
  const target = useMemo(() => ({ targetIdentity }), [targetIdentity])
  const activeTarget = useRef<typeof target | null>(null)
  useLayoutEffect(() => {
    activeTarget.current = target
    return () => {
      activeTarget.current = null
    }
  }, [target])
  return () => activeTarget.current === target
}

export function useWorkloadDetailScope() {
  const [searchParams] = useSearchParams()
  const { clusterId: scopedClusterId, namespace: scopedNamespace, setClusterId } =
    usePlatformScopeStore()
  const syncedCluster = useRef<string | null>(null)
  const requestedClusterId = searchParams.get('clusterId')?.trim() || null
  const hasRequestedNamespace = searchParams.has('namespace')
  const requestedNamespace = hasRequestedNamespace
    ? searchParams.get('namespace')?.trim() || null
    : undefined
  const clusterChanged = Boolean(requestedClusterId && requestedClusterId !== scopedClusterId)
  const clusterId = requestedClusterId || scopedClusterId
  const namespace =
    requestedNamespace !== undefined ? requestedNamespace : clusterChanged ? null : scopedNamespace

  useEffect(() => {
    if (syncedCluster.current === requestedClusterId) return
    syncedCluster.current = requestedClusterId
    if (requestedClusterId && scopedClusterId !== requestedClusterId) {
      setClusterId(requestedClusterId)
    }
  }, [requestedClusterId, scopedClusterId, setClusterId])

  return toScopeKey(clusterId, namespace)
}
