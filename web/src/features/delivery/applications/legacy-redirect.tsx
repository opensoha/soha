import { Navigate } from 'react-router-dom'

export function ApplicationCreateRedirect() {
  return <Navigate replace to="/applications?action=create" />
}
