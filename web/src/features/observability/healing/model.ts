import type { ReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import { toText } from '../shared/json'
import type { HealingPolicyFormValues, HealingPolicyPayload } from './types'

export function buildHealingPolicyPayload(
  values: HealingPolicyFormValues,
  definition: ReleaseDagDefinition,
): HealingPolicyPayload {
  return {
    id: typeof values.id === 'string' ? values.id : undefined,
    name: toText(values.name),
    triggerMode: toText(values.triggerMode),
    workflowTemplateId: toText(values.workflowTemplateId),
    approvalPolicyRef: toText(values.approvalPolicyRef),
    cooldownSeconds: Number(values.cooldownSeconds ?? 0),
    concurrencyKey: toText(values.concurrencyKey),
    safetyWindowSeconds: Number(values.safetyWindowSeconds ?? 0),
    definition,
    enabled: Boolean(values.enabled),
  }
}
