import { test, expect } from 'vitest'
import { createModelProxy } from './model-proxy.mjs'

test('S05/M02: paths, redirects and request budget fail closed', async () => {
  const requests = []
  const proxy = createModelProxy(
    'https://approved.example/v1',
    'synthetic-key-canary',
    1,
    async (url, options) => {
      requests.push({ url, options })
      return new Response('{}', { status: 200 })
    },
  )
  await new Promise((resolve) => proxy.server.listen(0, '127.0.0.1', resolve))
  const origin = `http://127.0.0.1:${proxy.server.address().port}`
  try {
    expect((await fetch(origin + '/execute', { method: 'POST' })).status).toBe(403)
    expect(
      (
        await fetch(origin + '/v1/chat/completions', {
          method: 'POST',
          body: '{}',
          headers: { Authorization: 'Bearer synthetic-proxy-key' },
        })
      ).status,
    ).toBe(200)
    expect(
      (
        await fetch(origin + '/v1/chat/completions', {
          method: 'POST',
          headers: { Authorization: 'Bearer synthetic-proxy-key' },
        })
      ).status,
    ).toBe(429)
    expect(requests).toHaveLength(1)
    expect(requests[0].url.href).toBe('https://approved.example/v1/chat/completions')
    expect(requests[0].options.redirect).toBe('error')
    expect(JSON.stringify(proxy.summary())).not.toContain('synthetic-key-canary')
  } finally {
    proxy.server.closeAllConnections()
    await new Promise((resolve) => proxy.server.close(resolve))
  }
})

test('M07: interrupted model stream is classified as failure without crashing the proxy', async () => {
  const proxy = createModelProxy(
    'https://approved.example/v1',
    'synthetic',
    1,
    async () =>
      new Response(
        new ReadableStream({
          start(controller) {
            controller.error(new Error('synthetic stream failure'))
          },
        }),
      ),
  )
  await new Promise((resolve) => proxy.server.listen(0, '127.0.0.1', resolve))
  try {
    await expect(
      fetch(`http://127.0.0.1:${proxy.server.address().port}/v1/chat/completions`, {
        method: 'POST',
        body: '{}',
        headers: { Authorization: 'Bearer synthetic-proxy-key' },
      }),
    ).rejects.toThrow()
    expect(proxy.summary().rejected).toBe(1)
  } finally {
    proxy.server.closeAllConnections()
    await new Promise((resolve) => proxy.server.close(resolve))
  }
})
