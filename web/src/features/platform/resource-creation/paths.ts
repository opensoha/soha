function requireClusterId(clusterId: string) {
  const normalized = clusterId.trim()
  if (!normalized) throw new Error('A cluster is required for resource creation')
  return encodeURIComponent(normalized)
}

function basePath(clusterId: string) {
  return `/clusters/${requireClusterId(clusterId)}/resource-creation`
}

export const resourceCreationPaths = {
  scopeDecision: (clusterId: string) => `${basePath(clusterId)}/scope-decision`,
  workloadSnapshot: (clusterId: string) => `${basePath(clusterId)}/workload-snapshot`,
  preflight: (clusterId: string) => `${basePath(clusterId)}/preflight`,
  execute: (clusterId: string) => `${basePath(clusterId)}/execute`,
}
