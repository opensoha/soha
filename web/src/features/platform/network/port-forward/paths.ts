import type { ScopeKey } from '@/types'
import { requireNetworkClusterId } from '../shared/scope'

export function buildPortForwardListPath(scope: ScopeKey) {
  return `/clusters/${requireNetworkClusterId(scope)}/network/port-forwards`
}

export function buildPortForwardItemPath(scope: ScopeKey, sessionId: string) {
  const normalized = sessionId.trim()
  if (!normalized) throw new Error('A port forward session is required')
  return `${buildPortForwardListPath(scope)}/${encodeURIComponent(normalized)}`
}
