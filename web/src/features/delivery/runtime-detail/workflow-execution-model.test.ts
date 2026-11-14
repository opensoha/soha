import { describe, expect, it } from 'vitest'
import type { WorkflowRun } from '../types'
import {
  workflowCanReview,
  workflowExecutionIsLive,
  workflowExecutionState,
  workflowExecutionSummary,
} from './workflow-execution-model'

const run: WorkflowRun = {
  id: 'workflow-1',
  applicationId: 'app-1',
  workflowName: '发布验证',
  status: 'running',
  createdAt: '2026-10-12T01:00:00Z',
  updatedAt: '2026-10-12T01:05:00Z',
  steps: [],
}

describe('workflow execution evidence', () => {
  it('counts only confirmed success and selects the active node without inferring dependencies', () => {
    const summary = workflowExecutionSummary({
      ...run,
      nodeRuns: [
        { nodeId: 'build', name: '构建', type: 'build', status: 'completed' },
        { nodeId: 'skip', name: '跳过', type: 'check', status: 'skipped' },
        { nodeId: 'failed', name: '检查', type: 'check', status: 'failed' },
        { nodeId: 'cancel', name: '取消', type: 'deploy', status: 'cancelled' },
        { nodeId: 'deploy', name: '部署', type: 'deploy', status: 'running' },
      ],
    })
    expect(summary.successful).toBe(1)
    expect(summary.failed).toBe(1)
    expect(summary.current?.key).toBe('deploy')
    expect(summary.stages.map((stage) => stage.key)).toEqual([
      'build',
      'skip',
      'failed',
      'cancel',
      'deploy',
    ])
  })

  it('keeps legacy step evidence and treats missing or unfamiliar states as unknown', () => {
    const summary = workflowExecutionSummary({
      ...run,
      steps: [{ name: '校验', status: 'external_pending' }],
    })
    expect(summary.successful).toBe(0)
    expect(summary.current?.name).toBe('校验')
    expect(summary.current?.state.label).toContain('external_pending')
    expect(workflowExecutionState('').tone).toBe('idle')
    expect(workflowExecutionState(' WAITING_APPROVAL ').label).toBe('等待审批')
  })

  it.each(['completed', 'failed', 'rejected', 'canceled', 'cancelled', 'external_unknown'])(
    'stops polling at terminal or unknown %s',
    (status) => {
      expect(workflowExecutionIsLive(status)).toBe(false)
    },
  )

  it('requires the existing exact approval state and keeps batch decisions outside this page', () => {
    expect(workflowCanReview({ ...run, status: 'waiting_approval' })).toBe(true)
    expect(
      workflowCanReview({
        ...run,
        status: 'waiting_approval',
        scope: 'delivery_batch',
      }),
    ).toBe(false)
    expect(workflowCanReview({ ...run, status: 'pending_approval' })).toBe(false)
    expect(workflowCanReview(run)).toBe(false)
  })
})
