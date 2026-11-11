import { Navigate, useLocation } from 'react-router-dom'

export function WorkflowCenterRedirect() {
  const location = useLocation()
  const search = new URLSearchParams(location.search)
  search.delete('kind')
  search.delete('tab')
  return (
    <Navigate
      replace
      to={{
        pathname:
          location.pathname === '/delivery/batches' ? '/execution-history' : '/release-board',
        search: search.toString(),
        hash: location.hash,
      }}
    />
  )
}
