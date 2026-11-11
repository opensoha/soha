import { describe, expect, it } from 'vitest'
import { companionSpeech, resolveCompanionVisualState } from './state-machine'

describe('companion state machine', () => {
  const assistant = (content: string, status: 'loading' | 'success' | 'error' = 'success') => [
    { id: 'assistant-1', role: 'assistant' as const, content, status },
  ]

  it('keeps deterministic priority across stream and pointer states', () => {
    expect(
      resolveCompanionVisualState({
        disabled: true,
        dragging: true,
        running: true,
        messages: assistant('reply'),
      }),
    ).toBe('disabled')
    expect(
      resolveCompanionVisualState({ dragging: true, running: true, messages: assistant('reply') }),
    ).toBe('dragging')
    expect(resolveCompanionVisualState({ running: true, messages: assistant('') })).toBe('thinking')
    expect(resolveCompanionVisualState({ running: true, messages: assistant('reply') })).toBe(
      'speaking',
    )
    expect(resolveCompanionVisualState({ panelOpen: true })).toBe('listening')
    expect(resolveCompanionVisualState({ hovered: true })).toBe('hover')
  })

  it('surfaces stream errors before drag and bounds bubble text', () => {
    expect(
      resolveCompanionVisualState({ dragging: true, messages: assistant('failed', 'error') }),
    ).toBe('error')
    expect(companionSpeech(assistant('x'.repeat(220)))).toHaveLength(183)
    expect(companionSpeech(assistant('', 'loading'), true)).toBe('正在思考...')
  })
})
