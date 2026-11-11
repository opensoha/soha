import { Navigate, useLocation } from 'react-router-dom'

export default function AdmissionSettingsRedirect() {
  const { pathname, search, hash } = useLocation()
  const params = new URLSearchParams(search)
  if (!params.has('tab')) {
    params.set('tab', pathname.split('/').filter(Boolean).pop()?.toLowerCase() ?? '')
  }
  return (
    <Navigate
      replace
      to={{ pathname: '/network-access/settings', search: params.toString(), hash }}
    />
  )
}
