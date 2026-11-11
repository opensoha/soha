import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  NotificationAlertEventOption,
  NotificationChannel,
  NotificationChannelPayload,
  NotificationOncallOption,
  NotificationPolicy,
  NotificationPolicyPayload,
  NotificationPreviewInput,
  NotificationPreviewItem,
  NotificationRoute,
  NotificationRoutePayload,
  NotificationSilence,
  NotificationSilencePayload,
  NotificationTemplate,
  NotificationTemplatePayload,
  NotificationUpdateInput,
} from './types'

async function unwrapList<T>(request: Promise<ApiResponse<T[]>>): Promise<T[]> {
  const response = await request
  return response.data ?? []
}

export const observabilityNotificationApi = {
  listChannels: () =>
    unwrapList(api.get<ApiResponse<NotificationChannel[]>>('/notification-channels')),
  listPreviewEvents: () =>
    unwrapList(api.get<ApiResponse<NotificationAlertEventOption[]>>('/alert-events?limit=20')),
  listPolicies: () =>
    unwrapList(api.get<ApiResponse<NotificationPolicy[]>>('/notification-policies')),
  listTemplates: () =>
    unwrapList(api.get<ApiResponse<NotificationTemplate[]>>('/notification-templates')),
  listRoutes: () => unwrapList(api.get<ApiResponse<NotificationRoute[]>>('/alert-routes')),
  listSilences: () => unwrapList(api.get<ApiResponse<NotificationSilence[]>>('/alert-silences')),
  listOncallSchedules: () =>
    unwrapList(api.get<ApiResponse<NotificationOncallOption[]>>('/oncall/schedules')),
  listOncallPolicies: () =>
    unwrapList(api.get<ApiResponse<NotificationOncallOption[]>>('/oncall/escalation-policies')),
  createPolicy: (payload: NotificationPolicyPayload) => api.post('/notification-policies', payload),
  updatePolicy: ({ id, payload }: NotificationUpdateInput<NotificationPolicyPayload>) =>
    api.put(`/notification-policies/${id}`, payload),
  createTemplate: (payload: NotificationTemplatePayload) =>
    api.post('/notification-templates', payload),
  updateTemplate: ({ id, payload }: NotificationUpdateInput<NotificationTemplatePayload>) =>
    api.put(`/notification-templates/${id}`, payload),
  createChannel: (payload: NotificationChannelPayload) =>
    api.post('/notification-channels', payload),
  updateChannel: ({ id, payload }: NotificationUpdateInput<NotificationChannelPayload>) =>
    api.put(`/notification-channels/${id}`, payload),
  createRoute: (payload: NotificationRoutePayload) => api.post('/alert-routes', payload),
  updateRoute: ({ id, payload }: NotificationUpdateInput<NotificationRoutePayload>) =>
    api.put(`/alert-routes/${id}`, payload),
  createSilence: (payload: NotificationSilencePayload) => api.post('/alert-silences', payload),
  updateSilence: ({ id, payload }: NotificationUpdateInput<NotificationSilencePayload>) =>
    api.put(`/alert-silences/${id}`, payload),
  preview: async ({ policyId, eventId }: NotificationPreviewInput) => {
    const response = await api.get<ApiResponse<NotificationPreviewItem[]>>(
      `/notification-policies/${policyId}/preview?eventId=${encodeURIComponent(eventId)}`,
    )
    return response.data ?? []
  },
}
