import { Navigate, useLocation } from 'react-router-dom'

export function ComputeBuildTasksPage() {
  const location = useLocation()
  const search = new URLSearchParams(location.search)
  if (!search.has('category')) search.set('category', 'build')
  return <Navigate replace to={`/compute/tasks/operations?${search.toString()}`} />
}
