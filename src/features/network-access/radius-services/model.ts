import type { NetworkRADIUSPool, NetworkRADIUSMember } from '@opensoha/contracts/gen/ts/sohaapi'
export const radiusKeys = { all: ['network-access', 'radius-pools'] as const }
export function memberFresh(member: NetworkRADIUSMember, now = Date.now()) {
  return Boolean(member.leaseUntil && Date.parse(member.leaseUntil) > now)
}
export function stateCount(member: NetworkRADIUSMember, now = Date.now()) {
  return memberFresh(member, now) &&
    member.stateCountKnown &&
    typeof member.activeStates === 'number'
    ? member.activeStates
    : '未知'
}
export function stopped(pool: NetworkRADIUSPool) {
  return pool.state === 'stopped' && pool.desiredReplicas === 0 && pool.observedReplicas === 0
}
export function canScale(pool: NetworkRADIUSPool) {
  if (pool.state !== 'ready' || typeof pool.observedReplicas !== 'number') return false
  const runningRange = Math.max(pool.desiredReplicas, pool.observedReplicas)
  if (!Number.isInteger(runningRange) || runningRange <= 0) return false
  for (let ordinal = 0; ordinal < runningRange; ordinal += 1) {
    const member = pool.members.find((m) => m.ordinal === ordinal)
    if (
      !member ||
      !memberFresh(member) ||
      !member.stateCountKnown ||
      typeof member.activeStates !== 'number'
    )
      return false
  }
  return true
}
export type RadiusCommand =
  | { kind: 'create'; input: import('@opensoha/contracts/gen/ts/sohaapi').NetworkRADIUSPoolInput }
  | {
      kind: 'configure'
      id: string
      input: import('@opensoha/contracts/gen/ts/sohaapi').NetworkRADIUSConfigurationInput
    }
  | {
      kind: 'control'
      id: string
      input: import('@opensoha/contracts/gen/ts/sohaapi').NetworkRADIUSControlInput
    }
