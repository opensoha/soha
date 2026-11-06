import type { ReactNode } from 'react'
import { Drawer } from 'antd'
import {
  DeliveryGatewayReadinessPanel,
  type DeliveryGatewayReadinessProps,
} from '../delivery-gateway-readiness'
import type { ReleaseBoardEntry } from '../types'

export function EvidencePanelHeader({
  title,
  description,
  actions,
}: {
  title: string
  description?: string
  actions?: ReactNode
}) {
  return (
    <header className="soha-delivery-evidence-heading">
      <div>
        <h2>{title}</h2>
        {description ? <span>{description}</span> : null}
      </div>
      {actions ? <div className="soha-delivery-evidence-actions">{actions}</div> : null}
    </header>
  )
}

export function DeliveryAssistDrawer({
  open,
  onClose,
  ...props
}: DeliveryGatewayReadinessProps & {
  open: boolean
  onClose: () => void
}) {
  return (
    <Drawer title={props.title} open={open} onClose={onClose} size="large" destroyOnHidden>
      <DeliveryGatewayReadinessPanel {...props} />
    </Drawer>
  )
}

export function deliveryEvidenceContext(
  board: ReleaseBoardEntry[],
  applicationId: string,
  bindingId?: string,
) {
  const application = board.find((item) => item.applicationId === applicationId)
  const binding = bindingId
    ? board.find(
        (item) =>
          item.applicationEnvironmentId === bindingId && item.applicationId === applicationId,
      )
    : undefined
  return {
    application: application?.applicationName || applicationId,
    environment: binding?.environmentName || bindingId,
    binding,
  }
}
