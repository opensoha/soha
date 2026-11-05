import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'

export type RelationshipNode = KubernetesResourceGraph['nodes'][number]
export const workloadKinds = [
  'Pod',
  'Deployment',
  'StatefulSet',
  'DaemonSet',
  'Job',
  'CronJob',
  'ReplicaSet',
  'ReplicationController',
] as const
export type WorkloadKind = (typeof workloadKinds)[number]
export const configurationRelationshipKinds = [
  'ConfigMap',
  'Secret',
  'HorizontalPodAutoscaler',
  'PodDisruptionBudget',
  'PriorityClass',
  'RuntimeClass',
  'MutatingWebhookConfiguration',
  'ValidatingWebhookConfiguration',
] as const
export const networkRelationshipKinds = [
  'Service',
  'Ingress',
  'IngressClass',
  'GatewayClass',
  'Gateway',
  'HTTPRoute',
  'GRPCRoute',
  'BackendTLSPolicy',
  'ReferenceGrant',
  'EndpointSlice',
  'NetworkPolicy',
] as const
export const storageRelationshipKinds = [
  'PersistentVolumeClaim',
  'PersistentVolume',
  'StorageClass',
] as const
export const accessControlRelationshipKinds = [
  'ServiceAccount',
  'ClusterRole',
  'Role',
  'ClusterRoleBinding',
  'RoleBinding',
] as const
export type RelationshipRootKind =
  | WorkloadKind
  | (typeof configurationRelationshipKinds)[number]
  | (typeof networkRelationshipKinds)[number]
  | (typeof storageRelationshipKinds)[number]
  | (typeof accessControlRelationshipKinds)[number]
