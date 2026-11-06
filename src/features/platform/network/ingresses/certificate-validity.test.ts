import { describe, it, expect } from 'vitest'
import { certificateValidity } from './certificate-validity'

describe('certificate time validity is separate from trust and transport', () => {
  const now = Date.parse('2026-11-02T08:00:00Z')
  const metadata = {
    name: 'web-tls',
    namespace: 'team-a',
    status: 'available' as const,
    notBefore: '2026-11-01T08:00:00Z',
    notAfter: '2027-02-01T08:00:00Z',
  }
  it('shows remaining days and the 30-day warning boundary', () => {
    expect(certificateValidity(metadata, now)).toEqual({ state: 'valid', days: 91 })
    expect(certificateValidity({ ...metadata, notAfter: '2026-12-02T08:00:00Z' }, now)).toEqual({
      state: 'expiring',
      days: 30,
    })
  })
  it('does not call an expired or future certificate valid', () => {
    expect(certificateValidity({ ...metadata, notAfter: '2026-11-02T08:00:00Z' }, now)).toEqual({
      state: 'expired',
    })
    expect(certificateValidity({ ...metadata, notBefore: '2026-11-03T08:00:00Z' }, now)).toEqual({
      state: 'future',
    })
  })
  it('does not invent dates for missing, malformed or inverted data', () => {
    for (const value of [
      undefined,
      { ...metadata, status: 'missing' as const },
      { ...metadata, notAfter: 'invalid' },
      { ...metadata, notAfter: metadata.notBefore },
    ])
      expect(certificateValidity(value, now)).toEqual({ state: 'unknown' })
  })
})
