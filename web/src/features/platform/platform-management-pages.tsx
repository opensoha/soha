import { Card, Empty, Tag, Typography } from '@douyinfe/semi-ui'
import { useQuery } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { BooleanTag } from '@/components/status-tag'
import { PageHeader } from '@/components/page-header'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

const { Text } = Typography

type ScopeKind = 'cluster' | 'mixed' | 'namespace'

interface LocalizedCopy {
  en_US: string
  zh_CN: string
}

interface ConfigMapResource {
  ageSeconds: number
  binaryEntries: number
  dataEntries: number
  immutable: boolean
  name: string
  namespace: string
}

interface SecretResource {
  ageSeconds: number
  dataEntries: number
  immutable: boolean
  name: string
  namespace: string
  type: string
}

interface ServiceAccountResource {
  ageSeconds: number
  automountServiceAccountToken: boolean
  imagePullSecrets: number
  name: string
  namespace: string
  secrets: number
}

interface RoleResource {
  ageSeconds: number
  name: string
  namespace: string
  rules: number
}

interface RoleBindingResource {
  ageSeconds: number
  name: string
  namespace: string
  roleRef: string
  subjects?: string[]
}

interface ReplicaSetResource {
  ageSeconds: number
  availableReplicas: number
  desiredReplicas: number
  name: string
  namespace: string
  readyReplicas: number
}

interface EndpointSliceResource {
  addressType: string
  ageSeconds: number
  endpoints: number
  name: string
  namespace: string
  ports?: string[]
}

interface NetworkPolicyResource {
  ageSeconds: number
  egressRules: number
  ingressRules: number
  name: string
  namespace: string
  policyTypes?: string[]
}

interface HorizontalPodAutoscalerResource {
  ageSeconds: number
  currentReplicas: number
  desiredReplicas: number
  maxReplicas: number
  minReplicas: number
  name: string
  namespace: string
  targetRef: string
}

interface PodDisruptionBudgetResource {
  ageSeconds: number
  currentHealthy: number
  desiredHealthy: number
  disruptionsAllowed: number
  maxUnavailable?: string
  minAvailable?: string
  name: string
  namespace: string
}

function localize(localeCode: 'zh_CN' | 'en_US', copy: LocalizedCopy) {
  return copy[localeCode]
}

function scopeLabel(localeCode: 'zh_CN' | 'en_US', scope: ScopeKind) {
  if (scope === 'cluster') {
    return localeCode === 'zh_CN' ? '作用域：Cluster 级' : 'Scope: cluster-wide'
  }
  if (scope === 'namespace') {
    return localeCode === 'zh_CN' ? '作用域：Namespace 级' : 'Scope: namespace-scoped'
  }
  return localeCode === 'zh_CN' ? '作用域：同时覆盖 Cluster / Namespace' : 'Scope: mixed cluster and namespace'
}

function PlaceholderPanel({
  description,
  scope,
  title,
}: {
  description: LocalizedCopy
  scope: ScopeKind
  title: LocalizedCopy
}) {
  const { localeCode } = useI18n()
  return (
    <Card>
      <Empty title={localize(localeCode, title)} description={localize(localeCode, description)} />
      <Text type="tertiary" size="small" style={{ display: 'block', marginTop: 12 }}>
        {scopeLabel(localeCode, scope)}
      </Text>
    </Card>
  )
}

function buildPendingDescription(resourceZh: string, resourceEn: string): LocalizedCopy {
  return {
    zh_CN: `当前平台导航已经补齐 ${resourceZh} 页面，待后端统一资源 API 接入后再恢复真实列表与操作能力。`,
    en_US: `The ${resourceEn} page is now wired into platform navigation. Connect the unified backend resource API next to restore real list and action support.`,
  }
}

function buildInspectDescription(resourceZh: string, resourceEn: string): LocalizedCopy {
  return {
    zh_CN: `查看当前 cluster / namespace scope 下的 ${resourceZh} 资源和后续操作入口。`,
    en_US: `Inspect ${resourceEn} resources and follow-up operations in the current cluster and namespace scope.`,
  }
}

function buildPlaceholderTitle(resourceZh: string, resourceEn: string): LocalizedCopy {
  return {
    zh_CN: `${resourceZh} 暂待接入`,
    en_US: `${resourceEn} pending API`,
  }
}

