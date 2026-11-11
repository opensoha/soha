import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import type { ScopeKey } from '@/types'
import { normalizeConfigurationScope, requireConfigurationClusterId } from './scope'
import type { ConfigurationKind } from './types'

function normalizeName(name: string) {
  const normalized = name.trim()
  if (!normalized) throw new Error('A configuration resource name is required')
  return normalized
}

function configurationPath(kind: ConfigurationKind, scope: ScopeKey, suffix = '') {
  const normalizedScope = normalizeConfigurationScope(scope)
  return buildClusterScopedPath(
    requireConfigurationClusterId(normalizedScope),
    `configuration/${kind}${suffix}`,
    normalizedScope.namespace,
  )
}

export function buildConfigurationListPath(kind: ConfigurationKind, scope: ScopeKey) {
  return configurationPath(kind, scope)
}

export function buildConfigurationItemPath(kind: ConfigurationKind, scope: ScopeKey, name: string) {
  return configurationPath(kind, scope, `/${encodeURIComponent(normalizeName(name))}`)
}

export function buildConfigurationDetailPath(
  kind: ConfigurationKind,
  scope: ScopeKey,
  name: string,
) {
  return configurationPath(kind, scope, `/${encodeURIComponent(normalizeName(name))}/detail`)
}

export function buildConfigurationDataPath(kind: ConfigurationKind, scope: ScopeKey, name: string) {
  return configurationPath(kind, scope, `/${encodeURIComponent(normalizeName(name))}/data`)
}

export function buildSecretCertificatePath(scope: ScopeKey, name: string) {
  return configurationPath(
    'secrets',
    scope,
    `/${encodeURIComponent(normalizeName(name))}/certificate`,
  )
}

export function buildConfigurationReferencesPath(
  kind: ConfigurationKind,
  scope: ScopeKey,
  name: string,
) {
  return configurationPath(kind, scope, `/${encodeURIComponent(normalizeName(name))}/references`)
}

export function buildConfigurationYAMLPath(kind: ConfigurationKind, scope: ScopeKey, name: string) {
  return configurationPath(kind, scope, `/${encodeURIComponent(normalizeName(name))}/yaml`)
}
