import { lazy } from 'react'
import { ApplicationWorkspaceRoute } from './workspace-shell'

const Detail = lazy(() =>
  import('./detail-page').then((module) => ({ default: module.ApplicationDetailWorkspace })),
)

export function ApplicationWorkspacePage() {
  return <ApplicationWorkspaceRoute Detail={Detail} />
}
