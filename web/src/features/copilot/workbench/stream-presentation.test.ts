import { describe, expect, it } from 'vitest'
import {
  replayArtifactsForMessage,
  thoughtChainStatus,
  toolCallSummaryText,
  toolExecutionLabel,
  toolExecutionSummary,
} from './stream-presentation'

describe('tool execution presentation', () => {
  it('names built-in actions consistently and preserves unknown tool identities', () => {
    expect(toolExecutionLabel('knowledge.search')).toBe('检索知识库')
    expect(toolExecutionLabel('agent.delegate')).toBe('专业只读核验')
    expect(toolExecutionLabel('custom.read')).toBe('custom.read')
    expect(
      toolExecutionSummary({ toolName: 'knowledge.search', summary: 'knowledge.search executed' }),
    ).toBe('')
    expect(toolExecutionSummary({ toolName: 'knowledge.search', summary: '找到维护窗口' })).toBe(
      '找到维护窗口',
    )
  })
  it('counts persisted and streamed outcomes consistently without treating unknown outcomes as success', () => {
    const statuses = [
      'completed',
      'succeeded',
      'success',
      'failed',
      'error',
      'running',
      'skipped',
      'unknown',
    ]
    expect(statuses.map(thoughtChainStatus)).toEqual([
      'success',
      'success',
      'success',
      'error',
      'error',
      'loading',
      'abort',
      'abort',
    ])
    expect(
      toolCallSummaryText(
        statuses.map((status, index) => ({
          id: String(index),
          adapterId: 'test',
          toolName: 'test.query',
          startedAt: '',
          status,
        })),
      ),
    ).toBe('8 个工具调用，3 成功，2 失败')
  })
})

describe('model failure presentation', () => {
  it('does not fabricate an analysis result from context collected before a failed model call', () => {
    for (const source of ['model-unconfigured', 'model-error', 'model-empty']) {
      expect(
        replayArtifactsForMessage({
          id: 'reply',
          sessionId: 'session',
          role: 'assistant',
          content: '模型暂不可用',
          createdAt: '',
          metadata: {
            source,
            thinkingSummary: '已收集上下文',
            sources: [{ id: 'context', kind: 'resource', title: '当前资源' }],
            toolExecutions: [
              { id: 'tool', adapterId: 'platform', toolName: 'read', status: 'success' },
            ],
          },
        }),
      ).toEqual([])
    }
  })
})

it('preserves explicit artifacts when only final model synthesis fails', () => {
  const report = { kind: 'root_cause', runId: 'run-1', summary: '已采集到的证据' }
  expect(
    replayArtifactsForMessage({
      id: 'reply',
      sessionId: 'session',
      role: 'assistant',
      content: '模型暂不可用',
      createdAt: '',
      metadata: {
        source: 'model-error',
        analysisArtifacts: [report, { kind: 'stream', runId: 'reply', summary: '上下文收集' }],
      },
    }),
  ).toEqual([report])
})

it('does not display a legacy synthetic general-analysis artifact', () => {
  expect(
    replayArtifactsForMessage({
      id: 'failed',
      sessionId: 'session',
      role: 'assistant',
      content: 'timeout',
      createdAt: '',
      metadata: {
        mode: 'general',
        source: 'agent-runtime',
        agentStatus: { status: 'callback_timeout' },
        analysisArtifacts: [{ kind: 'general', title: 'general analysis', summary: 'timeout' }],
      },
    }),
  ).toEqual([])
})
