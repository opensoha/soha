import type { CapabilityPlan } from '@opensoha/contracts/gen/ts/sohaapi'
import { mutationOptions } from '@tanstack/react-query'
import { observeApi } from './api'
import { observeMutationKeys } from './keys'
import type {
  AutomationPolicy,
  AutomationPolicyFormValues,
  AutomationPolicyMutationInput,
  InspectionTaskFormValues,
  InspectionTaskMutationInput,
  PatchSessionInput,
} from './types'

const AUTOMATION_ANALYSIS_KINDS = new Set([
  'root_cause',
  'performance',
  'trace',
  'inspection_review',
])

export function inspectionTaskPayload(values: InspectionTaskFormValues) {
  const analysisProfileId = String(values.analysisProfileId ?? '').trim()
  const metadata = { ...values.metadata }
  delete metadata.analysisProfileId
  if (analysisProfileId) metadata.analysisProfileId = analysisProfileId
  const capability = values.mode === 'capability'
  const capabilityPlan = capability
    ? (JSON.parse(values.planJSON || '{}') as CapabilityPlan)
    : undefined
  return {
    ...(values.id ? { id: values.id } : {}),
    ...(values.expectedRevision ? { expectedRevision: values.expectedRevision } : {}),
    ...(capability
      ? {
          capabilityPlan,
          aiClientId: values.aiClientId || undefined,
          skillId: values.skillId || undefined,
          trigger:
            values.triggerKind === 'alert'
              ? {
                  kind: 'alert' as const,
                  alertRuleId: values.alertRuleId,
                  maxEventAgeSeconds: values.maxEventAgeSeconds || 3600,
                }
              : { kind: 'schedule' as const },
        }
      : {}),
    title: String(values.title ?? '').trim(),
    scopeType: String(values.scopeType || 'platform'),
    clusterId: String(values.clusterId ?? '').trim(),
    namespace: String(values.namespace ?? '').trim(),
    checks: capability ? [] : Array.isArray(values.checks) ? values.checks : [],
    enabled: Boolean(values.enabled),
    intervalMinutes: Math.max(Number(values.intervalMinutes || 30), capability ? 1 : 5),
    metadata,
  }
}

export function automationPolicyPayload(values: AutomationPolicyFormValues) {
  const secondsOrDefault = (value: number | undefined, fallback: number) => {
    const numberValue = Number(value ?? fallback)
    return Math.max(Number.isFinite(numberValue) ? numberValue : fallback, 60)
  }
  const analysisKinds = Array.isArray(values.analysisKinds)
    ? values.analysisKinds
        .map((item) => String(item).trim())
        .filter((item) => AUTOMATION_ANALYSIS_KINDS.has(item))
    : []
  const triggerLabelKey = String(values.triggerLabelKey ?? '').trim()
  const triggerLabelValue = String(values.triggerLabelValue ?? '').trim()
  return {
    name: String(values.name ?? '').trim(),
    triggerType: 'alert_webhook',
    analysisKinds: analysisKinds.length > 0 ? analysisKinds : ['root_cause'],
    agentProviderId: String(values.agentProviderId ?? '').trim() || 'internal',
    triggerConditions: {
      severity: Array.isArray(values.triggerSeverity)
        ? values.triggerSeverity.map((item) => String(item).trim()).filter(Boolean)
        : [],
      status: Array.isArray(values.triggerStatus)
        ? values.triggerStatus.map((item) => String(item).trim()).filter(Boolean)
        : [],
      min_duration_seconds: Number(values.triggerMinDurationSeconds ?? 0),
      time_range_minutes: Number(values.triggerTimeRangeMinutes ?? 0),
      labels: triggerLabelKey && triggerLabelValue ? { [triggerLabelKey]: triggerLabelValue } : {},
    },
    dedupWindowSeconds: secondsOrDefault(values.dedupWindowSeconds, 900),
    analysisProfileId: String(values.analysisProfileId ?? '').trim() || 'default',
    remediationPolicy: String(values.remediationPolicy ?? '').trim() || 'suggest_only',
    approvalPolicy: {
      required: Boolean(values.approvalRequired),
      approverRoles: Array.isArray(values.approvalRoles)
        ? values.approvalRoles.map((item) => String(item).trim()).filter(Boolean)
        : [],
    },
    cooldownSeconds: secondsOrDefault(values.cooldownSeconds, 900),
    enabled: Boolean(values.enabled),
  }
}

