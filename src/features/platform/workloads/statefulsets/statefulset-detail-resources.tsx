import { ControllerRelatedResources } from '../shared/controller-related-resources'
import type { ComponentProps } from 'react'
export function StatefulSetDetailResources(
  props: Omit<ComponentProps<typeof ControllerRelatedResources>, 'kind'>,
) {
  return <ControllerRelatedResources {...props} kind="StatefulSet" />
}
