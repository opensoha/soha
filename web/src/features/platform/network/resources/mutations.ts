import type { QueryClient } from '@tanstack/react-query'
import { networkMutations } from '../shared/mutations'
import type { NetworkCoreKind } from './types'

export const networkCoreMutations = {
  remove: (kind: NetworkCoreKind, queryClient: QueryClient) =>
    networkMutations.remove(kind, queryClient),
  updateYAML: (kind: NetworkCoreKind, queryClient: QueryClient) =>
    networkMutations.updateYAML(kind, queryClient),
}
