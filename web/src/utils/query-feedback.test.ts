import {
  latestSubmittedOperation,
  readableQueryData,
  writeOutcomeUnknown,
} from '@/utils/query-feedback'
import { describe, expect, it } from 'vitest'
import { ApiError } from '@/services/api-error'

describe('shared query feedback evidence', () => {
  it('keeps the latest submitted request visible even if an older request completes later', () => {
    const old = { mutation: { submittedAt: 10 }, id: 'older' }
    const latest = { mutation: { submittedAt: 20 }, id: 'latest' }
    expect(latestSubmittedOperation([latest, old])).toBe(latest)
    expect(latestSubmittedOperation([])).toBeUndefined()
  })
  it('retains cached data after a temporary read failure, including a real empty result', () => {
    const data = [{ id: 'resource-1' }]
    expect(readableQueryData({ data, error: new ApiError(503, 'unavailable') })).toBe(data)
    expect(readableQueryData({ data: [], error: new Error('offline') })).toEqual([])
  })
  it.each([401, 403])('hides protected cached content after HTTP %s', (status) => {
    expect(
      readableQueryData({ data: ['protected'], error: new ApiError(status, 'denied') }),
    ).toBeUndefined()
  })
  it.each([400, 401, 403, 404, 409, 422, 429])(
    'recognizes an explicit HTTP %s rejection',
    (status) => {
      expect(writeOutcomeUnknown(new ApiError(status, 'rejected'))).toBe(false)
    },
  )
  it('does not infer rejection from a lost response or a server failure', () => {
    expect(writeOutcomeUnknown(new ApiError(0, 'response lost'))).toBe(true)
    expect(writeOutcomeUnknown(new ApiError(503, 'unavailable'))).toBe(true)
    expect(writeOutcomeUnknown(new Error('local input validation failed'))).toBe(false)
    expect(writeOutcomeUnknown(new ApiError(503, 'read failed', { method: 'GET' }))).toBe(false)
    expect(writeOutcomeUnknown(null)).toBe(false)
  })
})
