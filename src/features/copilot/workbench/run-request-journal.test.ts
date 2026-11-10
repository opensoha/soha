import { acknowledgeReadOnlyRunRequest } from './run-request-journal'
/** @vitest-environment jsdom */
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  forgetRunRequest,
  pendingRunRequests,
  rememberRunRequest,
  runRecoveryBatch,
  runCancellationRequested,
  pendingRunCancellations,
  requestRunCancellation,
} from './run-request-journal'

afterEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})
describe('Native request recovery journal', () => {
  it('keeps cancellation intent across retries and discovers it after switching sessions', () => {
    rememberRunRequest('owner', 'session/one', 'request/one')
    rememberRunRequest('other-owner', 'session/one', 'request/one')
    requestRunCancellation('owner', 'session/one')
    rememberRunRequest('owner', 'session/one', 'request/one')
    expect(runCancellationRequested('owner', 'session/one', 'request/one')).toBe(true)
    expect(pendingRunCancellations('owner')).toEqual([
      { sessionId: 'session/one', requestId: 'request/one' },
    ])
    expect(pendingRunCancellations('other-owner')).toEqual([])
    forgetRunRequest('owner', 'session/one', 'request/one')
    expect(pendingRunCancellations('owner')).toEqual([])
    expect(pendingRunRequests('other-owner', 'session/one')).toEqual(['request/one'])
  })
  it('survives a reload and keeps concurrent entries without request contents', () => {
    rememberRunRequest('service+user', 'session', 'one')
    rememberRunRequest('service+user', 'session', 'two')
    expect(pendingRunRequests('service+user', 'session')).toEqual(['one', 'two'])
    expect(
      [...Array(localStorage.length)].map((_, index) =>
        localStorage.getItem(localStorage.key(index)!),
      ),
    ).toEqual(['1', '1'])
    forgetRunRequest('service+user', 'session', 'one')
    expect(pendingRunRequests('service+user', 'session')).toEqual(['two'])
  })
  it('separates services, principals and sessions even with identical request IDs', () => {
    rememberRunRequest('service-A/user', 'session', 'opaque/key+id')
    rememberRunRequest('service-B/user', 'session', 'opaque/key+id')
    forgetRunRequest('service-A/user', 'session', 'opaque/key+id')
    expect(pendingRunRequests('service-B/user', 'session')).toEqual(['opaque/key+id'])
    expect(pendingRunRequests('service-A/other', 'session')).toEqual([])
    expect(pendingRunRequests('service-B/user', 'other')).toEqual([])
  })
  it('refuses to evict unresolved submissions or dispatch without durable recovery storage', () => {
    for (let index = 0; index < 65; index++) rememberRunRequest('owner', 'session', String(index))
    const ids = pendingRunRequests('owner', 'session')
    expect(ids).toHaveLength(65)
    expect(runRecoveryBatch(ids, 0)).toHaveLength(64)
    expect(new Set([...runRecoveryBatch(ids, 0), ...runRecoveryBatch(ids, 1)]).size).toBe(65)
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota')
    })
    expect(() => rememberRunRequest('owner', 'another-session', 'pending')).toThrow()
  })
})

it('imports exact legacy scope without deleting keys, deduplicates and ORs cancellation', () => {
  const old = 'soha.native-request.v1:' + encodeURIComponent('server+owner') + '/session/request'
  localStorage.setItem(old, '1')
  localStorage.setItem('soha.native-request.v1:foreign/session/request', 'cancel')
  expect(pendingRunRequests('server+owner', 'session')).toEqual(['request'])
  rememberRunRequest('server+owner', 'session', 'request')
  expect(pendingRunRequests('server+owner', 'session')).toEqual(['request'])
  localStorage.setItem(old, 'cancel')
  expect(runCancellationRequested('server+owner', 'session', 'request')).toBe(true)
  rememberRunRequest('server+owner', 'session', 'request')
  expect(runCancellationRequested('server+owner', 'session', 'request')).toBe(true)
  forgetRunRequest('server+owner', 'session', 'request')
  expect(pendingRunRequests('server+owner', 'session')).toEqual([])
  expect(localStorage.getItem(old)).toBe('cancel')
})

it('acknowledges readonly history while retaining unknown facts and mixed-client stop intent', () => {
  const old = 'soha.native-request.v1:owner/session/request'
  localStorage.setItem(old, '1')
  acknowledgeReadOnlyRunRequest('owner', 'session', 'request')
  expect(pendingRunRequests('owner', 'session')).toEqual([])
  expect(localStorage.getItem('soha.run-request.v2:owner/session/request')).toBe('readonly:unknown')
  localStorage.setItem(old, 'cancel')
  expect(pendingRunRequests('owner', 'session')).toEqual([])
  expect(runCancellationRequested('owner', 'session', 'request')).toBe(true)
  expect(pendingRunCancellations('owner')).toEqual([])
  expect(localStorage.getItem(old)).toBe('cancel')
  expect(() => rememberRunRequest('owner', 'session', 'request')).toThrow('仅供查看')
})
