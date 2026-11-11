import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { permissionSnapshotQueryKey } from '@/features/auth'
import { systemApi } from './api'
import { systemKeys, systemMutationKeys } from './keys'

function invalidateSessions(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: systemKeys.sessions.all })
}

export function invalidateAnnouncements(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: systemKeys.announcements.all })
}

function invalidateMenus(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: systemKeys.menus.all }),
    queryClient.invalidateQueries({ queryKey: permissionSnapshotQueryKey }),
  ])
}

export const systemMutations = {
  sessions: {
    revoke: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.sessions('revoke'),
        mutationFn: systemApi.sessions.revoke,
        onSuccess: () => invalidateSessions(queryClient),
      }),
    revokeMany: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.sessions('revoke-many'),
        mutationFn: systemApi.sessions.revokeMany,
        onSuccess: () => invalidateSessions(queryClient),
      }),
  },
  announcements: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.announcements('create'),
        mutationFn: systemApi.announcements.create,
        onSuccess: () => invalidateAnnouncements(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.announcements('update'),
        mutationFn: systemApi.announcements.update,
        onSuccess: () => invalidateAnnouncements(queryClient),
      }),
    publish: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.announcements('publish'),
        mutationFn: systemApi.announcements.publish,
        onSuccess: () => invalidateAnnouncements(queryClient),
      }),
    withdraw: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.announcements('withdraw'),
        mutationFn: systemApi.announcements.withdraw,
        onSuccess: () => invalidateAnnouncements(queryClient),
      }),
    remove: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.announcements('remove'),
        mutationFn: systemApi.announcements.remove,
        onSuccess: () => invalidateAnnouncements(queryClient),
      }),
  },
  menus: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.menus('create'),
        mutationFn: systemApi.menus.create,
        onSuccess: () => invalidateMenus(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.menus('update'),
        mutationFn: systemApi.menus.update,
        onSuccess: () => invalidateMenus(queryClient),
      }),
    remove: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: systemMutationKeys.menus('remove'),
        mutationFn: systemApi.menus.remove,
        onSuccess: () => invalidateMenus(queryClient),
      }),
  },
}
