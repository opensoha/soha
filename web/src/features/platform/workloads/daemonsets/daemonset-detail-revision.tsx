import { ControllerRevisionDetail } from '../shared/controller-revision-detail'
import { daemonSetQueries } from './queries'
import type { ComponentProps } from 'react'
export function DaemonSetDetailRevision(
  props: Omit<ComponentProps<typeof ControllerRevisionDetail>, 'revisionOptions'>,
) {
  return <ControllerRevisionDetail {...props} revisionOptions={daemonSetQueries.revision} />
}
