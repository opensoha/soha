import { createServer } from 'node:http'

// No raw prompts/responses in this proxy's report. Only synthetic test pages may use it.
export function createModelProxy(
  endpoint,
  apiKey,
  maxCalls,
  upstreamFetch = fetch,
  capability = 'synthetic-proxy-key',
) {
  const target = new URL(endpoint)
  let calls = 0
  let rejected = 0
  const server = createServer(async (request, response) => {
    if (
      request.method !== 'POST' ||
      request.headers.authorization !== `Bearer ${capability}` ||
      !['/v1/chat/completions', '/chat/completions'].includes(request.url)
    ) {
      rejected++
      response.writeHead(403).end()
      return
    }
    if (++calls > maxCalls) {
      rejected++
      response.writeHead(429).end('Request budget exhausted')
      return
    }
    try {
      const chunks = []
      let bytes = 0
      for await (const chunk of request) {
        bytes += chunk.length
        if (bytes > 20 * 1024 * 1024) throw new Error('Request too large')
        chunks.push(chunk)
      }
      const url = new URL(target.href.replace(/\/$/, '') + '/chat/completions')
      const result = await upstreamFetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${apiKey}` },
        body: Buffer.concat(chunks),
        redirect: 'error',
        signal: AbortSignal.timeout(60_000),
      })
      response.writeHead(result.status, {
        'Content-Type': result.headers.get('content-type') || 'application/json',
      })
      // Streaming response remains bounded by the same timeout; no buffering report content.
      for await (const chunk of result.body ?? []) response.write(chunk)
      response.end()
    } catch {
      rejected++
      if (response.headersSent) response.destroy()
      else response.writeHead(502).end('Model request rejected or unavailable')
    }
  })
  return {
    server,
    summary: () => ({
      calls: Math.min(calls, maxCalls),
      rejected,
      maxCalls,
      cost: 'UNKNOWN',
      host: target.hostname,
    }),
  }
}
