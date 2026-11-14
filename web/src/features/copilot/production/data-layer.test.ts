import { beforeEach, describe, expect, it, vi } from 'vitest'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

import { environmentsApi } from '../environments/api'
import { evaluationLifecycleApi } from '../evaluation-lifecycle/api'
import { knowledgeProductionApi } from '../knowledge-production/api'
import { memoryApi } from '../memory/api'
import { aiProductionOperationsApi } from '../production-operations/api'
import { providerFleetApi } from '../provider-fleet/api'

describe('Plan 19 AI production data layers', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses the versioned connector and ingestion action paths', async () => {
    await knowledgeProductionApi.connectors.list()
    await knowledgeProductionApi.connectors.validate('git/one')
    await knowledgeProductionApi.jobs.start({ knowledgeBaseId: 'kb/one', sourceId: 'source-1' })
    await knowledgeProductionApi.jobs.cancel('job/one')
    expect(apiMocks.get).toHaveBeenCalledWith('/ai/knowledge/connectors')
    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/ai/knowledge/connectors/git%2Fone/validate')
    expect(apiMocks.post).toHaveBeenNthCalledWith(2, '/ai/knowledge-bases/kb%2Fone/sync-jobs', {
      sourceId: 'source-1',
    })
    expect(apiMocks.post).toHaveBeenNthCalledWith(3, '/ai/knowledge/sync-jobs/job%2Fone/cancel')
  })

  it('keeps fleet, environment and evaluation commands on action endpoints', async () => {
    await providerFleetApi.rollouts.action({ id: 'rollout/1', action: 'rollback' })
    await environmentsApi.leases.release('lease/1')
    await evaluationLifecycleApi.execute({ runId: 'run/1', executorProfileId: 'profile-1' })
    await evaluationLifecycleApi.evaluateGate({
      policyId: 'policy-1',
      baselineRunId: 'a',
      candidateRunId: 'b',
    })
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      1,
      '/ai/agent-providers/rollouts/rollout%2F1/rollback',
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(2, '/ai/environments/leases/lease%2F1/release')
    expect(apiMocks.post).toHaveBeenNthCalledWith(3, '/ai/evaluations/runs/run%2F1/execute', {
      executorProfileId: 'profile-1',
    })
    expect(apiMocks.post).toHaveBeenNthCalledWith(4, '/ai/evaluations/gates/evaluate', {
      policyId: 'policy-1',
      baselineRunId: 'a',
      candidateRunId: 'b',
    })
  })

  it('uses bounded memory and operations endpoints', async () => {
    await memoryApi.records.delete('memory/1')
    await aiProductionOperationsApi.start({ kind: 'drill', targetRef: 'kb-1', runbookId: 'rb-1' })
    expect(apiMocks.delete).toHaveBeenCalledWith('/ai/memory/memory%2F1')
    expect(apiMocks.post).toHaveBeenCalledWith('/ai/operations', {
      kind: 'drill',
      targetRef: 'kb-1',
      runbookId: 'rb-1',
    })
  })
})
