// Router paths are internal; full-page navigation and public assets include the
// deployment prefix. Vite validates the configured prefix at startup/build.
export const appBasePath = import.meta.env.BASE_URL || '/'

export function appRouterPath(path: string, base = appBasePath) {
  const prefix = base.replace(/\/$/, '')
  if (
    prefix &&
    (path === prefix ||
      path.startsWith(prefix + '/') ||
      path.startsWith(prefix + '?') ||
      path.startsWith(prefix + '#'))
  )
    return path.slice(prefix.length) || '/'
  return path
}

export function appBrowserPath(path: string, base = appBasePath) {
  return base.replace(/\/$/, '') + (path.startsWith('/') ? path : '/' + path)
}

export function defaultBrandAsset(path: string) {
  return path === '/logo.svg' ? appBrowserPath(path) : path
}
