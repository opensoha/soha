export interface LocalizedCopy {
  en_US: string
  zh_CN: string
}

export const SERVICE_ACCOUNT_DEFAULT_TEMPLATE = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: example-service-account
`

export const ROLE_DEFAULT_TEMPLATE = `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: example-role
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
`

export const ROLE_BINDING_DEFAULT_TEMPLATE = `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: example-rolebinding
subjects:
  - kind: ServiceAccount
    name: example-service-account
    namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: example-role
`

export const CLUSTER_ROLE_DEFAULT_TEMPLATE = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: example-cluster-role
rules:
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["get", "list"]
`

export const CLUSTER_ROLE_BINDING_DEFAULT_TEMPLATE = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: example-cluster-rolebinding
subjects:
  - kind: User
    name: example-user
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: example-cluster-role
`

export interface ConfigMapResource {
  ageSeconds: number
  binaryEntries: number
  dataEntries: number
  immutable: boolean
  name: string
  namespace: string
}

export interface SecretResource {
  ageSeconds: number
  dataEntries: number
  immutable: boolean
  name: string
  namespace: string
  type: string
}

export interface ServiceAccountResource {
  ageSeconds: number
  automountServiceAccountToken: boolean
  imagePullSecrets: number
  name: string
  namespace: string
  secrets: number
  allowedActions?: string[]
}

export interface RoleResource {
  ageSeconds: number
  name: string
  namespace: string
  rules: number
  allowedActions?: string[]
}

export interface RoleBindingResource {
  ageSeconds: number
  name: string
  namespace: string
  roleRef: string
  subjects?: string[]
  allowedActions?: string[]
}

export interface ReplicaSetResource {
  ageSeconds: number
  allowedActions?: string[]
  availableReplicas: number
  desiredReplicas: number
  name: string
  namespace: string
  readyReplicas: number
}

export interface EndpointSliceResource {
  addressType: string
  ageSeconds: number
  endpoints: number
  name: string
  namespace: string
  ports?: string[]
}

export interface NetworkPolicyResource {
  ageSeconds: number
  egressRules: number
  ingressRules: number
  name: string
  namespace: string
  policyTypes?: string[]
}

export interface HorizontalPodAutoscalerResource {
  ageSeconds: number
  currentReplicas: number
  desiredReplicas: number
  maxReplicas: number
  minReplicas: number
  name: string
  namespace: string
  targetRef: string
}

export interface PodDisruptionBudgetResource {
  ageSeconds: number
  currentHealthy: number
  desiredHealthy: number
  disruptionsAllowed: number
  maxUnavailable?: string
  minAvailable?: string
  name: string
  namespace: string
}

export interface IngressClassResource {
  ageSeconds: number
  controller: string
  isDefault: boolean
  name: string
  parameters?: string
}

export interface PriorityClassResource {
  ageSeconds: number
  description?: string
  globalDefault: boolean
  name: string
  preemptionPolicy?: string
  value: number
}

export interface RuntimeClassResource {
  ageSeconds: number
  handler: string
  name: string
}

export interface ClusterRoleResource {
  ageSeconds: number
  aggregationRules: number
  name: string
  rules: number
  allowedActions?: string[]
}

export interface ClusterRoleBindingResource {
  ageSeconds: number
  name: string
  roleRef: string
  subjects?: string[]
  allowedActions?: string[]
}

export interface MutatingWebhookConfigurationResource {
  ageSeconds: number
  name: string
  webhooks: number
}

export interface ValidatingWebhookConfigurationResource {
  ageSeconds: number
  name: string
  webhooks: number
}

export interface ResourceQuotaResource {
  ageSeconds: number
  hard?: Record<string, string>
  name: string
  namespace: string
  scopes?: string[]
  used?: Record<string, string>
}

export interface LimitRangeResource {
  ageSeconds: number
  limits: number
  name: string
  namespace: string
}

export interface LeaseResource {
  acquireTime?: string
  ageSeconds: number
  holderIdentity?: string
  leaseDurationSeconds?: number
  name: string
  namespace: string
  renewTime?: string
}

export interface ReplicationControllerResource {
  ageSeconds: number
  allowedActions?: string[]
  availableReplicas: number
  currentReplicas: number
  desiredReplicas: number
  name: string
  namespace: string
  readyReplicas: number
}

export interface PortForwardSession {
  sessionId: string
  clusterId: string
  namespace: string
  targetKind: string
  targetName: string
  localPort: number
  remotePort: number
  status: string
  createdBy?: string
  createdAt: string
}

export interface RBACSubjectSummary {
  kind: string
  label: string
  namespace?: string
  name: string
}

export interface RBACActionConfig<T extends Record<string, any>> {
  resourceKind: string
  getName: (record: T) => string
  getNamespace?: (record: T) => string | undefined
  canDelete?: (record: T) => boolean
}

export interface RBACCreateConfig {
  defaultTemplate: string
  kind: string
  namespaceScope?: 'cluster' | 'required'
}

export interface WorkloadReplicaActionConfig<T extends { allowedActions?: string[] }> {
  resourceKind: string
  getName: (record: T) => string
  getNamespace?: (record: T) => string | undefined
}

export function localize(localeCode: 'zh_CN' | 'en_US', copy: LocalizedCopy) {
  return copy[localeCode]
}

export function normalizeSearchKeyword(value: string) {
  return value.trim().toLowerCase()
}

export function includesSearch(values: Array<string | undefined | null>, keyword: string) {
  if (!keyword) return true
  return values.some((value) => (value ?? '').toLowerCase().includes(keyword))
}

export function parseRBACSubject(value: string): RBACSubjectSummary {
  const normalized = value.trim()
  const [kindPart, remainder = ''] = normalized.split(':', 2)
  const kind = kindPart || 'Subject'
  const parts = remainder.split('/').filter(Boolean)
  if (parts.length >= 2) {
    const namespace = parts[0]
    const name = parts.slice(1).join('/')
    return {
      kind,
      namespace,
      name,
      label: `${kind} ${namespace}/${name}`,
    }
  }
  const name = remainder || normalized
  return {
    kind,
    name,
    label: `${kind} ${name}`.trim(),
  }
}

export function normalizeResourceSearchValue(value: unknown) {
  if (value == null) return undefined
  return String(value)
}

export function buildDefaultResourceSearchValues(record: Record<string, any>) {
  return [
    normalizeResourceSearchValue(record.name),
    normalizeResourceSearchValue(record.namespace),
  ]
}

export function buildResourceSearchPlaceholder(title: LocalizedCopy): LocalizedCopy {
  return {
    zh_CN: `搜索 ${title.zh_CN} 名称 / 命名空间`,
    en_US: `Search ${title.en_US} name / namespace`,
  }
}

export function buildResourceSearchEmptyDescription(title: LocalizedCopy): LocalizedCopy {
  return {
    zh_CN: `没有匹配的 ${title.zh_CN}`,
    en_US: `No matching ${title.en_US}`,
  }
}

export function buildWorkloadReplicaErrorDescription(localeCode: 'zh_CN' | 'en_US', error: unknown) {
  if (error instanceof Error && error.message.trim()) {
    return localeCode === 'zh_CN'
      ? `工作负载资源请求失败：${error.message}`
      : `Failed to load workload resources: ${error.message}`
  }
  return localeCode === 'zh_CN' ? '工作负载资源请求失败。' : 'Failed to load workload resources.'
}

export function buildWorkloadReplicaSearchEmptyDescription(title: LocalizedCopy): LocalizedCopy {
  return {
    zh_CN: `没有匹配的 ${title.zh_CN}`,
    en_US: `No matching ${title.en_US}`,
  }
}

export function buildNamespaceQuery(namespace: string | undefined | null) {
  if (!namespace) return ''
  return `?namespace=${encodeURIComponent(namespace)}`
}

export function buildRBACDetailPath(resourcePath: string, name: string, namespace?: string | null) {
  const base = `/platform-access-control/${resourcePath}/${encodeURIComponent(name)}`
  const query = buildNamespaceQuery(namespace)
  return query ? `${base}${query}` : base
}

export function buildRBACSearchPlaceholder(title: LocalizedCopy): LocalizedCopy {
  return {
    zh_CN: `搜索 ${title.zh_CN}`,
    en_US: `Search ${title.en_US}`,
  }
}

export function buildRBACSearchEmptyDescription(title: LocalizedCopy): LocalizedCopy {
  return {
    zh_CN: `没有匹配的 ${title.zh_CN}`,
    en_US: `No matching ${title.en_US}`,
  }
}

export function buildRequestErrorDescription(localeCode: 'zh_CN' | 'en_US', error: unknown) {
  if (error instanceof Error && error.message.trim()) {
    return localeCode === 'zh_CN'
      ? `RBAC 资源请求失败：${error.message}`
      : `Failed to load RBAC resources: ${error.message}`
  }
  return localeCode === 'zh_CN' ? 'RBAC 资源请求失败。' : 'Failed to load RBAC resources.'
}

export function buildClusterSelectionDescription(localeCode: 'zh_CN' | 'en_US') {
  return localeCode === 'zh_CN' ? '请选择集群查看 RBAC 资源。' : 'Select a cluster to inspect RBAC resources.'
}

export function buildRBACErrorMessage(localeCode: 'zh_CN' | 'en_US') {
  return localeCode === 'zh_CN' ? 'RBAC 资源暂时不可用' : 'RBAC resources unavailable'
}

export function buildRBACRefreshLabel(localeCode: 'zh_CN' | 'en_US') {
  return localeCode === 'zh_CN' ? '刷新' : 'Refresh'
}

export function parseQuotaNumeric(value: string | undefined): number | null {
  if (value == null) return null
  const match = value.trim().match(/^([0-9]*\.?[0-9]+)\s*([a-zA-Z]*)$/)
  if (!match) return null
  const amount = Number.parseFloat(match[1])
  if (!Number.isFinite(amount)) return null
  const unit = match[2] || ''
  const multipliers: Record<string, number> = {
    '': 1, m: 0.001, k: 1000, Ki: 1024, M: 1000 ** 2, Mi: 1024 ** 2,
    G: 1000 ** 3, Gi: 1024 ** 3, T: 1000 ** 4, Ti: 1024 ** 4, P: 1000 ** 5, Pi: 1024 ** 5,
  }
  return multipliers[unit] != null ? amount * multipliers[unit] : amount
}
