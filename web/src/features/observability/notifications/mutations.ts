import { mutationOptions, type QueryClient, type QueryKey } from '@tanstack/react-query'
import { observabilityKeys, observabilityMutationKeys } from '../keys'
import { observabilityNotificationApi } from './api'

function invalidate(queryClient: QueryClient, ...queryKeys: QueryKey[]) {
  return Promise.all(queryKeys.map((queryKey) => queryClient.invalidateQueries({ queryKey })))
}

export const observabilityNotificationMutations = {
  createPolicy: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('policy', 'create'),
      mutationFn: observabilityNotificationApi.createPolicy,
      onSuccess: () => invalidate(queryClient, observabilityKeys.notifications.policies()),
    }),
  updatePolicy: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('policy', 'update'),
      mutationFn: observabilityNotificationApi.updatePolicy,
      onSuccess: () => invalidate(queryClient, observabilityKeys.notifications.policies()),
    }),
  createTemplate: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('template', 'create'),
      mutationFn: observabilityNotificationApi.createTemplate,
      onSuccess: () => invalidate(queryClient, observabilityKeys.notifications.templates()),
    }),
  updateTemplate: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('template', 'update'),
      mutationFn: observabilityNotificationApi.updateTemplate,
      onSuccess: () => invalidate(queryClient, observabilityKeys.notifications.templates()),
    }),
  createChannel: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('channel', 'create'),
      mutationFn: observabilityNotificationApi.createChannel,
      onSuccess: () => invalidate(queryClient, observabilityKeys.notifications.channels()),
    }),
  updateChannel: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('channel', 'update'),
      mutationFn: observabilityNotificationApi.updateChannel,
      onSuccess: () => invalidate(queryClient, observabilityKeys.notifications.channels()),
    }),
  createRoute: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('route', 'create'),
      mutationFn: observabilityNotificationApi.createRoute,
      onSuccess: () =>
        invalidate(
          queryClient,
          observabilityKeys.notifications.routes(),
          observabilityKeys.notifications.policies(),
        ),
    }),
  updateRoute: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('route', 'update'),
      mutationFn: observabilityNotificationApi.updateRoute,
      onSuccess: () =>
        invalidate(
          queryClient,
          observabilityKeys.notifications.routes(),
          observabilityKeys.notifications.policies(),
        ),
    }),
  createSilence: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('silence', 'create'),
      mutationFn: observabilityNotificationApi.createSilence,
      onSuccess: () => invalidate(queryClient, observabilityKeys.notifications.silences()),
    }),
  updateSilence: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('silence', 'update'),
      mutationFn: observabilityNotificationApi.updateSilence,
      onSuccess: () => invalidate(queryClient, observabilityKeys.notifications.silences()),
    }),
  preview: () =>
    mutationOptions({
      mutationKey: observabilityMutationKeys.notifications('policy', 'preview'),
      mutationFn: observabilityNotificationApi.preview,
    }),
}
