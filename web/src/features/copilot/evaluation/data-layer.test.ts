import { describe, expect, it, vi } from 'vitest'
import { evaluationApi } from './api'
import { evaluationKeys } from './keys'
import { evaluationQueries } from './queries'
import type { EvaluationDataset, EvaluationRun } from './types'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('evaluation data layer', () => {
  it('keeps dataset list and query keys on the evaluation boundary', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    const options = evaluationQueries.datasets()
    expect(options.queryKey).toEqual(evaluationKeys.datasets())
    await options.queryFn?.({} as never)
    expect(apiMocks.get).toHaveBeenCalledWith('/ai/evaluations/datasets')
  })

  it('posts complete contract-shaped dataset and run documents', async () => {
    const dataset: EvaluationDataset = {
      schemaVersion: 'opensoha.dev/evaluation-dataset/v1',
      id: 'rag-regression',
      name: 'RAG regression',
      version: 'v1',
      samples: [{ id: 's1', input: 'What failed?' }],
      createdAt: '2026-07-14T08:00:00Z',
    }
    const run: EvaluationRun = {
      schemaVersion: 'opensoha.dev/evaluation-run/v1',
      id: 'eval-1',
      datasetId: dataset.id,
      datasetVersion: dataset.version,
      candidateRefs: { prompt: 'prompt:v2' },
      status: 'running',
      startedAt: '2026-07-14T08:00:00Z',
    }
    await evaluationApi.datasets.create(dataset)
    await evaluationApi.runs.create(run)
    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/ai/evaluations/datasets', dataset)
    expect(apiMocks.post).toHaveBeenNthCalledWith(2, '/ai/evaluations/runs', run)
  })

  it('reads results from the result-schema endpoint', async () => {
    await evaluationApi.runs.results('eval-1')
    expect(apiMocks.get).toHaveBeenCalledWith('/ai/evaluations/runs/eval-1/results')
  })
})
