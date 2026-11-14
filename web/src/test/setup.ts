if (typeof document !== 'undefined' && typeof document.queryCommandSupported !== 'function') {
  Object.defineProperty(document, 'queryCommandSupported', {
    configurable: true,
    value: () => false,
  })
}

// jsdom has no layout observer; geometry tests provide their own measurements.
if (typeof document !== 'undefined' && typeof ResizeObserver === 'undefined') {
  Object.defineProperty(globalThis, 'ResizeObserver', {
    configurable: true,
    writable: true,
    value: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  })
}
