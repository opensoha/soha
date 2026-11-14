import { describe, expect, it } from 'vitest'
import { parseStreamMessage } from './stream-message'

describe('parseStreamMessage', () => {
  it('accepts object messages and rejects malformed or scalar payloads', () => {
    expect(parseStreamMessage<{ type: string }>(`{"type":"status"}`)).toEqual({ type: 'status' })
    expect(parseStreamMessage('{')).toBeNull()
    expect(parseStreamMessage('"status"')).toBeNull()
    expect(parseStreamMessage('[]')).toBeNull()
  })
})
