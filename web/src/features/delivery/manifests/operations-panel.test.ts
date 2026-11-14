import { describe, expect, it } from 'vitest'
import { parseBindingOverlay } from './operations-panel'

describe('parseBindingOverlay', () => {
  it('accepts only a string-to-string JSON object', () => {
    expect(parseBindingOverlay('')).toEqual({})
    expect(parseBindingOverlay('{"replicas":"3"}')).toEqual({ replicas: '3' })
    expect(() => parseBindingOverlay('["replicas"]')).toThrow('键值均为字符串')
    expect(() => parseBindingOverlay('{"replicas":3}')).toThrow('键值均为字符串')
    expect(() => parseBindingOverlay('{')).toThrow('有效的 JSON 对象')
  })
})
