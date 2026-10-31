import { appBasePath, appRouterPath } from '@/utils/app-base'

const SERVER_RETURN_PATHS = new Set(['/api/v1/provider/proxy/callback', '/oauth2/authorize'])

function hasControlCharacter(value: string) {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index)
    if (code <= 31 || code === 127) {
      return true
    }
  }
  return false
}

function containsDecodedControlCharacter(value: string) {
  try {
    return hasControlCharacter(decodeURIComponent(value))
  } catch {
    return true
  }
}

function containsDecodedBackslash(value: string) {
  try {
    return decodeURIComponent(value).includes('\\')
  } catch {
    return true
  }
}

function urlContainsControlCharacter(url: URL) {
  if (
    containsDecodedControlCharacter(url.pathname) ||
    containsDecodedControlCharacter(url.hash.slice(1))
  ) {
    return true
  }
  for (const [key, item] of url.searchParams) {
    if (hasControlCharacter(key) || hasControlCharacter(item)) {
      return true
    }
  }
  return false
}

function urlContainsBackslash(url: URL) {
  return containsDecodedBackslash(url.pathname)
}

export function normalizeLocalReturnTo(value: string | null | undefined, base = appBasePath) {
  if (!value) {
    return null
  }
  if (hasControlCharacter(value) || value.includes('\\') || value.startsWith('//')) {
    return null
  }

  let parsed: URL
  try {
    parsed = new URL(value, window.location.origin)
  } catch {
    return null
  }

  if (parsed.origin !== window.location.origin) {
    return null
  }
  if (urlContainsControlCharacter(parsed) || urlContainsBackslash(parsed)) {
    return null
  }
  if (!value.startsWith('/') && !value.startsWith(window.location.origin)) {
    return null
  }

  const normalized = `${parsed.pathname}${parsed.search}${parsed.hash}`
  if (
    !normalized.startsWith('/') ||
    normalized.startsWith('//') ||
    hasControlCharacter(normalized) ||
    normalized.includes('\\')
  ) {
    return null
  }
  return appRouterPath(normalized, base)
}

export function shouldUseDocumentNavigation(path: string, base = appBasePath) {
  try {
    const parsed = new URL(path, window.location.origin)
    return (
      parsed.origin === window.location.origin &&
      SERVER_RETURN_PATHS.has(appRouterPath(parsed.pathname, base))
    )
  } catch {
    return false
  }
}