function createPlaceholderPage(
  title: LocalizedCopy,
  scope: ScopeKind,
  resourceZh: string,
  resourceEn: string,
) {
  return function ResourcePage() {
    const { localeCode } = useI18n()
    return (
      <div className="kc-page">
        <PageHeader title={localize(localeCode, title)} description={localize(localeCode, buildInspectDescription(resourceZh, resourceEn))} />
        <PlatformScopeToolbar />
        <PlaceholderPanel
          title={buildPlaceholderTitle(resourceZh, resourceEn)}
          description={buildPendingDescription(resourceZh, resourceEn)}
          scope={scope}
        />
      </div>
    )
  }
}

function useScopedResourceQuery<T>(resourcePath: string) {
  const { clusterId, namespace } = usePlatformScopeStore()
  return useQuery({
    queryKey: ['platform-resource', resourcePath, clusterId, namespace],
    queryFn: () => api.get<ApiResponse<T[]>>(buildClusterScopedPath(clusterId!, resourcePath, namespace)),
    enabled: !!clusterId,
  })
}

function ResourceTableCard<T extends Record<string, any>>({
  columns,
  emptyDescription,
  resourcePath,
  rowKey,
}: {
  columns: ColumnProps<T>[]
  emptyDescription: LocalizedCopy
  resourcePath: string
  rowKey: string | ((record: T) => string)
}) {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const query = useScopedResourceQuery<T>(resourcePath)

  if (!clusterId) {
    return <Empty description={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'} />
  }

  return (
    <AdminTable
      columns={columns}
      dataSource={query.data?.data ?? []}
      rowKey={rowKey}
      loading={query.isLoading}
      empty={<Empty description={localize(localeCode, emptyDescription)} />}
      pageSize={20}
      enableColumnSelection={false}
    />
  )
}

function ResourceListPage<T extends Record<string, any>>({
  columns,
  emptyDescription,
  resourcePath,
  rowKey,
  title,
}: {
  columns: ColumnProps<T>[]
  emptyDescription: LocalizedCopy
  resourcePath: string
  rowKey: string | ((record: T) => string)
  title: LocalizedCopy
}) {
  const { localeCode } = useI18n()
  const titleZh = localize('zh_CN', title)
  const titleEn = localize('en_US', title)

  return (
    <div className="kc-page">
      <PageHeader title={localize(localeCode, title)} description={localize(localeCode, buildInspectDescription(titleZh, titleEn))} />
      <PlatformScopeToolbar />
      <ResourceTableCard<T>
        columns={columns}
        resourcePath={resourcePath}
        rowKey={rowKey}
        emptyDescription={emptyDescription}
      />
    </div>
  )
}

const configMapColumns: ColumnProps<ConfigMapResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Data', dataIndex: 'dataEntries' },
  { title: 'Binary', dataIndex: 'binaryEntries' },
  { title: 'Immutable', dataIndex: 'immutable', render: (value: boolean) => <BooleanTag value={value} /> },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const secretColumns: ColumnProps<SecretResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Type', dataIndex: 'type', render: (value: string) => value || '-' },
  { title: 'Data', dataIndex: 'dataEntries' },
  { title: 'Immutable', dataIndex: 'immutable', render: (value: boolean) => <BooleanTag value={value} /> },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const replicaSetColumns: ColumnProps<ReplicaSetResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Desired', dataIndex: 'desiredReplicas' },
  { title: 'Ready', dataIndex: 'readyReplicas' },
  { title: 'Available', dataIndex: 'availableReplicas' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const hpaColumns: ColumnProps<HorizontalPodAutoscalerResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Target', dataIndex: 'targetRef' },
  { title: 'Min', dataIndex: 'minReplicas' },
  { title: 'Max', dataIndex: 'maxReplicas' },
  { title: 'Current', dataIndex: 'currentReplicas' },
  { title: 'Desired', dataIndex: 'desiredReplicas' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const pdbColumns: ColumnProps<PodDisruptionBudgetResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Min Available', dataIndex: 'minAvailable', render: (value: string) => value || '-' },
  { title: 'Max Unavailable', dataIndex: 'maxUnavailable', render: (value: string) => value || '-' },
  { title: 'Healthy', dataIndex: 'currentHealthy' },
  { title: 'Desired', dataIndex: 'desiredHealthy' },
  { title: 'Allowed', dataIndex: 'disruptionsAllowed' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const endpointSliceColumns: ColumnProps<EndpointSliceResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Address Type', dataIndex: 'addressType' },
  { title: 'Endpoints', dataIndex: 'endpoints' },
  { title: 'Ports', dataIndex: 'ports', render: (value: string[] | undefined) => value?.join(', ') || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const networkPolicyColumns: ColumnProps<NetworkPolicyResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Policy Types', dataIndex: 'policyTypes', render: (value: string[] | undefined) => value?.map((item) => <Tag key={item} size="small">{item}</Tag>) ?? '-' },
  { title: 'Ingress Rules', dataIndex: 'ingressRules' },
  { title: 'Egress Rules', dataIndex: 'egressRules' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const serviceAccountColumns: ColumnProps<ServiceAccountResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Secrets', dataIndex: 'secrets' },
  { title: 'Image Pull Secrets', dataIndex: 'imagePullSecrets' },
  { title: 'Automount SA Token', dataIndex: 'automountServiceAccountToken', render: (value: boolean) => <BooleanTag value={value} /> },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const roleColumns: ColumnProps<RoleResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Rules', dataIndex: 'rules' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const roleBindingColumns: ColumnProps<RoleBindingResource>[] = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'RoleRef', dataIndex: 'roleRef' },
  { title: 'Subjects', dataIndex: 'subjects', render: (value: string[] | undefined) => value?.join(', ') || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

export function WorkloadsReplicaSetsPage() {
  return (
    <ResourceListPage<ReplicaSetResource>
      title={{ zh_CN: 'ReplicaSets', en_US: 'ReplicaSets' }}
      resourcePath="workloads/replicasets"
      columns={replicaSetColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 ReplicaSets', en_US: 'No replica sets in the current scope' }}
    />
  )
}

export const WorkloadsReplicationControllersPage = createPlaceholderPage(
  { zh_CN: 'ReplicationControllers', en_US: 'ReplicationControllers' },
  'namespace',
  'ReplicationControllers',
  'ReplicationControllers',
)

export function ConfigurationConfigMapsPage() {
  return (
    <ResourceListPage<ConfigMapResource>
      title={{ zh_CN: 'ConfigMaps', en_US: 'ConfigMaps' }}
      resourcePath="configuration/configmaps"
      columns={configMapColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 ConfigMaps', en_US: 'No configmaps in the current scope' }}
    />
  )
}

export function ConfigurationSecretsPage() {
  return (
    <ResourceListPage<SecretResource>
      title={{ zh_CN: 'Secrets', en_US: 'Secrets' }}
      resourcePath="configuration/secrets"
      columns={secretColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 Secrets', en_US: 'No secrets in the current scope' }}
    />
  )
}

export const ConfigurationResourceQuotasPage = createPlaceholderPage(
  { zh_CN: 'ResourceQuotas', en_US: 'ResourceQuotas' },
  'namespace',
  'ResourceQuotas',
  'ResourceQuotas',
)

export const ConfigurationLimitRangesPage = createPlaceholderPage(
  { zh_CN: 'LimitRanges', en_US: 'LimitRanges' },
  'namespace',
  'LimitRanges',
  'LimitRanges',
)

export function ConfigurationHPAPage() {
  return (
    <ResourceListPage<HorizontalPodAutoscalerResource>
      title={{ zh_CN: 'HorizontalPodAutoscalers', en_US: 'HorizontalPodAutoscalers' }}
      resourcePath="configuration/hpas"
      columns={hpaColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 HPA', en_US: 'No HPA resources in the current scope' }}
    />
  )
}

export function ConfigurationPodDisruptionBudgetsPage() {
  return (
    <ResourceListPage<PodDisruptionBudgetResource>
      title={{ zh_CN: 'PodDisruptionBudgets', en_US: 'PodDisruptionBudgets' }}
      resourcePath="configuration/poddisruptionbudgets"
      columns={pdbColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 PodDisruptionBudgets', en_US: 'No pod disruption budgets in the current scope' }}
    />
  )
}

export const ConfigurationPriorityClassesPage = createPlaceholderPage(
  { zh_CN: 'PriorityClasses', en_US: 'PriorityClasses' },
  'cluster',
  'PriorityClasses',
  'PriorityClasses',
)

export const ConfigurationRuntimeClassesPage = createPlaceholderPage(
  { zh_CN: 'RuntimeClasses', en_US: 'RuntimeClasses' },
  'cluster',
  'RuntimeClasses',
  'RuntimeClasses',
)

export const ConfigurationLeasesPage = createPlaceholderPage(
  { zh_CN: 'Leases', en_US: 'Leases' },
  'mixed',
  'Leases',
  'Leases',
)

export const ConfigurationMutatingWebhooksPage = createPlaceholderPage(
  { zh_CN: 'MutatingWebhookConfigurations', en_US: 'MutatingWebhookConfigurations' },
  'cluster',
  'MutatingWebhookConfigurations',
  'MutatingWebhookConfigurations',
)

export const ConfigurationValidatingWebhooksPage = createPlaceholderPage(
  { zh_CN: 'ValidatingWebhookConfigurations', en_US: 'ValidatingWebhookConfigurations' },
  'cluster',
  'ValidatingWebhookConfigurations',
  'ValidatingWebhookConfigurations',
)

export function NetworkEndpointSlicesPage() {
  return (
    <ResourceListPage<EndpointSliceResource>
      title={{ zh_CN: 'EndpointSlices', en_US: 'EndpointSlices' }}
      resourcePath="network/endpointslices"
      columns={endpointSliceColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 EndpointSlices', en_US: 'No endpoint slices in the current scope' }}
    />
  )
}

export const NetworkIngressClassesPage = createPlaceholderPage(
  { zh_CN: 'IngressClasses', en_US: 'IngressClasses' },
  'cluster',
  'IngressClasses',
  'IngressClasses',
)

export function NetworkPoliciesPage() {
  return (
    <ResourceListPage<NetworkPolicyResource>
      title={{ zh_CN: 'NetworkPolicies', en_US: 'NetworkPolicies' }}
      resourcePath="network/networkpolicies"
      columns={networkPolicyColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 NetworkPolicies', en_US: 'No network policies in the current scope' }}
    />
  )
}

export const NetworkPortForwardPage = createPlaceholderPage(
  { zh_CN: 'Port Forward', en_US: 'Port Forward' },
  'mixed',
  'Port Forward',
  'Port Forward',
)

export function PlatformAccessControlServiceAccountsPage() {
  return (
    <ResourceListPage<ServiceAccountResource>
      title={{ zh_CN: 'ServiceAccounts', en_US: 'ServiceAccounts' }}
      resourcePath="access-control/serviceaccounts"
      columns={serviceAccountColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 ServiceAccounts', en_US: 'No service accounts in the current scope' }}
    />
  )
}

export const PlatformAccessControlClusterRolesPage = createPlaceholderPage(
  { zh_CN: 'ClusterRoles', en_US: 'ClusterRoles' },
  'cluster',
  'ClusterRoles',
  'ClusterRoles',
)

export function PlatformAccessControlRolesPage() {
  return (
    <ResourceListPage<RoleResource>
      title={{ zh_CN: 'Roles', en_US: 'Roles' }}
      resourcePath="access-control/roles"
      columns={roleColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 Roles', en_US: 'No roles in the current scope' }}
    />
  )
}

export const PlatformAccessControlClusterRoleBindingsPage = createPlaceholderPage(
  { zh_CN: 'ClusterRoleBindings', en_US: 'ClusterRoleBindings' },
  'cluster',
  'ClusterRoleBindings',
  'ClusterRoleBindings',
)

export function PlatformAccessControlRoleBindingsPage() {
  return (
    <ResourceListPage<RoleBindingResource>
      title={{ zh_CN: 'RoleBindings', en_US: 'RoleBindings' }}
      resourcePath="access-control/rolebindings"
      columns={roleBindingColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 RoleBindings', en_US: 'No role bindings in the current scope' }}
    />
  )
}
