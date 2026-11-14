import { describe, expect, it, vi } from 'vitest'
import { knowledgeApi } from './api'
import { knowledgeKeys } from './keys'
import { knowledgeQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('knowledge data layer', () => {
  it('uses the canonical knowledge list endpoint and key', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    const options = knowledgeQueries.bases()
    expect(options.queryKey).toEqual(knowledgeKeys.bases())
    await options.queryFn?.({} as never)
    expect(apiMocks.get).toHaveBeenCalledWith('/ai/knowledge-bases')
  })

  it('preserves the scoped search contract', async () => {
    const input = {
      knowledgeBaseIds: ['kb-1'],
      query: 'deployment error',
      topK: 8,
      filters: { sourceIds: ['source-1'] },
    }
    await knowledgeApi.search(input)
    expect(apiMocks.post).toHaveBeenCalledWith('/ai/knowledge/search', input)
  })

  it('keeps lifecycle resources nested under a knowledge base', async () => {
    apiMocks.get.mockClear()
    apiMocks.get.mockResolvedValue({ data: [] })
    await knowledgeApi.sources('kb-1')
    await knowledgeApi.documents('kb-1')
    await knowledgeApi.syncRuns('kb-1')
    await knowledgeApi.indexRevisions('kb-1')

    expect(apiMocks.get).toHaveBeenNthCalledWith(1, '/ai/knowledge-bases/kb-1/sources')
    expect(apiMocks.get).toHaveBeenNthCalledWith(2, '/ai/knowledge-bases/kb-1/documents')
    expect(apiMocks.get).toHaveBeenNthCalledWith(3, '/ai/knowledge-bases/kb-1/sync-runs')
    expect(apiMocks.get).toHaveBeenNthCalledWith(4, '/ai/knowledge-bases/kb-1/index-revisions')
  })

  it('creates and synchronizes inline sources through bounded lifecycle endpoints', async () => {
    apiMocks.post.mockClear()
    const input = {
      name: 'Runbook',
      kind: 'inline' as const,
      syncPolicy: { mode: 'manual' as const },
      config: { documents: [{ externalId: 'runbook-1', title: 'Runbook', content: 'steps' }] },
    }
    await knowledgeApi.createSource('kb-1', input)
    await knowledgeApi.syncSource('kb-1', 'source-1')

    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/ai/knowledge-bases/kb-1/sources', input)
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/ai/knowledge-bases/kb-1/sources/source-1/sync',
    )
  })
})
