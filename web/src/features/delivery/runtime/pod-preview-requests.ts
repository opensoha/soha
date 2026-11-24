export function createPodPreviewRequestLimit(limit: number) {
  let inFlight = 0
  const waiting = new Set<() => void>()

  return function runPreview<T>(signal: AbortSignal, read: () => Promise<T>) {
    return new Promise<T>((resolve, reject) => {
      signal.throwIfAborted()

      function cancel() {
        waiting.delete(start)
        reject(signal.reason)
      }

      function start() {
        waiting.delete(start)
        signal.removeEventListener('abort', cancel)
        inFlight += 1
        void Promise.resolve()
          .then(() => {
            signal.throwIfAborted()
            return read()
          })
          .then(resolve, reject)
          .finally(() => {
            inFlight -= 1
            waiting.values().next().value?.()
          })
      }

      // Visibility can change while the previous batch is still settling.
      if (inFlight < limit) {
        start()
      } else {
        waiting.add(start)
        signal.addEventListener('abort', cancel, { once: true })
      }
    })
  }
}
