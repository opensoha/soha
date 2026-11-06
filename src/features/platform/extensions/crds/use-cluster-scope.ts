import { useLayoutEffect, useRef } from 'react'
import type { SetURLSearchParams } from 'react-router-dom'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'

/** URL scope wins on entry; an accepted global selection then updates that URL. */
export function useCRDClusterScope(
  params: URLSearchParams,
  setParams: SetURLSearchParams,
  requestLeave?: (action: () => void) => void,
) {
  const storedClusterId = usePlatformScopeStore((state) => state.clusterId)
  const requestedClusterId = params.get('clusterId')?.trim() || null
  const clusterId = requestedClusterId || storedClusterId
  const changingScope = useRef(false)
  useLayoutEffect(() => {
    if (!requestedClusterId || usePlatformScopeStore.getState().clusterId === requestedClusterId)
      return
    changingScope.current = true
    usePlatformScopeStore.getState().setClusterId(requestedClusterId)
    changingScope.current = false
  }, [requestedClusterId])
  useLayoutEffect(
    () =>
      usePlatformScopeStore.subscribe((next, previous) => {
        if (changingScope.current || next.clusterId === previous.clusterId) return
        // Revert synchronously: a blocked switch must never unmount or retarget an editor.
        changingScope.current = true
        usePlatformScopeStore.setState({
          clusterId: previous.clusterId,
          namespace: previous.namespace,
        })
        changingScope.current = false
        const accept = () => {
          changingScope.current = true
          usePlatformScopeStore.setState({ clusterId: next.clusterId, namespace: next.namespace })
          changingScope.current = false
          setParams((current) => {
            const updated = new URLSearchParams(current)
            if (next.clusterId) updated.set('clusterId', next.clusterId)
            else updated.delete('clusterId')
            for (const key of ['resource', 'resourceNamespace', 'namespace', 'tab'])
              updated.delete(key)
            return updated
          })
        }
        if (requestLeave) requestLeave(accept)
        else accept()
      }),
    [requestLeave, setParams],
  )
  return { clusterId, scopeReady: clusterId === storedClusterId }
}
