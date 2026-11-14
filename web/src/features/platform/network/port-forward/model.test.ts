import { describe, expect, it } from 'vitest'
import { validForwardPort, hasKnownPortConflict, sessionCanStop } from './model'
import type { PortForwardSession } from './types'
const session: PortForwardSession = {
  sessionId: 's',
  clusterId: 'c',
  namespace: 'demo',
  targetKind: 'Pod',
  targetName: 'http',
  localPort: 18080,
  remotePort: 80,
  status: 'active',
  createdAt: '',
}
describe('port forward form and lifecycle boundaries', () => {
  it('accepts only integral TCP port numbers', () => {
    for (const value of [1, 18080, 65535]) expect(validForwardPort(value)).toBe(true)
    for (const value of [0, -1, 65536, 1.5, NaN, null, '8080'])
      expect(validForwardPort(value)).toBe(false)
  })
  it('warns about returned active listeners without claiming global port availability', () => {
    expect(hasKnownPortConflict([session], 18080)).toBe(true)
    expect(hasKnownPortConflict([{ ...session, status: 'starting' }], 18080)).toBe(true)
    expect(hasKnownPortConflict([{ ...session, status: 'registered' }], 18080)).toBe(false)
    expect(hasKnownPortConflict([session], 18081)).toBe(false)
  })
  it('retains stop for errored sessions but not reported terminal sessions', () => {
    expect(sessionCanStop('error')).toBe(true)
    expect(sessionCanStop('active')).toBe(true)
    expect(sessionCanStop('stopped')).toBe(false)
  })
})
