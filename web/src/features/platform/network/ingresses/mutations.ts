import type { QueryClient } from '@tanstack/react-query'
import { networkMutations } from '../shared/mutations'

export const ingressMutations = {
  remove: (queryClient: QueryClient) => networkMutations.remove('ingresses', queryClient),
  updateYAML: (queryClient: QueryClient) => networkMutations.updateYAML('ingresses', queryClient),
}
