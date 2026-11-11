import { describe, expect, it, vi } from 'vitest'
import { contextApi } from './api'

const apiMocks = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('context inspector data layer', () => {
  it('posts the bounded context request to the inspector endpoint', async () => {
    const input = {
      task: { mode: 'analysis', goal: 'Explain the failed release' },
      knowledge: { enabled: true, knowledgeBaseIds: ['kb-1'], query: 'failed release', topK: 5 },
      budgets: { maxInputTokens: 16000, maxEvidenceTokens: 6000, maxSteps: 8 },
    }
    await contextApi.inspect(input)
    expect(apiMocks.post).toHaveBeenCalledWith('/ai/context/inspect', input)
  })
})
