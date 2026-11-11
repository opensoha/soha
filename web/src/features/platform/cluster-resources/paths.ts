import { requireClusterId, requireResourceName } from './scope'
import type { ClusterScope } from './types'

function segment(value: string) {
  return encodeURIComponent(value)
}

export function buildNodesPath(scope: ClusterScope) {
  return `/clusters/${segment(requireClusterId(scope))}/infrastructure/nodes`
}

export function buildNodePath(scope: ClusterScope, name: string) {
  return `${buildNodesPath(scope)}/${segment(requireResourceName(name, 'node'))}`
}

export function buildNodeDetailPath(scope: ClusterScope, name: string) {
  return `${buildNodePath(scope, name)}/detail`
}

export function buildNodeYAMLPath(scope: ClusterScope, name: string) {
  return `${buildNodePath(scope, name)}/yaml`
}

export function buildNodeSchedulabilityPath(scope: ClusterScope, name: string) {
  return `${buildNodePath(scope, name)}/schedulability`
}

export function buildNodeDrainPath(scope: ClusterScope, name: string) {
  return `${buildNodePath(scope, name)}/drain`
}

export function buildNamespacesPath(scope: ClusterScope) {
  return `/clusters/${segment(requireClusterId(scope))}/namespaces`
}

export function buildNamespacePath(scope: ClusterScope, name: string) {
  return `${buildNamespacesPath(scope)}/${segment(requireResourceName(name, 'namespace'))}`
}
