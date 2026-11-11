import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  OnCallAssignmentPayload,
  OnCallAssignmentRule,
  OnCallEscalationPolicy,
  OnCallEscalationPolicyPayload,
  OnCallRotation,
  OnCallRotationPayload,
  OnCallSchedule,
  OnCallSchedulePayload,
  OnCallTask,
  OnCallUpdateInput,
  OnCallUser,
} from './types'

async function unwrapList<T>(request: Promise<ApiResponse<T[]>>): Promise<T[]> {
  const response = await request
  return response.data ?? []
}

export const observabilityOncallApi = {
  listUsers: () => unwrapList(api.get<ApiResponse<OnCallUser[]>>('/access/users')),
  listSchedules: () => unwrapList(api.get<ApiResponse<OnCallSchedule[]>>('/oncall/schedules')),
  listRotations: () => unwrapList(api.get<ApiResponse<OnCallRotation[]>>('/oncall/rotations')),
  listEscalationPolicies: () =>
    unwrapList(api.get<ApiResponse<OnCallEscalationPolicy[]>>('/oncall/escalation-policies')),
  listRoutes: () => unwrapList(api.get<ApiResponse<OnCallAssignmentRule[]>>('/oncall/routes')),
  listTasks: () =>
    unwrapList(
      api.get<ApiResponse<OnCallTask[]>>('/oncall/tasks?status=pending&status=acknowledged'),
    ),
  createSchedule: (payload: OnCallSchedulePayload) => api.post('/oncall/schedules', payload),
  updateSchedule: ({ id, payload }: OnCallUpdateInput<OnCallSchedulePayload>) =>
    api.put(`/oncall/schedules/${id}`, payload),
  createRotation: (payload: OnCallRotationPayload) => api.post('/oncall/rotations', payload),
  updateRotation: ({ id, payload }: OnCallUpdateInput<OnCallRotationPayload>) =>
    api.put(`/oncall/rotations/${id}`, payload),
  createEscalationPolicy: (payload: OnCallEscalationPolicyPayload) =>
    api.post('/oncall/escalation-policies', payload),
  updateEscalationPolicy: ({ id, payload }: OnCallUpdateInput<OnCallEscalationPolicyPayload>) =>
    api.put(`/oncall/escalation-policies/${id}`, payload),
  createRoute: (payload: OnCallAssignmentPayload) => api.post('/oncall/routes', payload),
  updateRoute: ({ id, payload }: OnCallUpdateInput<OnCallAssignmentPayload>) =>
    api.put(`/oncall/routes/${id}`, payload),
}
