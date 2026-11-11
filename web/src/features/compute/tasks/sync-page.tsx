import { Navigate, useLocation } from 'react-router-dom'

export function ComputeSyncTasksPage() {
  const location = useLocation()
  const search = new URLSearchParams(location.search)
  if (!search.has('category')) search.set('category', 'sync')
  return <Navigate replace to={`/compute/tasks/operations?${search.toString()}`} />
}
