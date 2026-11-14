import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import { observabilityRuleApi } from './api'

export const observabilityRuleQueries = {
  list: () =>
    queryOptions({
      queryKey: observabilityKeys.rules.list(),
      queryFn: observabilityRuleApi.list,
    }),
  detail: (ruleId: string) =>
    queryOptions({
      queryKey: observabilityKeys.rules.detail(ruleId),
      queryFn: () => observabilityRuleApi.detail(ruleId),
    }),
  runs: (ruleId: string) =>
    queryOptions({
      queryKey: observabilityKeys.rules.runs(ruleId),
      queryFn: () => observabilityRuleApi.runs(ruleId),
    }),
  notificationPolicies: () =>
    queryOptions({
      queryKey: observabilityKeys.notifications.policies(),
      queryFn: observabilityRuleApi.notificationPolicies,
    }),
  healingPolicies: () =>
    queryOptions({
      queryKey: observabilityKeys.alerts.healingPolicies(),
      queryFn: observabilityRuleApi.healingPolicies,
    }),
}
