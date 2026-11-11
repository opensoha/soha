import type { QueryClient } from '@tanstack/react-query'
import { invalidateAccessTeams } from '../shared/mutations'
import { directorySyncApi } from './api'
import { directorySyncKeys } from './queries'
import type { DirectoryConnectionInput } from './types'

const invalidate = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: directorySyncKeys.all })

export const directorySyncMutations = {
  create: (queryClient: QueryClient) => ({
    mutationFn: (input: DirectoryConnectionInput) => directorySyncApi.createConnection(input),
    onSuccess: () => invalidate(queryClient),
  }),
  update: (queryClient: QueryClient) => ({
    mutationFn: ({ id, input }: { id: string; input: DirectoryConnectionInput }) =>
      directorySyncApi.updateConnection(id, input),
    onSuccess: () => invalidate(queryClient),
  }),
  validate: (queryClient: QueryClient) => ({
    mutationFn: directorySyncApi.validateConnection,
    onSuccess: () => invalidate(queryClient),
  }),
  sync: (queryClient: QueryClient) => ({
    mutationFn: directorySyncApi.startSync,
    onSuccess: async () => {
      await invalidate(queryClient)
      await invalidateAccessTeams(queryClient)
    },
  }),
  cancel: (queryClient: QueryClient) => ({
    mutationFn: directorySyncApi.cancelSync,
    onSuccess: () => invalidate(queryClient),
  }),
  retryEvent: (queryClient: QueryClient) => ({
    mutationFn: ({ connectionId, eventId }: { connectionId: string; eventId: string }) =>
      directorySyncApi.retryEvent(connectionId, eventId),
    onSuccess: (_event: unknown, { connectionId }: { connectionId: string; eventId: string }) =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: directorySyncKeys.events(connectionId) }),
        queryClient.invalidateQueries({ queryKey: directorySyncKeys.runtimeStatus(connectionId) }),
      ]),
  }),
  resolveConflict: (queryClient: QueryClient) => ({
    mutationFn: ({ id, resolution }: { id: string; resolution: 'ignore' | 'retry' }) =>
      directorySyncApi.resolveConflict(id, resolution),
    onSuccess: () => invalidate(queryClient),
  }),
}
