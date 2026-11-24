import { createHash } from 'node:crypto'
import express, { type NextFunction, type Request, type Response } from 'express'
import { Pool } from 'pg'

const parsedPort = Number.parseInt(process.env.PORT ?? '8080', 10)
const port = Number.isInteger(parsedPort) && parsedPort > 0 && parsedPort < 65536 ? parsedPort : 8080
const databaseUrl = process.env.DATABASE_URL?.trim()
const pool = databaseUrl ? new Pool({ connectionString: databaseUrl, max: 5 }) : null
const app = express()

class HttpError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
  }
}

type Identity = { id: string; displayName: string }

function decodeHeader(value: string): string {
  const decoded = Buffer.from(value, 'latin1').toString('utf8')
  return decoded.includes('\uFFFD') ? value : decoded
}

function textHeader(req: Request, name: string): string {
  const raw = req.get(name)
  return raw ? decodeHeader(raw).trim() : ''
}

function readIdentity(req: Request): Identity {
  const id = textHeader(req, 'x-soha-user-id')
  if (!id || id.length > 200 || /[\u0000-\u001f\u007f]/.test(id)) {
    throw new HttpError(401, 'missing or invalid X-Soha-User-ID')
  }
  return { id, displayName: textHeader(req, 'x-soha-user') }
}

function entryID(userID: string, key: string): string {
  return createHash('sha256').update(userID).update('\u0000').update(key).digest('hex')
}

function parseEntry(body: unknown): { key: string; value: unknown } {
  if (!body || typeof body !== 'object' || Array.isArray(body)) {
    throw new HttpError(400, 'body must be a JSON object')
  }
  const record = body as Record<string, unknown>
  if (typeof record.key !== 'string' || !/^[A-Za-z0-9._-]{1,120}$/.test(record.key) || !Object.hasOwn(record, 'value')) {
    throw new HttpError(400, 'key must match [A-Za-z0-9._-]{1,120} and value is required')
  }
  return { key: record.key, value: record.value }
}

function database(): Pool {
  if (!pool) throw new HttpError(503, 'DATABASE_URL is not configured')
  return pool
}

function asyncRoute(handler: (req: Request, res: Response) => Promise<void>) {
  return (req: Request, res: Response, next: NextFunction) => {
    void handler(req, res).catch(next)
  }
}

app.disable('x-powered-by')
app.use(express.json({ limit: '64kb' }))

app.get('/healthz', (_req, res) => {
  res.json({ status: 'ok' })
})

app.get('/', (_req, res) => {
  res.json({ service: 'soha-app-hosting-node-starter', endpoints: ['GET /healthz', 'GET /api/entries', 'POST /api/entries'] })
})

app.get(
  '/api/entries',
  asyncRoute(async (req, res) => {
    const user = readIdentity(req)
    const result = await database().query(
      "SELECT data FROM app.entries WHERE data->>'owner' = $1 ORDER BY id LIMIT 200",
      [user.id],
    )
    res.setHeader('Cache-Control', 'no-store')
    res.json({ user, entries: result.rows.map((row) => row.data) })
  }),
)

app.post(
  '/api/entries',
  asyncRoute(async (req, res) => {
    const user = readIdentity(req)
    const entry = parseEntry(req.body)
    const data = {
      key: entry.key,
      value: entry.value,
      owner: user.id,
      ownerDisplayName: user.displayName,
      updatedAt: new Date().toISOString(),
    }
    const result = await database().query(
      'INSERT INTO app.entries (id, data) VALUES ($1, $2::jsonb) ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data RETURNING data',
      [entryID(user.id, entry.key), JSON.stringify(data)],
    )
    res.setHeader('Cache-Control', 'no-store')
    res.json({ user, entry: result.rows[0].data })
  }),
)

app.use((error: unknown, _req: Request, res: Response, _next: NextFunction) => {
  if (error instanceof HttpError) {
    res.status(error.status).json({ error: error.message })
    return
  }
  if (error instanceof SyntaxError) {
    res.status(400).json({ error: 'invalid JSON body' })
    return
  }
  console.error('request failed', error)
  res.status(500).json({ error: 'internal server error' })
})

const server = app.listen(port, '0.0.0.0', () => {
  console.log(`listening on ${port}`)
})

function shutdown() {
  server.close(() => {
    void pool?.end().finally(() => process.exit(0))
  })
}

process.on('SIGTERM', shutdown)
process.on('SIGINT', shutdown)
