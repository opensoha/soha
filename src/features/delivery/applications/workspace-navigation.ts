export const APPLICATION_WORKSPACE_LABELS = {
  delivery: '工作流',
  services: '服务',
  resources: '资源清单',
  application: '服务与构建',
  'environment-bindings': '环境配置',
  permissions: '访问权限',
} as const

export const APPLICATION_SETTINGS_NAV_ITEMS = [
  { key: 'application', label: '服务与构建', labelEn: 'Services & builds' },
  { key: 'environment-bindings', label: '环境配置', labelEn: 'Environments' },
  { key: 'permissions', label: '访问权限', labelEn: 'Access permissions' },
  { key: 'resources', label: '资源清单', labelEn: 'Resource manifests' },
]

export const APPLICATION_SETTINGS_KEYS = APPLICATION_SETTINGS_NAV_ITEMS.map((item) => item.key)

export function applicationEnvironmentSearch(search: URLSearchParams, environmentId: string) {
  const next = new URLSearchParams(search)
  for (const key of [
    'pod',
    'podCluster',
    'podNamespace',
    'tool',
    'container',
    'workload',
    'buildId',
    'releaseId',
    'workflowRunId',
    'settings',
    'launch',
    'buildSourceId',
    'bindingId',
    'templateId',
    'source',
    'name',
    'metricPod',
    'metricCluster',
    'metricNamespace',
    'metricRange',
  ])
    next.delete(key)
  next.set('applicationEnvironmentId', environmentId)
  return next
}

export function applicationWorkspacePath(
  applicationId: string,
  tab = 'services',
  environmentId?: string,
  serviceId?: string,
  serviceTab?: string,
) {
  const search = new URLSearchParams({ tab })
  if (environmentId) search.set('applicationEnvironmentId', environmentId)
  if (serviceId) search.set('serviceId', serviceId)
  if (serviceTab) search.set('serviceTab', serviceTab)
  return `/applications/${encodeURIComponent(applicationId)}?${search}`
}

export function serviceRuntimePath(
  applicationId: string,
  environmentId: string,
  workloadName: string,
  tab = 'pods',
) {
  return `/applications/${encodeURIComponent(applicationId)}/application-environments/${encodeURIComponent(environmentId)}/workloads/${encodeURIComponent(workloadName)}?${new URLSearchParams({ tab: tab === 'overview' ? 'pods' : tab })}`
}
