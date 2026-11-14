import { useMemo } from 'react'
import { useLocation, useSearchParams } from 'react-router-dom'
import { normalizeAIWorkbenchMode } from '../navigation'
import type { WorkbenchMode, WorkbenchSessionScope } from '../types'

export function useWorkbenchRouteState() {
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedSessionId = searchParams.get('session') || undefined
  const searchMode = normalizeAIWorkbenchMode(searchParams.get('mode')) || 'general'
  const pathMode = useMemo<WorkbenchMode>(() => {
    if (location.pathname === '/ai-workbench/root-cause') return 'root_cause'
    if (location.pathname === '/ai-workbench/performance') return 'performance'
    if (location.pathname === '/ai-workbench/chat') return searchMode
    return 'general'
  }, [location.pathname, searchMode])
  const isExplicitRouteMode =
    location.pathname === '/ai-workbench/root-cause' ||
    location.pathname === '/ai-workbench/performance' ||
    (location.pathname === '/ai-workbench/chat' && searchParams.has('mode'))
  const draftScope = useMemo<WorkbenchSessionScope>(
    () => ({
      clusterId: searchParams.get('clusterId') || undefined,
      namespace: searchParams.get('namespace') || undefined,
      workload: searchParams.get('workload') || undefined,
      service: searchParams.get('service') || undefined,
      pod: searchParams.get('pod') || undefined,
      node: searchParams.get('node') || undefined,
      alertId: searchParams.get('alertId') || undefined,
      timeRangeMinutes: Number(searchParams.get('timeRangeMinutes') || 60) || 60,
    }),
    [searchParams],
  )

  const updateSearchParams = (patch: Record<string, string | undefined>) => {
    const next = new URLSearchParams(searchParams)
    for (const [key, value] of Object.entries(patch)) {
      if (!value) next.delete(key)
      else next.set(key, value)
    }
    setSearchParams(next)
  }

  return {
    personal: location.pathname.startsWith('/heya'),
    feedView: location.pathname.startsWith('/heya') && searchParams.get('view') === 'feed',
    messageAnchor: searchParams.get('message') || '',
    messageSearchQuery: searchParams.get('q') || '',
    draftScope,
    isExplicitRouteMode,
    location,
    pathMode,
    requestedSessionId,
    searchParams,
    setSearchParams,
    updateSearchParams,
  }
}
