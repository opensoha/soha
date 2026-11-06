import { formatBytesAsG, formatCpu } from '@/features/platform/node-resource-utils'
import type { PodRelatedResource, PodVolume, ResourceQuantity, WorkloadCondition } from '@/types'

export function resolveWorkloadNamespace(
  selectedNamespace: string | null,
  searchNamespace: string | null,
  rowNamespace?: string,
) {
  if (searchNamespace) return searchNamespace
  if (selectedNamespace && selectedNamespace !== '') return selectedNamespace
  return rowNamespace ?? ''
}

function buildWorkloadScopeQuery(namespace: string | null, clusterId: string | null) {
  const params = new URLSearchParams()
  if (clusterId) params.set('clusterId', clusterId)
  if (namespace) params.set('namespace', namespace)
  return params.toString()
}

export function buildWorkloadListPagePath(
  resource: string,
  namespace: string | null,
  clusterId: string | null,
) {
  const query = buildWorkloadScopeQuery(namespace, clusterId)
  const path = `/workloads/${resource}`
  return query ? `${path}?${query}` : path
}

export function buildWorkloadDetailPath(
  resource: string,
  name: string,
  selectedNamespace: string | null,
  rowNamespace: string,
  clusterId: string | null,
) {
  const resolvedNamespace = resolveWorkloadNamespace(selectedNamespace, null, rowNamespace)
  const query = buildWorkloadScopeQuery(resolvedNamespace, clusterId)
  const path = `/workloads/${resource}/${encodeURIComponent(name)}`
  return query ? `${path}?${query}` : path
}

export function buildNamespacedDetailQuery(
  namespace?: string | null,
  clusterId: string | null = null,
) {
  const query = buildWorkloadScopeQuery(namespace || null, clusterId)
  return query ? `?${query}` : ''
}

export function formatContainerStateLabel(value?: string) {
  if (!value) return '-'
  const [phase, reason] = value.split(':', 2)
  if (!reason) return phase
  return `${phase} · ${reason}`
}

export function formatVolumeTypeLabel(type: string) {
  const labelMap: Record<string, string> = {
    ConfigMap: 'configMap',
    Secret: 'secret',
    PersistentVolumeClaim: 'pvc',
    Projected: 'projected',
    EmptyDir: 'emptyDir',
    HostPath: 'hostPath',
    DownwardAPI: 'downwardAPI',
    ServiceAccountToken: 'serviceAccountToken',
    CSI: 'csi',
    NFS: 'nfs',
    Other: 'other',
  }
  return labelMap[type] ?? type
}

export function localizeRelatedResourceKind(kind: string, localeCode: 'zh_CN' | 'en_US') {
  const labelMap: Record<string, { zh_CN: string; en_US: string }> = {
    ConfigMap: { zh_CN: 'ConfigMap', en_US: 'ConfigMap' },
    Secret: { zh_CN: 'Secret', en_US: 'Secret' },
    Service: { zh_CN: 'Service', en_US: 'Service' },
    Ingress: { zh_CN: 'Ingress', en_US: 'Ingress' },
    HTTPRoute: { zh_CN: 'HTTPRoute', en_US: 'HTTPRoute' },
    GRPCRoute: { zh_CN: 'GRPCRoute', en_US: 'GRPCRoute' },
    Deployment: { zh_CN: 'Deployment', en_US: 'Deployment' },
    ReplicaSet: { zh_CN: 'ReplicaSet', en_US: 'ReplicaSet' },
    ReplicationController: { zh_CN: 'ReplicationController', en_US: 'ReplicationController' },
    ServiceAccount: { zh_CN: 'ServiceAccount', en_US: 'ServiceAccount' },
    PersistentVolumeClaim: { zh_CN: 'PVC', en_US: 'PVC' },
    StatefulSet: { zh_CN: 'StatefulSet', en_US: 'StatefulSet' },
    DaemonSet: { zh_CN: 'DaemonSet', en_US: 'DaemonSet' },
    Job: { zh_CN: 'Job', en_US: 'Job' },
    CronJob: { zh_CN: 'CronJob', en_US: 'CronJob' },
  }
  return labelMap[kind]?.[localeCode] ?? kind
}

