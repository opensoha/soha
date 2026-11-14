import { describe, expect, it } from 'vitest'
import { createDefaultReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import { buildHealingPolicyPayload } from './model'

describe('healing model', () => {
  it('builds a typed policy payload with its DAG definition', () => {
    const definition = createDefaultReleaseDagDefinition()
    expect(
      buildHealingPolicyPayload(
        {
          name: 'Restart checkout',
          triggerMode: 'approval_then_auto',
          workflowTemplateId: 'workflow-restart',
          approvalPolicyRef: 'approval-prod',
          cooldownSeconds: 300,
          concurrencyKey: 'checkout',
          safetyWindowSeconds: 600,
          enabled: true,
        },
        definition,
      ),
    ).toMatchObject({ name: 'Restart checkout', definition, enabled: true })
  })
})
