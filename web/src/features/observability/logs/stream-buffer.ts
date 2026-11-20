import type { LogEntry } from '@opensoha/contracts/gen/ts/sohaapi'

export function createLogEntryBuffer(consume: (entries: LogEntry[]) => void) {
  let entries: LogEntry[] = []
  let timer: ReturnType<typeof setTimeout> | undefined

  const clear = () => {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
    entries = []
  }
  const flush = () => {
    const batch = entries
    clear()
    if (batch.length) consume(batch)
  }

  return {
    add: (entry: LogEntry) => {
      entries.push(entry)
      if (entries.length >= 128) flush()
      else if (timer === undefined) timer = setTimeout(flush, 32)
    },
    clear,
    flush,
  }
}
