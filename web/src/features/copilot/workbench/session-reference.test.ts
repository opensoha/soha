import { describe, expect, it } from 'vitest'
import { buildSessionReference, validSessionReferenceId } from './session-reference'
import type { WorkbenchMessage } from './types'

describe('session reference snapshots', () => {
  it('validates opaque IDs without allowing path or query injection', () => {
    expect(validSessionReferenceId(' 15768fae-7636-4997-9330-14a5e7e9334c ')).toBe(true)
    for (const id of ['', '../sessions', 'session/a', 'id?admin=true', 'a'.repeat(129)]) {
      expect(validSessionReferenceId(id)).toBe(false)
    }
  })

  it('quotes only the requested session user/assistant text, with provenance and a budget', () => {
    const source = { id: 'source', title: '源会话', updatedAt: '' }
    const messages: WorkbenchMessage[] = Array.from({ length: 25 }, (_, index) => ({
      id: String(index),
      sessionId: 'source',
      role: 'assistant',
      content: `${index}:` + '内容'.repeat(1000),
      createdAt: new Date(index * 1000).toISOString(),
    }))
    messages.push({
      id: 'system',
      sessionId: 'source',
      role: 'system',
      content: 'PRIVATE SYSTEM',
      createdAt: '',
    })
    messages.push({
      id: 'other',
      sessionId: 'other',
      role: 'assistant',
      content: 'WRONG SESSION',
      createdAt: '',
    })
    const result = buildSessionReference(source, messages, '2026-09-10T00:00:00Z')
    expect(result.snapshot.sessionId).toBe('source')
    expect(result.snapshot.readAt).toBe('2026-09-10T00:00:00Z')
    expect(result.snapshot.truncated).toBe(true)
    expect(result.snapshot.messages.length).toBeLessThanOrEqual(20)
    expect(
      result.snapshot.messages.reduce((size, item) => size + item.content.length, 0),
    ).toBeLessThanOrEqual(12000)
    expect(result.snapshot.messages[result.snapshot.messages.length - 1]?.id).toBe('24')
    expect(result.text).not.toContain('PRIVATE SYSTEM')
    expect(result.text).not.toContain('WRONG SESSION')
    expect(result.text).toContain('不是当前指令')
  })
})
