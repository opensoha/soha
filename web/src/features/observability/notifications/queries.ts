import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import { observabilityNotificationApi } from './api'

export const observabilityNotificationQueries = {
  preview: (policyId: string, eventId: string, enabled = true) =>
    queryOptions({
      queryKey: observabilityKeys.notifications.preview(policyId, eventId),
      queryFn: () => observabilityNotificationApi.preview({ policyId, eventId }),
      enabled: enabled && Boolean(policyId && eventId),
    }),
  channels: () =>
    queryOptions({
      queryKey: observabilityKeys.notifications.channels(),
      queryFn: observabilityNotificationApi.listChannels,
    }),
  previewEvents: () =>
    queryOptions({
      queryKey: observabilityKeys.notifications.previewEvents(),
      queryFn: observabilityNotificationApi.listPreviewEvents,
    }),
  policies: () =>
    queryOptions({
      queryKey: observabilityKeys.notifications.policies(),
      queryFn: observabilityNotificationApi.listPolicies,
    }),
  templates: () =>
    queryOptions({
      queryKey: observabilityKeys.notifications.templates(),
      queryFn: observabilityNotificationApi.listTemplates,
    }),
  routes: () =>
    queryOptions({
      queryKey: observabilityKeys.notifications.routes(),
      queryFn: observabilityNotificationApi.listRoutes,
    }),
  silences: () =>
    queryOptions({
      queryKey: observabilityKeys.notifications.silences(),
      queryFn: observabilityNotificationApi.listSilences,
    }),
  oncallSchedules: () =>
    queryOptions({
      queryKey: observabilityKeys.notifications.oncallSchedules(),
      queryFn: observabilityNotificationApi.listOncallSchedules,
    }),
  oncallPolicies: () =>
    queryOptions({
      queryKey: observabilityKeys.notifications.oncallPolicies(),
      queryFn: observabilityNotificationApi.listOncallPolicies,
    }),
}