export function policyFormValuesFromRecord(policy: AutomationPolicy): AutomationPolicyFormValues {
  const conditions = policy.triggerConditions ?? {}
  const labels = (conditions.labels as Record<string, unknown> | undefined) ?? {}
  const labelKey = Object.keys(labels)[0] ?? ''
  const approval = policy.approvalPolicy ?? {}
  const analysisKinds = policy.analysisKinds
    ?.map((item) => String(item).trim())
    .filter((item) => AUTOMATION_ANALYSIS_KINDS.has(item))
  return {
    name: policy.name,
    triggerType: policy.triggerType === 'alert_webhook' ? policy.triggerType : 'alert_webhook',
    analysisKinds: analysisKinds?.length ? analysisKinds : ['root_cause'],
    agentProviderId: policy.agentProviderId || 'internal',
    analysisProfileId: policy.analysisProfileId || 'default',
    remediationPolicy: policy.remediationPolicy || 'suggest_only',
    dedupWindowSeconds: policy.dedupWindowSeconds || 900,
    cooldownSeconds: policy.cooldownSeconds || 900,
    enabled: policy.enabled,
    triggerSeverity: Array.isArray(conditions.severity)
      ? conditions.severity.map((item) => String(item))
      : [],
    triggerStatus: Array.isArray(conditions.status)
      ? conditions.status.map((item) => String(item))
      : [],
    triggerMinDurationSeconds: Number(conditions.min_duration_seconds ?? 120),
    triggerLabelKey: labelKey,
    triggerLabelValue: String(labels[labelKey] ?? ''),
    triggerTimeRangeMinutes: Number(conditions.time_range_minutes ?? 60),
    approvalRequired: Boolean(approval.required),
    approvalRoles: Array.isArray(approval.approverRoles)
      ? approval.approverRoles.map((item) => String(item))
      : [],
  }
}

export const observeMutations = {
  operations: {
    createSession: () =>
      mutationOptions({
        mutationKey: observeMutationKeys.operations('create-session'),
        mutationFn: observeApi.operations.createSession,
      }),
    createTask: () =>
      mutationOptions({
        mutationKey: observeMutationKeys.operations('create-task'),
        mutationFn: (values: InspectionTaskFormValues) =>
          observeApi.operations.createTask(inspectionTaskPayload(values)),
      }),
    updateTask: () =>
      mutationOptions({
        mutationKey: observeMutationKeys.operations('update-task'),
        mutationFn: ({ taskId, values }: InspectionTaskMutationInput) =>
          observeApi.operations.updateTask({ taskId, values: inspectionTaskPayload(values) }),
      }),
    deleteTask: () =>
      mutationOptions({
        mutationKey: observeMutationKeys.operations('delete-task'),
        mutationFn: observeApi.operations.deleteTask,
      }),
    createPolicy: () =>
      mutationOptions({
        mutationKey: observeMutationKeys.operations('create-policy'),
        mutationFn: (values: AutomationPolicyFormValues) =>
          observeApi.operations.createPolicy(automationPolicyPayload(values)),
      }),
    updatePolicy: () =>
      mutationOptions({
        mutationKey: observeMutationKeys.operations('update-policy'),
        mutationFn: ({ policyId, values }: AutomationPolicyMutationInput) =>
          observeApi.operations.updatePolicy({
            policyId,
            values: automationPolicyPayload(values),
          }),
      }),
    deletePolicy: () =>
      mutationOptions({
        mutationKey: observeMutationKeys.operations('delete-policy'),
        mutationFn: observeApi.operations.deletePolicy,
      }),
    executeTask: () =>
      mutationOptions({
        mutationKey: observeMutationKeys.operations('execute-task'),
        mutationFn: observeApi.operations.executeTask,
      }),
  },
  tools: {
    patchSession: () =>
      mutationOptions({
        mutationKey: observeMutationKeys.tools('patch-session'),
        mutationFn: (input: PatchSessionInput) => observeApi.tools.patchSession(input),
      }),
  },
}
