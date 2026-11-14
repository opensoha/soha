import { Navigate, useLocation } from 'react-router-dom'

export function ObservabilityExploreRedirect() {
  const { search, hash } = useLocation()
  const signal = new URLSearchParams(search).get('signal')
  const pathname =
    signal === 'logs'
      ? '/monitoring-workbench/logs'
      : signal === 'traces'
        ? '/monitoring-workbench/traces'
        : '/monitoring-workbench/metrics'
  return <Navigate replace to={{ pathname, search, hash }} />
}
