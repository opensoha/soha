let activeGuard: ((event: PopStateEvent) => void) | undefined

/** Install before BrowserRouter registers its window popstate listener. */
export function installBrowserPopGuard() {
  window.addEventListener('popstate', handleBrowserPop)
}

function handleBrowserPop(event: PopStateEvent) {
  activeGuard?.(event)
}

export function registerBrowserPopGuard(guard: (event: PopStateEvent) => void) {
  activeGuard = guard
  return () => {
    if (activeGuard === guard) activeGuard = undefined
  }
}
