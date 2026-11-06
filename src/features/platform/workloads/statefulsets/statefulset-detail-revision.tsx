import { ControllerRevisionDetail } from '../shared/controller-revision-detail'
import { statefulSetQueries } from './queries'
import type { ComponentProps } from 'react'
export function StatefulSetDetailRevision(
  props: Omit<ComponentProps<typeof ControllerRevisionDetail>, 'revisionOptions'>,
) {
  return <ControllerRevisionDetail {...props} revisionOptions={statefulSetQueries.revision} />
}
