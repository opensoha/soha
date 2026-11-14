import type { TemplateUsageRuntimeItem } from '@/types'

function encoded(value?: string) {
  const text = String(value ?? '').trim()
  return text ? encodeURIComponent(text) : ''
}

function withParams(path: string, params: Record<string, string | undefined>) {
  const query = Object.entries(params)
    .filter(([, value]) => String(value ?? '').trim())
    .map(([key, value]) => `${encodeURIComponent(key)}=${encoded(value)}`)
    .join('&')
  return query ? `${path}?${query}` : path
}

export function runtimeEvidencePath(item: TemplateUsageRuntimeItem) {
  if (item.kind === 'execution_task' || item.executionTaskId) {
    return withParams(`/delivery/execution-tasks/${encoded(item.executionTaskId || item.id)}`, {
      highlight: item.executionTaskId || item.id,
    })
  }
  if (item.kind === 'release_bundle' || item.releaseBundleId) {
    return withParams(`/delivery/release-bundles/${encoded(item.releaseBundleId || item.id)}`, {
      highlight: item.releaseBundleId || item.id,
    })
  }
  if (item.kind === 'workflow') {
    return withParams(`/workflows/${encoded(item.id)}`, {
      highlight: item.id,
    })
  }
  if (item.kind === 'release') {
    return withParams(`/releases/${encoded(item.id)}`, {
      highlight: item.id,
    })
  }
  if (item.kind === 'build') {
    return withParams(`/builds/${encoded(item.id)}`, {
      highlight: item.id,
    })
  }
  if (item.applicationEnvironmentId && item.applicationId) {
    return withParams(`/applications/${encoded(item.applicationId)}`, {
      tab: 'services',
      applicationEnvironmentId: item.applicationEnvironmentId,
    })
  }
  if (item.applicationId) {
    return `/applications/${encoded(item.applicationId)}`
  }
  return ''
}
