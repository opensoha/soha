import { expect, it, vi } from 'vitest'
import { createPodPreviewRequestLimit } from './pod-preview-requests'

it('keeps the slot until the active request settles and drops canceled waiters', async () => {
  const run = createPodPreviewRequestLimit(1)
  const active = new AbortController()
  const queued = new AbortController()
  let finish!: () => void
  const transport = new Promise<void>((resolve) => {
    finish = resolve
  })
  const read = vi.fn(() => transport)
  const first = run(active.signal, read)
  const discarded = vi.fn(async () => undefined)
  const canceled = run(queued.signal, discarded)
  const next = vi.fn(async () => 'ready')
  const last = run(new AbortController().signal, next)

  await Promise.resolve()
  expect(read).toHaveBeenCalledOnce()
  active.abort()
  queued.abort()
  await expect(canceled).rejects.toMatchObject({ name: 'AbortError' })
  expect(discarded).not.toHaveBeenCalled()
  expect(next).not.toHaveBeenCalled()

  finish()
  await first.catch(() => undefined)
  await expect(last).resolves.toBe('ready')
  expect(next).toHaveBeenCalledOnce()
})