type ResourceDescriptor = {
  permission: string
  apiVersions: readonly string[]
  scope: 'namespace' | 'cluster'
}
// This registry owns built-in relationship browsing identities. A familiar Kind in a
// custom API group is still a different resource, and must never use a built-in route.
export const relationshipResourceDescriptors: Record<string, ResourceDescriptor> = {
  Pod: { permission: 'platform.pods.view', apiVersions: ['v1'], scope: 'namespace' },
  Deployment: {
    permission: 'platform.deployment.view',
    apiVersions: ['apps/v1'],
    scope: 'namespace',
  },
  StatefulSet: {
    permission: 'platform.workloads.stateful-sets.view',
    apiVersions: ['apps/v1'],
    scope: 'namespace',
  },
  DaemonSet: {
    permission: 'platform.workloads.daemon-sets.view',
    apiVersions: ['apps/v1'],
    scope: 'namespace',
  },
  Job: {
    permission: 'platform.workloads.jobs.view',
    apiVersions: ['batch/v1'],
    scope: 'namespace',
  },
  CronJob: {
    permission: 'platform.workloads.cron-jobs.view',
    apiVersions: ['batch/v1'],
    scope: 'namespace',
  },
  ReplicaSet: {
    permission: 'platform.workloads.replica-sets.view',
    apiVersions: ['apps/v1'],
    scope: 'namespace',
  },
  ReplicationController: {
    permission: 'platform.workloads.replication-controllers.view',
    apiVersions: ['v1'],
    scope: 'namespace',
  },
  Namespace: { permission: 'platform.namespaces.view', apiVersions: ['v1'], scope: 'cluster' },
  Node: { permission: 'platform.nodes.view', apiVersions: ['v1'], scope: 'cluster' },
  Service: {
    permission: 'platform.network.services.view',
    apiVersions: ['v1'],
    scope: 'namespace',
  },
  Ingress: {
    permission: 'platform.network.ingresses.view',
    apiVersions: ['networking.k8s.io/v1'],
    scope: 'namespace',
  },
  IngressClass: {
    permission: 'platform.network.ingress-classes.view',
    apiVersions: ['networking.k8s.io/v1'],
    scope: 'cluster',
  },
  GatewayClass: {
    permission: 'platform.network.gateway-classes.view',
    apiVersions: ['gateway.networking.k8s.io/v1', 'gateway.networking.k8s.io/v1beta1'],
    scope: 'cluster',
  },
  Gateway: {
    permission: 'platform.network.gateways.view',
    apiVersions: ['gateway.networking.k8s.io/v1', 'gateway.networking.k8s.io/v1beta1'],
    scope: 'namespace',
  },
  HTTPRoute: {
    permission: 'platform.network.http-routes.view',
    apiVersions: ['gateway.networking.k8s.io/v1', 'gateway.networking.k8s.io/v1beta1'],
    scope: 'namespace',
  },
  GRPCRoute: {
    permission: 'platform.network.grpc-routes.view',
    apiVersions: ['gateway.networking.k8s.io/v1', 'gateway.networking.k8s.io/v1alpha2'],
    scope: 'namespace',
  },
  BackendTLSPolicy: {
    permission: 'platform.network.backend-tls-policies.view',
    apiVersions: [
      'gateway.networking.k8s.io/v1',
      'gateway.networking.k8s.io/v1alpha2',
      'gateway.networking.k8s.io/v1alpha3',
    ],
    scope: 'namespace',
  },
  ReferenceGrant: {
    permission: 'platform.network.reference-grants.view',
    apiVersions: [
      'gateway.networking.k8s.io/v1',
      'gateway.networking.k8s.io/v1beta1',
      'gateway.networking.k8s.io/v1alpha2',
    ],
    scope: 'namespace',
  },
  EndpointSlice: {
    permission: 'platform.network.endpoint-slices.view',
    apiVersions: ['discovery.k8s.io/v1'],
    scope: 'namespace',
  },
  NetworkPolicy: {
    permission: 'platform.network.network-policies.view',
    apiVersions: ['networking.k8s.io/v1'],
    scope: 'namespace',
  },
  ConfigMap: {
    permission: 'platform.configuration.config-maps.view',
    apiVersions: ['v1'],
    scope: 'namespace',
  },
  Secret: {
    permission: 'platform.configuration.secrets.view',
    apiVersions: ['v1'],
    scope: 'namespace',
  },
  ServiceAccount: {
    permission: 'platform.access-control.service-accounts.view',
    apiVersions: ['v1'],
    scope: 'namespace',
  },
  ClusterRole: {
    permission: 'platform.access-control.cluster-roles.view',
    apiVersions: ['rbac.authorization.k8s.io/v1'],
    scope: 'cluster',
  },
  Role: {
    permission: 'platform.access-control.roles.view',
    apiVersions: ['rbac.authorization.k8s.io/v1'],
    scope: 'namespace',
  },
  ClusterRoleBinding: {
    permission: 'platform.access-control.cluster-role-bindings.view',
    apiVersions: ['rbac.authorization.k8s.io/v1'],
    scope: 'cluster',
  },
  RoleBinding: {
    permission: 'platform.access-control.role-bindings.view',
    apiVersions: ['rbac.authorization.k8s.io/v1'],
    scope: 'namespace',
  },
  PersistentVolumeClaim: {
    permission: 'platform.storage.persistent-volume-claims.view',
    apiVersions: ['v1'],
    scope: 'namespace',
  },
  PersistentVolume: {
    permission: 'platform.storage.persistent-volumes.view',
    apiVersions: ['v1'],
    scope: 'cluster',
  },
  StorageClass: {
    permission: 'platform.storage.storage-classes.view',
    apiVersions: ['storage.k8s.io/v1'],
    scope: 'cluster',
  },
  HorizontalPodAutoscaler: {
    permission: 'platform.configuration.horizontal-pod-autoscalers.view',
    apiVersions: ['autoscaling/v1', 'autoscaling/v2'],
    scope: 'namespace',
  },
  PodDisruptionBudget: {
    permission: 'platform.configuration.pod-disruption-budgets.view',
    apiVersions: ['policy/v1'],
    scope: 'namespace',
  },
  ResourceQuota: {
    permission: 'platform.configuration.resource-quotas.view',
    apiVersions: ['v1'],
    scope: 'namespace',
  },
  LimitRange: {
    permission: 'platform.configuration.limit-ranges.view',
    apiVersions: ['v1'],
    scope: 'namespace',
  },
  PriorityClass: {
    permission: 'platform.configuration.priority-classes.view',
    apiVersions: ['scheduling.k8s.io/v1'],
    scope: 'cluster',
  },
  RuntimeClass: {
    permission: 'platform.configuration.runtime-classes.view',
    apiVersions: ['node.k8s.io/v1'],
    scope: 'cluster',
  },
  Lease: {
    permission: 'platform.configuration.leases.view',
    apiVersions: ['coordination.k8s.io/v1'],
    scope: 'namespace',
  },
  MutatingWebhookConfiguration: {
    permission: 'platform.configuration.mutating-webhook-configurations.view',
    apiVersions: ['admissionregistration.k8s.io/v1'],
    scope: 'cluster',
  },
  ValidatingWebhookConfiguration: {
    permission: 'platform.configuration.validating-webhook-configurations.view',
    apiVersions: ['admissionregistration.k8s.io/v1'],
    scope: 'cluster',
  },
}
export function relationshipResourceDescriptor(resource: RelationshipNode['resource']) {
  if (!Object.prototype.hasOwnProperty.call(relationshipResourceDescriptors, resource.kind))
    return undefined
  const descriptor = relationshipResourceDescriptors[resource.kind]
  if (
    !descriptor ||
    !descriptor.apiVersions.includes(resource.apiVersion) ||
    resource.scopeMode !== descriptor.scope
  )
    return undefined
  const namespace = resource.namespace || ''
  if (descriptor.scope === 'namespace') {
    if (namespace.length > 63 || !/^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/.test(namespace))
      return undefined
  } else if (namespace !== '') return undefined
  return descriptor
}
// Each pair describes the selected source and selected target respectively. Never reverse edges.
const relations: Record<string, [string, string, string, string]> = {
  controls: ['控制', '受控于', 'Controls', 'Controlled by'],
  owns: ['拥有', '归属于', 'Owns', 'Owned by'],
  'scheduled-on': ['调度到', '承载', 'Scheduled on', 'Hosts'],
  'uses-config': ['引用配置', '被引用为配置', 'Uses configuration', 'Configuration used by'],
  'uses-secret': ['引用密钥', '被引用为密钥', 'Uses secret', 'Secret used by'],
  mounts: ['声明引用卷', '被声明引用为卷', 'Declares volume reference', 'Declared as volume by'],
  'binds-to': ['绑定到', '绑定来源', 'Binds to', 'Bound from'],
  'uses-storage-class': [
    '引用存储类',
    '存储类被引用于',
    'References storage class',
    'Storage class referenced by',
  ],
  'uses-service-account': [
    '使用服务账号',
    '服务账号用于',
    'Uses service account',
    'Service account used by',
  ],
  'binds-role': [
    '声明绑定角色',
    '被声明为绑定角色',
    'Declares role binding',
    'Referenced as the bound role by',
  ],
  'binds-subject': [
    '声明绑定服务账号',
    '被声明为绑定主体',
    'Declares service account subject',
    'Referenced as a binding subject by',
  ],
  'aggregates-role': [
    '聚合选择器匹配',
    '被聚合选择器匹配',
    'Aggregation selector matches',
    'Matched by aggregation selector of',
  ],
  selects: ['选择器匹配', '被选择器匹配', 'Selector matches', 'Selector matched by'],
  'routes-to': [
    '声明路由后端',
    '被声明为路由后端',
    'Declares route backend',
    'Declared as route backend by',
  ],
  resolves: ['端点关联', '端点关联来源', 'Endpoint association', 'Endpoint association from'],
  'uses-class': ['引用类', '类被引用于', 'References class', 'Class referenced by'],
  parameters: ['引用参数', '参数被引用于', 'References parameters', 'Parameters referenced by'],
  'attaches-to': [
    '被路由父级引用',
    '引用网关父级',
    'Referenced as route parent',
    'References gateway parent',
  ],
  targets: [
    '策略目标声明',
    '被声明为策略目标',
    'Declares policy target',
    'Declared as policy target by',
  ],
  'authorizes-reference': [
    '授权引用声明',
    '引用被授权声明',
    'Declares reference authorization',
    'Reference authorization declared by',
  ],
  'policy-peer': [
    '策略对端选择声明',
    '被声明为策略对端',
    'Declares policy peer selector',
    'Declared as policy peer by',
  ],
  scales: ['伸缩', '被伸缩于', 'Scales', 'Scaled by'],
  protects: ['保护', '受保护于', 'Protects', 'Protected by'],
  'applies-in': ['作用于命名空间', '适用配置', 'Applies in namespace', 'Applicable configuration'],
  'uses-priority-class': [
    '使用优先级类',
    '优先级类用于',
    'Uses priority class',
    'Priority class used by',
  ],
  'uses-runtime-class': [
    '使用运行时类',
    '运行时类用于',
    'Uses runtime class',
    'Runtime class used by',
  ],
  'webhook-service': [
    '声明回调服务',
    '被声明为回调服务',
    'Declares webhook service',
    'Declared as webhook service by',
  ],
  defines: ['定义实例', '由此定义', 'Defines instance', 'Defined by'],
}
const declaredStorageBinding = [
  '声明绑定到',
  '被声明为绑定卷',
  'Declares binding to',
  'Declared as the bound volume by',
] as const
const declaredClaimReference = [
  '被声明为卷申领',
  '声明引用申领',
  'Claim referenced by',
  'Declares claim reference to',
] as const

