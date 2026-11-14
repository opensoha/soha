import { describe, expect, it } from 'vitest'
import { summarizeDeliveryGovernance } from './governance'
import type { ExecutionTask, ReleaseBundle } from '../types'

const bundle = (metadata: Record<string, unknown> = {}): ReleaseBundle => ({
  id: 'bundle-1',
  applicationId: 'app-1',
  version: '1.0.0',
  sourceType: 'build',
  status: 'completed',
  metadata,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
})
const task = (overrides: Partial<ExecutionTask>): ExecutionTask => ({
  id: 'task-1',
  releaseBundleId: 'bundle-1',
  applicationId: 'app-1',
  taskKind: 'verify',
  providerKind: 'k8s_job_runner',
  targetKind: 'quality_gate',
  status: 'completed',
  maxRetries: 1,
  attemptCount: 1,
  timeoutSeconds: 60,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  ...overrides,
})

describe('summarizeDeliveryGovernance', () => {
  it('blocks a bundle when verification fails even if bundle status completed', () => {
    const result = summarizeDeliveryGovernance(bundle(), [
      task({
        status: 'failed',
        operationState: { failureMessage: 'smoke test failed' } as ExecutionTask['operationState'],
      }),
    ])
    expect(result.decision).toBe('blocked')
    expect(result.label).toBe('禁止晋级')
    expect(result.reason).toContain('smoke test failed')
  })

  it('exposes approval, rollback, evidence and AI audit references', () => {
    const result = summarizeDeliveryGovernance(
      bundle({ approvalStatus: 'waiting', aiGatewayRunId: 'run-1', reportId: 'report-1' }),
      [task({ taskKind: 'rollback' })],
    )
    expect(result.approvalStatus).toBe('waiting')
    expect(result.rollbackTaskCount).toBe(1)
    expect(result.aiAuditRefs).toContain('run-1')
    expect(result.evidenceRefs).toContain('report-1')
  })
})
