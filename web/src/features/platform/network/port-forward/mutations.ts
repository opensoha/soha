import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { registerPortForward, stopPortForward } from './api'
import { portForwardKeys } from './keys'
import type { PortForwardDraft, PortForwardTarget } from './types'

export const portForwardMutations = {
  register: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...portForwardKeys.all, 'register'] as const,
      mutationFn: (draft: PortForwardDraft) => registerPortForward(draft),
      onSuccess: (_data, draft) =>
        queryClient.invalidateQueries({ queryKey: portForwardKeys.list(draft.scope) }),
    }),
  stop: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...portForwardKeys.all, 'stop'] as const,
      mutationFn: (target: PortForwardTarget) => stopPortForward(target),
      onSuccess: (_data, target) =>
        queryClient.invalidateQueries({ queryKey: portForwardKeys.list(target.scope) }),
    }),
}