export function localizeRelatedRelation(relation: string, localeCode: 'zh_CN' | 'en_US') {
  const labelMap: Record<string, { zh_CN: string; en_US: string }> = {
    owner: { zh_CN: '所有者', en_US: 'Owner' },
    'service-account': { zh_CN: '服务账号', en_US: 'Service account' },
    config: { zh_CN: '配置引用', en_US: 'Config reference' },
    secret: { zh_CN: '密钥引用', en_US: 'Secret reference' },
    volume: { zh_CN: '卷引用', en_US: 'Volume reference' },
    'selected-by-service': { zh_CN: 'Service 选择器命中', en_US: 'Selected by service' },
    'routes-service': { zh_CN: 'Ingress 后端指向', en_US: 'Ingress backend' },
    'selector-match': { zh_CN: 'Selector 命中', en_US: 'Selector match' },
    'managed-by-replicaset': { zh_CN: 'ReplicaSet 所属', en_US: 'Managed by ReplicaSet' },
    'name-prefix': { zh_CN: '名称前缀匹配', en_US: 'Name prefix match' },
    'generated-name': { zh_CN: '生成名匹配', en_US: 'Generated name match' },
  }
  return labelMap[relation]?.[localeCode] ?? relation
}

export function buildRelatedResourcePath(
  resource: PodRelatedResource,
  selectedNamespace: string | null,
  clusterId: string | null = null,
) {
  const effectiveNamespace = resource.namespace || selectedNamespace || ''
  switch (resource.kind) {
    case 'Service':
      return `/network/services/${encodeURIComponent(resource.name)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
    case 'Ingress':
      return `/network/ingresses/${encodeURIComponent(resource.name)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
    case 'HTTPRoute':
      return `/network/gateway-api/httproutes/${encodeURIComponent(resource.name)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
    case 'GRPCRoute':
      return `/network/gateway-api/grpcroutes/${encodeURIComponent(resource.name)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
    case 'ConfigMap':
      return `/configuration/configmaps/${encodeURIComponent(resource.name)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
    case 'Secret':
      return `/configuration/secrets/${encodeURIComponent(resource.name)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
    case 'Deployment':
      return buildWorkloadDetailPath(
        'deployments',
        resource.name,
        selectedNamespace,
        effectiveNamespace,
        clusterId,
      )
    case 'ReplicaSet':
      return buildWorkloadDetailPath(
        'replicasets',
        resource.name,
        selectedNamespace,
        effectiveNamespace,
        clusterId,
      )
    case 'ReplicationController':
      return buildWorkloadDetailPath(
        'replicationcontrollers',
        resource.name,
        selectedNamespace,
        effectiveNamespace,
        clusterId,
      )
    case 'StatefulSet':
      return buildWorkloadDetailPath(
        'statefulsets',
        resource.name,
        selectedNamespace,
        effectiveNamespace,
        clusterId,
      )
    case 'DaemonSet':
      return buildWorkloadDetailPath(
        'daemonsets',
        resource.name,
        selectedNamespace,
        effectiveNamespace,
        clusterId,
      )
    case 'Job':
      return buildWorkloadDetailPath(
        'jobs',
        resource.name,
        selectedNamespace,
        effectiveNamespace,
        clusterId,
      )
    case 'CronJob':
      return buildWorkloadDetailPath(
        'cronjobs',
        resource.name,
        selectedNamespace,
        effectiveNamespace,
        clusterId,
      )
    case 'PersistentVolumeClaim':
      return `/storage/persistentvolumeclaims/${encodeURIComponent(resource.name)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
    case 'ServiceAccount':
      return `/platform-access-control/serviceaccounts/${encodeURIComponent(resource.name)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
    default:
      return null
  }
}

export function buildVolumeDetailPath(
  volume: PodVolume,
  selectedNamespace: string | null,
  clusterId: string | null = null,
) {
  const effectiveNamespace = selectedNamespace || ''
  switch (volume.type) {
    case 'ConfigMap':
      return volume.sourceName
        ? `/configuration/configmaps/${encodeURIComponent(volume.sourceName)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
        : null
    case 'Secret':
      return volume.sourceName
        ? `/configuration/secrets/${encodeURIComponent(volume.sourceName)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
        : null
    case 'PersistentVolumeClaim':
      return volume.sourceName
        ? `/storage/persistentvolumeclaims/${encodeURIComponent(volume.sourceName)}${buildNamespacedDetailQuery(effectiveNamespace, clusterId)}`
        : null
    default:
      return null
  }
}

export function normalizeSearchKeyword(value: string) {
  return value.trim().toLowerCase()
}

export function includesSearch(values: Array<string | undefined | null>, keyword: string) {
  if (!keyword) return true
  return values.some((value) => (value ?? '').toLowerCase().includes(keyword))
}

export function formatRefreshTimestamp(value: number, localeCode: 'zh_CN' | 'en_US') {
  if (!value) {
    return localeCode === 'zh_CN' ? '尚未刷新' : 'Not refreshed yet'
  }
  return new Intl.DateTimeFormat(localeCode === 'zh_CN' ? 'zh-CN' : 'en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(value)
}

export interface WorkloadOverviewEvent {
  name: string
  namespace?: string
  type: string
  reason: string
  involvedKind?: string
  involvedName?: string
  message: string
  count: number
  ageSeconds: number
}

export interface ApplicationEnvironment {
  id: string
  applicationId: string
  environmentId: string
  environmentKey?: string
  workflowTemplate?: {
    id: string
    name: string
    category?: string
  }
  targets?: Array<{
    id: string
    clusterId: string
    namespace: string
    workloadKind: string
    workloadName: string
    containerName?: string
    enabled: boolean
  }>
}

export interface ApplicationSummary {
  id: string
  name: string
  businessLineId?: string
}

export interface BuildRecord {
  id: string
  applicationId: string
  status: string
  createdAt: string
}

export interface WorkflowRecord {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  updatedAt: string
}

export interface ReleaseRecord {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  createdAt: string
}

export function targetMatchesWorkload(
  target: NonNullable<ApplicationEnvironment['targets']>[number] | undefined,
  clusterId: string,
  namespace: string,
  workloadName: string,
  kind: string,
) {
  if (!target) return false
  return (
    target.clusterId === clusterId &&
    target.namespace === namespace &&
    target.workloadName === workloadName &&
    target.workloadKind.toLowerCase() === kind.toLowerCase() &&
    target.enabled !== false
  )
}

export function targetMatchesDeployment(
  target: NonNullable<ApplicationEnvironment['targets']>[number] | undefined,
  clusterId: string,
  namespace: string,
  name: string,
) {
  return targetMatchesWorkload(target, clusterId, namespace, name, 'Deployment')
}

export function selectorMatchesLabels(
  selector?: Record<string, string>,
  labels?: Record<string, string>,
) {
  const entries = Object.entries(selector ?? {})
  if (entries.length === 0) return false
  return entries.every(([key, value]) => (labels ?? {})[key] === value)
}

export function conditionToTimelineEvent(condition: WorkloadCondition): WorkloadOverviewEvent {
  const timestamp = condition.lastTransitionTime
    ? new Date(condition.lastTransitionTime).getTime()
    : Date.now()
  const ageSeconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000))
  return {
    name: `${condition.type}:${condition.status}`,
    type: condition.status,
    reason: condition.reason || condition.type,
    involvedKind: 'Condition',
    involvedName: condition.type,
    message: condition.message || `${condition.type}: ${condition.status}`,
    count: 1,
    ageSeconds,
  }
}

export interface Deployment {
  name: string
  namespace: string
  desiredReplicas: number
  readyReplicas: number
  updatedReplicas: number
  available: number
  ageSeconds: number
  allowedActions?: string[]
}

export interface DeploymentDetailMeta {
  name: string
  namespace: string
  createdAt?: string
  selector?: Record<string, string>
}

export interface StatefulSetDetailMeta {
  name: string
  namespace: string
  serviceName?: string
  desiredReplicas?: number
  readyReplicas?: number
  currentReplicas?: number
  updateStrategy?: string
  currentRevision?: string
  updateRevision?: string
  createdAt?: string
  selector?: Record<string, string>
}

export interface DaemonSetDetailMeta {
  name: string
  namespace: string
  desiredNumber?: number
  currentNumber?: number
  readyNumber?: number
  availableNumber?: number
  updatedNumber?: number
  updateStrategy?: string
  createdAt?: string
  selector?: Record<string, string>
}

export interface BatchRollbackDraft {
  key: string
  name: string
  namespace: string
  options: Array<{ value: string; label: string }>
  revision: string
}

export function getDeploymentHealth(deployment: Deployment) {
  if (deployment.desiredReplicas === 0) return 'scaled-down'
  if (
    deployment.readyReplicas >= deployment.desiredReplicas &&
    deployment.available >= deployment.desiredReplicas &&
    deployment.updatedReplicas >= deployment.desiredReplicas
  ) {
    return 'healthy'
  }
  if (deployment.readyReplicas === 0 && deployment.available === 0) {
    return 'degraded'
  }
  return 'progressing'
}

export interface Pod {
  uid?: string
  name: string
  namespace: string
  phase: string
  readyContainers: string
  restarts: number
  nodeName: string
  podIp?: string
  cpu?: string
  memory?: string
  requests?: ResourceQuantity
  limits?: ResourceQuantity
  labels?: Record<string, string>
  persistentVolumeClaims?: string[]
  ageSeconds: number
  allowedActions?: string[]
}

export function parseReadyContainers(value: string) {
  const [ready = '0', total = '0'] = value.split('/')
  return {
    ready: Number(ready) || 0,
    total: Number(total) || 0,
  }
}

export function parseCpuValue(value?: string) {
  if (!value) return -1
  const normalized = value.trim().toLowerCase()
  if (!normalized) return -1
  if (normalized.endsWith('m')) {
    return Number.parseFloat(normalized.slice(0, -1)) / 1000
  }
  const parsed = Number.parseFloat(normalized)
  return Number.isNaN(parsed) ? -1 : parsed
}

export function parseMemoryValue(value?: string) {
  if (!value) return -1
  const normalized = value.trim()
  const match = normalized.match(/^([\d.]+)\s*(Ki|Mi|Gi|Ti|Pi|Ei|B)?$/i)
  if (!match) return -1
  const amount = Number.parseFloat(match[1])
  if (Number.isNaN(amount)) return -1
  const unit = (match[2] || 'B').toUpperCase()
  const factors: Record<string, number> = {
    B: 1,
    KI: 1024,
    MI: 1024 ** 2,
    GI: 1024 ** 3,
    TI: 1024 ** 4,
    PI: 1024 ** 5,
    EI: 1024 ** 6,
  }
  return amount * (factors[unit] || 1)
}

export function formatCpuDisplay(value?: string) {
  const formatted = formatCpu(value)
  return formatted === '-' ? value || '-' : formatted
}

export function formatMemoryDisplay(value?: string) {
  if (!value) return '-'
  const formatted = formatBytesAsG(value.replace(/\s+/g, ''))
  return formatted === '-' ? value : formatted
}

export function compareStrings(left?: string, right?: string) {
  return (left || '').localeCompare(right || '')
}

export function podSorter(compareFn: (left: Pod, right: Pod) => number) {
  return (left?: Pod, right?: Pod) => {
    if (!left && !right) return 0
    if (!left) return -1
    if (!right) return 1
    return compareFn(left, right)
  }
}

export interface StatefulSet {
  name: string
  namespace: string
  serviceName?: string
  desiredReplicas: number
  readyReplicas: number
  currentReplicas: number
  ageSeconds: number
  allowedActions?: string[]
}

export interface DaemonSet {
  name: string
  namespace: string
  desiredNumber: number
  currentNumber: number
  readyNumber: number
  availableNumber: number
  updatedNumber: number
  ageSeconds: number
  allowedActions?: string[]
}

export interface Job {
  name: string
  namespace: string
  completions: number
  succeeded: number
  failed: number
  active: number
  completionMode?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface JobDetailMeta extends Job {
  parallelism?: number
  createdAt?: string
  startTime?: string
  completionTime?: string
}

export interface CronJob {
  name: string
  namespace: string
  schedule: string
  suspend: boolean
  activeJobs: number
  lastScheduleTime?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface CronJobDetailMeta extends CronJob {
  concurrencyPolicy?: string
  timeZone?: string
  createdAt?: string
}
