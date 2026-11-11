import { Navigate } from 'react-router-dom'

export function ResourceCreationModalRedirect() {
  return <Navigate replace to="/?createResource=1" />
}
