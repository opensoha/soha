import { describe, expect, it } from 'vitest'
import type { NetworkRADIUSMember, NetworkRADIUSPool } from '@opensoha/contracts/gen/ts/sohaapi'
import { canScale, memberFresh, stateCount, stopped } from './model'
const member = {
  ordinal: 0,
  leaseUntil: new Date(Date.now() + 60000).toISOString(),
  stateCountKnown: true,
  activeStates: 0,
  processStopped: true,
} as NetworkRADIUSMember
const pool = {
  state: 'stopped',
  desiredReplicas: 0,
  observedReplicas: 0,
  members: [member],
} as NetworkRADIUSPool
describe('RADIUS observation gates', () => {
  it('distinguishes a reported zero from absent and unknown counts', () => {
    expect(stateCount(member)).toBe(0)
    expect(stateCount({ ...member, activeStates: undefined })).toBe('未知')
    expect(stateCount({ ...member, stateCountKnown: false })).toBe('未知')
    expect(stopped(pool)).toBe(true)
    expect(stopped({ ...pool, observedReplicas: undefined })).toBe(false)
    expect(stopped({ ...pool, state: 'draining' })).toBe(false)
    expect(stopped({ ...pool, members: [{ ...member, processStopped: false }] })).toBe(true)
  })
  it('rejects expired observations and unknown states for scaling', () => {
    const expired = { ...member, leaseUntil: '2000-01-01T00:00:00Z' }
    expect(memberFresh(expired)).toBe(false)
    expect(stateCount(expired)).toBe('未知')
    const ready = { ...pool, state: 'ready' as const, desiredReplicas: 1, observedReplicas: 1 }
    expect(canScale(ready)).toBe(true)
    expect(canScale({ ...pool, state: 'unknown' })).toBe(false)
    expect(canScale({ ...ready, members: [expired] })).toBe(false)
    expect(canScale({ ...ready, members: [{ ...member, activeStates: undefined }] })).toBe(false)
  })
  it('RAD-F01 trusts reconciled workload zero without manufacturing member counts', () => {
    const unknown = {
      ...member,
      leaseUntil: undefined,
      stateCountKnown: false,
      activeStates: undefined,
    }
    const expired = { ...member, leaseUntil: '2000-01-01T00:00:00Z' }
    expect(stopped({ ...pool, members: [unknown] })).toBe(true)
    expect(stopped({ ...pool, members: [expired] })).toBe(true)
    expect(stateCount(unknown)).toBe('未知')
    expect(stateCount(expired)).toBe('未知')
  })
  it('RAD-F01 checks desired/observed ordinals while ignoring unused future slots', () => {
    const unused = {
      ...member,
      ordinal: 1,
      leaseUntil: undefined,
      stateCountKnown: false,
      activeStates: undefined,
    }
    const ready = {
      ...pool,
      state: 'ready' as const,
      desiredReplicas: 1,
      observedReplicas: 1,
      members: [member, unused],
    }
    expect(canScale(ready)).toBe(true)
    expect(canScale({ ...ready, observedReplicas: 2 })).toBe(false)
    expect(canScale({ ...ready, desiredReplicas: 2 })).toBe(false)
    expect(canScale({ ...ready, members: [unused] })).toBe(false)
    expect(canScale({ ...ready, observedReplicas: undefined })).toBe(false)
    for (const state of ['pending', 'conflict', 'unknown'] as const)
      expect(canScale({ ...ready, state })).toBe(false)
  })
})