export function isUnresolvedStorageBinding(token: string, resolution?: string) {
  return token === 'binds-to' && resolution === 'unresolved'
}

export function relationshipLabel(
  token: string,
  locale: string,
  outgoing = true,
  resolution?: string,
  fieldPath?: string,
) {
  let pair: readonly [string, string, string, string] | undefined = relations[token]
  if (isUnresolvedStorageBinding(token, resolution)) {
    if (fieldPath === 'spec.claimRef') {
      pair = declaredClaimReference
    } else if (fieldPath === 'spec.volumeName') {
      pair = declaredStorageBinding
    } else {
      return locale === 'zh_CN' ? '绑定待确认' : 'Binding unconfirmed'
    }
  }
  return pair ? pair[(locale === 'zh_CN' ? 0 : 2) + (outgoing ? 0 : 1)] : token
}
export function isOwnership(relation: string) {
  return relation === 'controls' || relation === 'owns'
}
export function graphMatches(
  graph: KubernetesResourceGraph,
  kind: string,
  name: string,
  clusterId: string | null,
  namespace: string | null,
  apiVersion?: string,
) {
  const root = graph.nodes.find((node) => node.id === graph.rootId)
  const identityMatches = apiVersion
    ? root?.resource.apiVersion === apiVersion &&
      root.resource.scopeMode === (namespace ? 'namespace' : 'cluster')
    : root && relationshipResourceDescriptor(root.resource) &&
      [
        ...workloadKinds,
        ...configurationRelationshipKinds,
        ...networkRelationshipKinds,
        ...storageRelationshipKinds,
        ...accessControlRelationshipKinds,
      ].some((rootKind) => rootKind === kind)
  return Boolean(
    root &&
    identityMatches &&
    Boolean(clusterId?.trim()) &&
    graph.clusterId === clusterId &&
    root.resource.kind === kind &&
    root.resource.name === name &&
    root.resource.clusterId === clusterId &&
    (root.resource.namespace || '') === (namespace || ''),
  )
}
export function filterRelationshipGraph(graph: KubernetesResourceGraph, hiddenIds: Set<string>) {
  return {
    ...graph,
    nodes: graph.nodes.filter((node) => !hiddenIds.has(node.id)),
    edges: graph.edges.filter(
      (edge) => !hiddenIds.has(edge.sourceId) && !hiddenIds.has(edge.targetId),
    ),
  }
}
// Hide only owned descendants of an excluded branch; unrelated incoming edges remain intact.
export function hiddenOwnershipBranches(graph: KubernetesResourceGraph, roots: string[]) {
  const hidden = new Set(roots)
  const queue = [...roots]
  while (queue.length) {
    const id = queue.pop()
    for (const edge of graph.edges)
      if (edge.sourceId === id && isOwnership(edge.relation) && !hidden.has(edge.targetId)) {
        hidden.add(edge.targetId)
        queue.push(edge.targetId)
      }
  }
  return hidden
}
export function podRelationshipGraph(graph: KubernetesResourceGraph, includeOther: boolean) {
  if (includeOther) return graph
  const ancestors = new Set([graph.rootId])
  const queue = [graph.rootId]
  while (queue.length) {
    const id = queue.pop()
    for (const edge of graph.edges)
      if (edge.targetId === id && isOwnership(edge.relation) && !ancestors.has(edge.sourceId)) {
        ancestors.add(edge.sourceId)
        queue.push(edge.sourceId)
      }
  }
  const others = graph.nodes.filter(
    (node) =>
      node.resource.kind === 'ReplicaSet' &&
      relationshipResourceDescriptor(node.resource) &&
      !ancestors.has(node.id) &&
      graph.edges.some(
        (edge) =>
          edge.targetId === node.id && ancestors.has(edge.sourceId) && isOwnership(edge.relation),
      ),
  )
  return filterRelationshipGraph(
    graph,
    hiddenOwnershipBranches(
      graph,
      others.map((node) => node.id),
    ),
  )
}
export function cronJobRelationshipGraph(graph: KubernetesResourceGraph, selectedName?: string) {
  if (!selectedName) return graph
  const otherJobs = graph.nodes.filter(
    (node) =>
      node.resource.kind === 'Job' &&
      relationshipResourceDescriptor(node.resource) &&
      node.resource.name !== selectedName &&
      graph.edges.some(
        (edge) =>
          edge.sourceId === graph.rootId && edge.targetId === node.id && isOwnership(edge.relation),
      ),
  )
  return filterRelationshipGraph(
    graph,
    hiddenOwnershipBranches(
      graph,
      otherJobs.map((node) => node.id),
    ),
  )
}

export type RelationshipEdge = KubernetesResourceGraph['edges'][number]
export function relationshipFacts(edge: RelationshipEdge, locale: string): [string, string][] {
  const zh = locale === 'zh_CN'
  const resolution: Record<string, [string, string]> = {
    ready: ['观测到就绪端点', 'Ready endpoints observed'],
    no_ready_endpoints: ['未观测到就绪端点', 'No ready endpoints observed'],
    unresolved: ['引用未解析', 'Reference unresolved'],
    unknown: ['未知', 'Unknown'],
    partial: ['部分解析', 'Partially resolved'],
  }
  return [
    [
      zh ? '关系解析' : 'Relationship resolution',
      edge.resolutionState
        ? resolution[edge.resolutionState]?.[zh ? 0 : 1] || edge.resolutionState
        : undefined,
    ],
    [zh ? '协议' : 'Protocol', edge.protocol],
    [zh ? '端口' : 'Port', edge.port],
    ['SectionName', edge.sectionName],
    [zh ? '声明字段' : 'Declaration field', edge.fieldPath],
    [zh ? '说明' : 'Note', edge.note],
  ].filter((fact): fact is [string, string] => Boolean(fact[1]))
}
