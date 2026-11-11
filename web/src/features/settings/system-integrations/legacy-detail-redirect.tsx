import { Navigate, useLocation, useParams } from 'react-router-dom'

export function LegacySourceConnectionDetailRedirect() {
  const { integrationId = '' } = useParams<{ integrationId: string }>()
  const { hash, search } = useLocation()
  return (
    <Navigate
      replace
      to={{ pathname: `/settings/source-control/${encodeURIComponent(integrationId)}`, search, hash }}
    />
  )
}
