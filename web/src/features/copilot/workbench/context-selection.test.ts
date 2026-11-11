import { describe, expect, it } from 'vitest'
import {
  contextSnapshotSummary,
  modelContextSummary,
  taskUsageSummary,
  readTextAttachment,
} from './context-selection'

function textFile(name: string, bytes: Uint8Array) {
  const file = new File([new Uint8Array(bytes).buffer], name)
  Object.defineProperty(file, 'arrayBuffer', { value: async () => bytes.buffer })
  return file
}

describe('chat context selection', () => {
  it('reads UTF-8 text and rejects binary, malformed UTF-8 and oversized files', async () => {
    const file = await readTextAttachment(
      textFile('incident.log', new TextEncoder().encode('维护窗口 03:00')),
    )
    expect(file).toMatchObject({ name: 'incident.log', content: '维护窗口 03:00' })
    expect(file.id).toBeTruthy()
    await expect(readTextAttachment(textFile('x.log', new Uint8Array([0])))).rejects.toThrow()
    await expect(readTextAttachment(textFile('x.log', new Uint8Array([255])))).rejects.toThrow()
    await expect(readTextAttachment(textFile('x.log', new Uint8Array(65_537)))).rejects.toThrow()
    await expect(readTextAttachment(textFile('x.pdf', new Uint8Array([65])))).rejects.toThrow()
  })

  it('reports evidence estimates separately from unknown model input usage', () => {
    expect(contextSnapshotSummary(undefined)).toEqual([])
    const summary = contextSnapshotSummary({
      id: 'snapshot-1',
      budgetUsage: { evidenceItems: 1, evidenceTokens: 100 },
      usageProvenance: 'estimated_characters',
      citations: [{ documentTitle: 'incident.log' }],
      truncations: ['attachment:file-1:maxEvidenceTokens'],
    })
    expect(summary.map((item) => item.value).join(' ')).toContain('估算 100 tokens')
    expect(summary.map((item) => item.value).join(' ')).toContain(
      'attachment:file-1:maxEvidenceTokens',
    )
  })
  it('keeps single-call window usage separate from cumulative task consumption', () => {
    const current = modelContextSummary({
      measurement: 'reported',
      inputTokens: 8000,
      outputTokens: 200,
      contextWindowTokens: 128000,
    })
    expect(current.label).toBe('上下文 8K / 128K')
    expect(current.percent).toBe(6.25)
    expect(
      taskUsageSummary({
        totalTokens: 50000,
        estimatedTokens: true,
        taskTokenLimit: 100000,
        steps: 8,
      })[0].value,
    ).toBe('估算 50K tokens')
    expect(modelContextSummary({ totalTokens: 50000 }).input).toBeUndefined()
    expect(modelContextSummary({ measurement: 'estimated', inputTokens: 1200 }).label).toBe(
      '上下文 约 1.2K',
    )
    expect(
      modelContextSummary({ measurement: 'reported', inputTokens: -1, contextWindowTokens: 0 })
        .percent,
    ).toBeUndefined()
    expect(modelContextSummary(undefined).label).toBe('上下文 未知')
    expect(
      contextSnapshotSummary({
        citations: [null, { documentTitle: '手册' }],
        budgetUsage: { evidenceTokens: 6000 },
      })[0].value,
    ).toBe('手册')
  })
})
