import { ControllerRelatedResources } from '../shared/controller-related-resources'
import type { ComponentProps } from 'react'
export function DaemonSetDetailResources(
  props: Omit<ComponentProps<typeof ControllerRelatedResources>, 'kind'>,
) {
  return <ControllerRelatedResources {...props} kind="DaemonSet" />
}
