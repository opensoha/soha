import type { LogEntry } from '@opensoha/contracts/gen/ts/sohaapi'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mergeLogEntries } from './model'
import { createLogEntryBuffer } from './stream-buffer'

function entry(index: number): LogEntry {
  return {
    timestamp: new Date(Date.UTC(2026, 10, 19) + Math.floor(index / 3)).toISOString(),
    message: `entry-${index}`,
    source: { domain: 'kubernetes', podName: 'api', containerName: 'main' },
    sourceMode: 'runtime',
  }
}

describe('live log entry buffer', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('bounds delay and batch size without a stale timer after an explicit flush', () => {
    const consume = vi.fn()
    const buffer = createLogEntryBuffer(consume)
    buffer.add(entry(0))
    vi.advanceTimersByTime(31)
    expect(consume).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(consume).toHaveBeenCalledOnce()
    for (let i = 1; i <= 128; i += 1) buffer.add(entry(i))
    expect(consume).toHaveBeenCalledTimes(2)
    expect(consume.mock.calls[1][0]).toHaveLength(128)
    expect(vi.getTimerCount()).toBe(0)
    buffer.add(entry(129))
    buffer.flush()
    expect(consume).toHaveBeenCalledTimes(3)
    vi.advanceTimersByTime(32)
    expect(consume).toHaveBeenCalledTimes(3)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('matches the existing 10k merge output with over 100 times fewer retained-row scans', () => {
    const initial = mergeLogEntries(
      [],
      Array.from({ length: 10000 }, (_, i) => entry(i)),
    )
    const incoming = Array.from({ length: 1024 }, (_, i) => entry(10000 + i)).reverse()
    incoming.push(...incoming.slice(0, 16), initial[0])
    const expected = mergeLogEntries(initial, incoming)
    let current = initial
    let scanned = 0
    const consume = vi.fn((batch: LogEntry[]) => {
      scanned += current.length
      current = mergeLogEntries(current, batch)
    })
    const buffer = createLogEntryBuffer(consume)
    for (const value of incoming) buffer.add(value)
    vi.advanceTimersByTime(32)
    expect(current).toEqual(expected)
    expect(current).toHaveLength(10000)
    expect(consume).toHaveBeenCalledTimes(Math.ceil(incoming.length / 128))
    expect(consume.mock.calls.every(([batch]) => batch.length <= 128)).toBe(true)
    expect(scanned).toBeLessThan((incoming.length * initial.length) / 100)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('preserves unlimited merging and the historical descending-page composition', () => {
    const firstPage = [entry(10002), entry(10001)]
    const nextPage = Array.from({ length: 10002 }, (_, i) => entry(i)).reverse()
    const ascending = mergeLogEntries([...firstPage].reverse(), nextPage, Infinity)
    const descending = ascending.reverse()
    expect(descending).toHaveLength(10003)
    expect(descending[0]).toEqual(entry(10002))
    expect(descending.at(-1)).toEqual(entry(0))
    expect(new Set(descending.map((value) => value.message)).size).toBe(descending.length)
  })

  it('discards old queued entries while accepting a later batch', () => {
    const consume = vi.fn()
    const buffer = createLogEntryBuffer(consume)
    buffer.add(entry(0))
    buffer.clear()
    buffer.flush()
    vi.advanceTimersByTime(32)
    expect(consume).not.toHaveBeenCalled()
    buffer.add(entry(1))
    vi.advanceTimersByTime(32)
    expect(consume).toHaveBeenCalledExactlyOnceWith([entry(1)])
  })
})
