import { useAuthStore } from '@/stores/auth-store'
import { restoreApplicationWorkbenchSession, restoreAuthSession } from './auth-api'
import { preparePermissionSnapshot } from './permission-snapshot-request'

const { pathname } = window.location
const basePath = import.meta.env.BASE_URL.replace(/\/$/, '')
const applicationMatch = pathname.startsWith(basePath + '/')
  ? /^\/applications\/([^/]+)\/?$/.exec(pathname.slice(basePath.length))
  : null

function readApplicationId() {
  if (!applicationMatch) return ''
  try {
    const id = decodeURIComponent(applicationMatch[1]).trim()
    return Array.from(id).length <= 200 ? id : ''
  } catch {
    return ''
  }
}
const applicationId = readApplicationId()

const preloadedModules = new Set<string>()

function preloadModules(name: string, priority: 'auto' | 'low' = 'auto') {
  const preload = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
  for (const href of preload?.content.split(' ').filter(Boolean) ?? []) {
    if (preloadedModules.has(href)) continue
    preloadedModules.add(href)
    const link = document.createElement('link')
    link.rel = 'modulepreload'
    link.crossOrigin = 'anonymous'
    link.fetchPriority = priority
    link.href = href
    document.head.append(link)
  }
}

if (applicationMatch && useAuthStore.getState().user) {
  // The regular auth guard continues to own access and joins an in-flight session refresh.
  const restore = applicationId
    ? restoreApplicationWorkbenchSession(applicationId)
    : restoreAuthSession()
  void restore.then((status) => {
    if (status === 'authenticated') preparePermissionSnapshot()
  })
}

function startDeferredApp() {
  const entry = document.querySelector<HTMLMetaElement>(
    'meta[name="soha-workbench-entry"]',
  )?.content
  if (!entry) return
  for (const link of document.querySelectorAll('link[rel="modulepreload"]')) {
    const href = link.getAttribute('href')
    if (href) preloadedModules.add(href)
  }
  // Start the cookie refresh before the rest of the SPA claims the same-origin sockets.
  setTimeout(() => {
    preloadModules('soha-workbench-entry-dependencies')
    preloadModules('soha-workbench-layout-dependencies', 'low')
    const script = document.createElement('script')
    script.type = 'module'
    script.crossOrigin = 'anonymous'
    script.src = entry
    document.head.append(script)
  }, 0)
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', startDeferredApp, { once: true })
} else {
  startDeferredApp()
}
