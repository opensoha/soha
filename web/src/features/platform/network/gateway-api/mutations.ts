import type { QueryClient } from '@tanstack/react-query'
import { networkMutations } from '../shared/mutations'
import type { GatewayAPIKind } from './types'

export const gatewayAPIMutations = {
  remove: (
    kind: Extract<GatewayAPIKind, 'gatewayclasses' | 'gateways'>,
    queryClient: QueryClient,
  ) => networkMutations.remove(kind, queryClient),
  updateYAML: (
    kind: Extract<GatewayAPIKind, 'gatewayclasses' | 'gateways'>,
    queryClient: QueryClient,
  ) => networkMutations.updateYAML(kind, queryClient),
}
