import { afterEach, describe, expect, it, vi } from 'vitest'
import { createUUID } from './uuid'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('createUUID', () => {
  it('uses the native implementation when available', () => {
    const randomUUID = vi.fn(() => '00000000-0000-4000-8000-000000000000')
    vi.stubGlobal('crypto', { randomUUID })

    expect(createUUID()).toBe('00000000-0000-4000-8000-000000000000')
    expect(randomUUID).toHaveBeenCalledOnce()
  })

  it('generates an RFC 4122 version 4 UUID when randomUUID is unavailable', () => {
    const getRandomValues = vi.fn((bytes: Uint8Array) => {
      bytes.set([0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15])
      return bytes
    })
    vi.stubGlobal('crypto', { getRandomValues })

    expect(createUUID()).toBe('00010203-0405-4607-8809-0a0b0c0d0e0f')
    expect(getRandomValues).toHaveBeenCalledOnce()
  })

  it('fails instead of using a weak random source', () => {
    vi.stubGlobal('crypto', undefined)

    expect(() => createUUID()).toThrow('Secure random number generation is unavailable')
  })
})
