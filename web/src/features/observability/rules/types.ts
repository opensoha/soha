import type {
  AlertRule as ContractAlertRule,
  AlertRuleRun as ContractAlertRuleRun,
  AlertRuleStringMap,
  AlertRuleTestResult as ContractAlertRuleTestResult,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type { ObservabilityPayloadMap } from '../shared/types'

export type AlertRuleDatasourceSelector = ObservabilityPayloadMap
export type AlertRuleQuerySpec = ObservabilityPayloadMap
export type AlertRuleThresholdSpec = ObservabilityPayloadMap
export type AlertRuleTestResult = ContractAlertRuleTestResult
export type AlertRuleTextMap = AlertRuleStringMap
export type AlertRule = ContractAlertRule
export type AlertRuleRun = ContractAlertRuleRun

export interface AlertRuleFormValues {
  id?: string
  name: string
  mode?: 'simple' | 'advanced'
  ruleType: string
  datasourceSelector: string
  querySpec: string
  thresholdSpec: string
  metricKey?: string
  operator?: string
  thresholdValue?: number
  reducer?: string
  windowMinutes?: number
  stepSeconds?: number
  clusterId?: string
  namespace?: string
  workload?: string
  severity?: string
  summary?: string
  forSeconds: number
  groupBy: string
  labels: string
  annotations: string
  notificationPolicyId?: string
  healingPolicyIds: string[]
  enabled: boolean
}

export interface AlertRulePayload {
  id?: string
  name: string
  ruleType: string
  datasourceSelector: AlertRuleDatasourceSelector
  querySpec: AlertRuleQuerySpec
  thresholdSpec: AlertRuleThresholdSpec
  forSeconds: number
  groupBy: string[]
  labels: ObservabilityPayloadMap
  annotations: ObservabilityPayloadMap
  notificationPolicyId: string
  healingPolicyIds: string[]
  enabled: boolean
}

export interface NotificationPolicyOption {
  id: string
  name: string
  enabled: boolean
}

export interface HealingPolicyOption {
  id: string
  name: string
  enabled: boolean
}

export interface UpdateAlertRuleInput {
  id: string
  payload: AlertRulePayload
}

export interface TestAlertRuleInput {
  id: string
  payload: AlertRulePayload
}
