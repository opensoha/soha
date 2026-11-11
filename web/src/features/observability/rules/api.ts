import type {
  AlertRuleEnvelope,
  AlertRuleListEnvelope,
  AlertRuleRunListEnvelope,
  AlertRuleTestResultEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import type { ApiItemsResponse, ApiResponse } from '@/types'
import type {
  AlertRule,
  AlertRulePayload,
  AlertRuleRun,
  AlertRuleTestResult,
  HealingPolicyOption,
  NotificationPolicyOption,
  TestAlertRuleInput,
  UpdateAlertRuleInput,
} from './types'

async function unwrapItems<T>(request: Promise<ApiItemsResponse<T>>): Promise<T[]> {
  const response = await request
  return response.items ?? []
}

async function unwrapItem<T>(request: Promise<ApiResponse<T>>): Promise<T> {
  const response = await request
  return response.data
}

export const observabilityRuleApi = {
  list: () => unwrapItems<AlertRule>(api.getEnvelope<AlertRuleListEnvelope>('/alert-rules')),
  detail: (ruleId: string) =>
    unwrapItem<AlertRule>(api.get<AlertRuleEnvelope>(`/alert-rules/${encodeURIComponent(ruleId)}`)),
  runs: (ruleId: string) =>
    unwrapItems<AlertRuleRun>(
      api.getEnvelope<AlertRuleRunListEnvelope>(
        `/alert-rule-runs?ruleId=${encodeURIComponent(ruleId)}`,
      ),
    ),
  notificationPolicies: () =>
    unwrapItems(
      api.getEnvelope<ApiItemsResponse<NotificationPolicyOption>>('/notification-policies'),
    ),
  healingPolicies: () =>
    unwrapItems(api.getEnvelope<ApiItemsResponse<HealingPolicyOption>>('/healing-policies')),
  create: (payload: AlertRulePayload) =>
    unwrapItem<AlertRule>(api.post<AlertRuleEnvelope>('/alert-rules', payload)),
  update: ({ id, payload }: UpdateAlertRuleInput) =>
    unwrapItem<AlertRule>(
      api.put<AlertRuleEnvelope>(`/alert-rules/${encodeURIComponent(id)}`, payload),
    ),
  test: ({ id, payload }: TestAlertRuleInput) =>
    unwrapItem<AlertRuleTestResult>(
      api.post<AlertRuleTestResultEnvelope>(`/alert-rules/${encodeURIComponent(id)}/test`, payload),
    ),
}
