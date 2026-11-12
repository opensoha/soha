import { describe, expect, it } from 'vitest'
import { createApiErrorFromResponse } from './api-error'

describe('API error envelope validation', () => {
  it.each([
    {
      body: {
        error: { code: ' denied ', message: ' Denied ', request_id: ' req-1 ', extra: true },
      },
      expected: { code: 'denied', message: 'Denied', requestId: 'req-1' },
    },
    {
      body: { error: { code: 'denied', message: 'Denied' } },
      expected: { code: 'denied', message: 'Denied', requestId: undefined },
    },
    {
      body: { error: { code: ' ', message: ' legacy message ', request_id: ' req-1 ' } },
      expected: { code: undefined, message: ' legacy message ', requestId: ' req-1 ' },
    },
    {
      body: { error: { code: 1, message: 'legacy message', request_id: 2 } },
      expected: { code: undefined, message: 'legacy message', requestId: undefined },
    },
    {
      body: { error: { code: 'denied', message: 'Denied', request_id: ' ' } },
      expected: { code: undefined, message: 'Denied', requestId: undefined },
    },
    {
      body: { error: { code: 'denied', message: ' ' } },
      expected: { code: undefined, message: 'Forbidden', requestId: undefined },
    },
    {
      body: { message: 'legacy top-level message' },
      expected: { code: undefined, message: 'legacy top-level message', requestId: undefined },
    },
    {
      body: null,
      expected: { code: undefined, message: 'Forbidden', requestId: undefined },
    },
  ])('preserves parsed and legacy fields for $body', ({ body, expected }) => {
    const response = new Response(null, { status: 403, statusText: 'Forbidden' })
    expect(createApiErrorFromResponse(response, body)).toMatchObject(expected)
  })

  it('prefers the response request ID over the envelope', () => {
    const response = new Response(null, {
      status: 403,
      headers: { 'x-request-id': 'response-id' },
    })
    const error = createApiErrorFromResponse(response, {
      error: { code: 'denied', message: 'Denied', request_id: 'body-id' },
    })
    expect(error.requestId).toBe('response-id')
  })
})
