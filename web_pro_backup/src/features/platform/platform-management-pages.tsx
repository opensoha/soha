import { useState } from 'react'
import { App, Button, Empty, Input, InputNumber, Modal, Progress, Select, Space, Tag, Tooltip, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { useResourceActions } from '@/components/resource-actions'
import { BooleanTag } from '@/components/status-tag'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import {
  CONFIGMAP_DEFAULT_TEMPLATE,
  CreateResourceModal,
  SECRET_DEFAULT_TEMPLATE,
} from '@/features/platform/configuration-detail-pages'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'
import type { TableColumnsType } from 'antd'

const { Text } = Typography




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

interface IngressClassResource {
  ageSeconds: number
  controller: string
  isDefault: boolean
  name: string
  parameters?: string
}

interface PriorityClassResource {
  ageSeconds: number
  description?: string
  globalDefault: boolean
  name: string
  preemptionPolicy?: string
  value: number
}

interface RuntimeClassResource {
  ageSeconds: number
  handler: string
  name: string
}

interface ClusterRoleResource {
  ageSeconds: number
  aggregationRules: number
  name: string
  rules: number
}

interface ClusterRoleBindingResource {
  ageSeconds: number
  name: string
  roleRef: string
  subjects?: string[]
}

interface MutatingWebhookConfigurationResource {
  ageSeconds: number
  name: string
  webhooks: number
}

interface ValidatingWebhookConfigurationResource {
  ageSeconds: number
  name: string
  webhooks: number
}

interface ResourceQuotaResource {
  ageSeconds: number
  hard?: Record<string, string>
  name: string
  namespace: string
  scopes?: string[]
  used?: Record<string, string>
}

interface LimitRangeResource {
  ageSeconds: number
  limits: number
  name: string
  namespace: string
}

interface LeaseResource {
  acquireTime?: string
  ageSeconds: number
  holderIdentity?: string
  leaseDurationSeconds?: number
  name: string
  namespace: string
  renewTime?: string
}

interface ReplicationControllerResource {
  ageSeconds: number
  availableReplicas: number
  currentReplicas: number
  desiredReplicas: number
  name: string
  namespace: string
  readyReplicas: number
}

interface PortForwardSession {
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

function localize(localeCode: 'zh_CN' | 'en_US', copy: LocalizedCopy) {
  return copy[localeCode]
}

function buildInspectDescription(resourceZh: string, resourceEn: string): LocalizedCopy {
  return {
    zh_CN: `查看当前 cluster / namespace scope 下的 ${resourceZh} 资源和后续操作入口。`,
    en_US: `Inspect ${resourceEn} resources and follow-up operations in the current cluster and namespace scope.`,
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
  actionConfig,
}: {
  columns: TableColumnsType<T>
  emptyDescription: LocalizedCopy
  resourcePath: string
  rowKey: string | ((record: T) => string)
  actionConfig?: {
    resourceKind: string
    resourceLabel?: string
    getName: (record: T) => string
    getNamespace?: (record: T) => string | undefined
  }
}) {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const query = useScopedResourceQuery<T>(resourcePath)

  const { column: actionColumn, modalNode } = useResourceActions<T>({
    resourcePath,
    resourceKind: actionConfig?.resourceKind ?? 'Resource',
    resourceLabel: actionConfig?.resourceLabel,
    getName: actionConfig?.getName ?? (() => ''),
    getNamespace: actionConfig?.getNamespace,
  })

  if (!clusterId) {
    return <Empty description={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'} />
  }

  const effectiveColumns = actionConfig ? [...columns, actionColumn] : columns
  const scroll = actionConfig ? { x: 'max-content' as const } : undefined

  return (
    <>
      {actionConfig ? modalNode : null}
      <AdminTable
        columns={effectiveColumns}
        dataSource={query.data?.data ?? []}
        rowKey={rowKey}
        loading={query.isLoading}
        empty={<Empty description={localize(localeCode, emptyDescription)} />}
        pageSize={10}
        enableColumnSelection={false}
        scroll={scroll}
      />
    </>
  )
}

function ResourceListPage<T extends Record<string, any>>({
  columns,
  emptyDescription,
  resourcePath,
  rowKey,
  title,
  actionConfig,
}: {
  columns: TableColumnsType<T>
  emptyDescription: LocalizedCopy
  resourcePath: string
  rowKey: string | ((record: T) => string)
  title: LocalizedCopy
  actionConfig?: {
    resourceKind: string
    resourceLabel?: string
    getName: (record: T) => string
    getNamespace?: (record: T) => string | undefined
  }
}) {
  const { localeCode } = useI18n()
  const titleZh = localize('zh_CN', title)
  const titleEn = localize('en_US', title)

  return (
    <div className="kc-page">
      <PlatformScopeToolbar />
      <ResourceTableCard<T>
        columns={columns}
        resourcePath={resourcePath}
        rowKey={rowKey}
        emptyDescription={emptyDescription}
        actionConfig={actionConfig}
      />
    </div>
  )
}

function buildNamespaceQuery(namespace: string | undefined | null) {
  if (!namespace) return ''
  return `?namespace=${encodeURIComponent(namespace)}`
}

function ResourceNameLink({ to, name }: { to: string; name: string }) {
  const navigate = useNavigate()
  return (
    <Button type="text" onClick={() => navigate(to)}>{name}</Button>
  )
}

const configMapColumns: TableColumnsType<ConfigMapResource> = [
  {
    title: 'Name',
    dataIndex: 'name',
    render: (value: string, record: ConfigMapResource) => (
      <ResourceNameLink name={value} to={`/configuration/configmaps/${encodeURIComponent(value)}${buildNamespaceQuery(record.namespace)}`} />
    ),
  },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Data', dataIndex: 'dataEntries' },
  { title: 'Binary', dataIndex: 'binaryEntries' },
  { title: 'Immutable', dataIndex: 'immutable', render: (value: boolean) => <BooleanTag value={value} /> },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const secretColumns: TableColumnsType<SecretResource> = [
  {
    title: 'Name',
    dataIndex: 'name',
    render: (value: string, record: SecretResource) => (
      <ResourceNameLink name={value} to={`/configuration/secrets/${encodeURIComponent(value)}${buildNamespaceQuery(record.namespace)}`} />
    ),
  },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Type', dataIndex: 'type', render: (value: string) => value || '-' },
  { title: 'Data', dataIndex: 'dataEntries' },
  { title: 'Immutable', dataIndex: 'immutable', render: (value: boolean) => <BooleanTag value={value} /> },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const replicaSetColumns: TableColumnsType<ReplicaSetResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Desired', dataIndex: 'desiredReplicas' },
  { title: 'Ready', dataIndex: 'readyReplicas' },
  { title: 'Available', dataIndex: 'availableReplicas' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const hpaColumns: TableColumnsType<HorizontalPodAutoscalerResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Target', dataIndex: 'targetRef' },
  { title: 'Min', dataIndex: 'minReplicas' },
  { title: 'Max', dataIndex: 'maxReplicas' },
  { title: 'Current', dataIndex: 'currentReplicas' },
  { title: 'Desired', dataIndex: 'desiredReplicas' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const pdbColumns: TableColumnsType<PodDisruptionBudgetResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Min Available', dataIndex: 'minAvailable', render: (value: string) => value || '-' },
  { title: 'Max Unavailable', dataIndex: 'maxUnavailable', render: (value: string) => value || '-' },
  { title: 'Healthy', dataIndex: 'currentHealthy' },
  { title: 'Desired', dataIndex: 'desiredHealthy' },
  { title: 'Allowed', dataIndex: 'disruptionsAllowed' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const endpointSliceColumns: TableColumnsType<EndpointSliceResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Address Type', dataIndex: 'addressType' },
  { title: 'Endpoints', dataIndex: 'endpoints' },
  { title: 'Ports', dataIndex: 'ports', render: (value: string[] | undefined) => value?.join(', ') || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const networkPolicyColumns: TableColumnsType<NetworkPolicyResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Policy Types', dataIndex: 'policyTypes', render: (value: string[] | undefined) => value?.map((item) => <Tag key={item}>{item}</Tag>) ?? '-' },
  { title: 'Ingress Rules', dataIndex: 'ingressRules' },
  { title: 'Egress Rules', dataIndex: 'egressRules' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const serviceAccountColumns: TableColumnsType<ServiceAccountResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Secrets', dataIndex: 'secrets' },
  { title: 'Image Pull Secrets', dataIndex: 'imagePullSecrets' },
  { title: 'Automount SA Token', dataIndex: 'automountServiceAccountToken', render: (value: boolean) => <BooleanTag value={value} /> },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const roleColumns: TableColumnsType<RoleResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Rules', dataIndex: 'rules' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const roleBindingColumns: TableColumnsType<RoleBindingResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'RoleRef', dataIndex: 'roleRef' },
  { title: 'Subjects', dataIndex: 'subjects', render: (value: string[] | undefined) => value?.join(', ') || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const ingressClassColumns: TableColumnsType<IngressClassResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Controller', dataIndex: 'controller' },
  { title: 'Default', dataIndex: 'isDefault', render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" /> },
  { title: 'Parameters', dataIndex: 'parameters', render: (value: string | undefined) => value || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const priorityClassColumns: TableColumnsType<PriorityClassResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Value', dataIndex: 'value' },
  { title: 'Global Default', dataIndex: 'globalDefault', render: (value: boolean) => <BooleanTag value={value} trueLabel="Yes" falseLabel="No" /> },
  { title: 'Preemption', dataIndex: 'preemptionPolicy', render: (value: string | undefined) => value || '-' },
  { title: 'Description', dataIndex: 'description', render: (value: string | undefined) => value || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const runtimeClassColumns: TableColumnsType<RuntimeClassResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Handler', dataIndex: 'handler' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const clusterRoleColumns: TableColumnsType<ClusterRoleResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Rules', dataIndex: 'rules' },
  { title: 'Aggregation', dataIndex: 'aggregationRules' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const clusterRoleBindingColumns: TableColumnsType<ClusterRoleBindingResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'RoleRef', dataIndex: 'roleRef' },
  { title: 'Subjects', dataIndex: 'subjects', render: (value: string[] | undefined) => value?.join(', ') || '-' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const mutatingWebhookColumns: TableColumnsType<MutatingWebhookConfigurationResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Webhooks', dataIndex: 'webhooks' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const validatingWebhookColumns: TableColumnsType<ValidatingWebhookConfigurationResource> = [
  { title: 'Name', dataIndex: 'name' },
  { title: 'Webhooks', dataIndex: 'webhooks' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

function parseQuotaNumeric(value: string | undefined): number | null {
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

function renderQuotaProgress(record: ResourceQuotaResource) {
  const hard = record.hard ?? {}
  const used = record.used ?? {}
  const keys = Object.keys(hard)
  if (keys.length === 0) return <Text type="secondary">-</Text>
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 220 }}>
      {keys.map((key) => {
        const hardRaw = hard[key]
        const usedRaw = used[key] ?? '0'
        const hardNum = parseQuotaNumeric(hardRaw)
        const usedNum = parseQuotaNumeric(usedRaw)
        const percent = hardNum && hardNum > 0 && usedNum != null
          ? Math.min(100, Math.round((usedNum / hardNum) * 100))
          : 0
        return (
          <Tooltip key={key} title={`${key}: ${usedRaw} / ${hardRaw}`} placement="top">
            <div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'rgba(0, 0, 0, 0.45)' }}>
                <span style={{ maxWidth: 140, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{key}</span>
                <span>{usedRaw} / {hardRaw}</span>
              </div>
              <Progress percent={percent} aria-label={`${key} quota usage`} showInfo={false} size="small" />
            </div>
          </Tooltip>
        )
      })}
    </div>
  )
}

const resourceQuotaColumns: TableColumnsType<ResourceQuotaResource> = [
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Name', dataIndex: 'name' },
  { title: 'Scopes', dataIndex: 'scopes', render: (value: string[] | undefined) => value?.join(', ') || '-' },
  { title: 'Usage', dataIndex: 'hard', render: (_: unknown, record: ResourceQuotaResource) => renderQuotaProgress(record) },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const limitRangeColumns: TableColumnsType<LimitRangeResource> = [
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Name', dataIndex: 'name' },
  { title: 'Limits', dataIndex: 'limits' },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const leaseColumns: TableColumnsType<LeaseResource> = [
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Name', dataIndex: 'name' },
  { title: 'Holder', dataIndex: 'holderIdentity', render: (value: string | undefined) => value || '-' },
  { title: 'Duration (s)', dataIndex: 'leaseDurationSeconds', render: (value: number | undefined) => (value == null ? '-' : String(value)) },
  { title: 'Acquired', dataIndex: 'acquireTime', render: (value: string | undefined) => (value ? formatDateTime(value) : '-') },
  { title: 'Renewed', dataIndex: 'renewTime', render: (value: string | undefined) => (value ? formatDateTime(value) : '-') },
  { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
]

const replicationControllerColumns: TableColumnsType<ReplicationControllerResource> = [
  { title: 'Namespace', dataIndex: 'namespace' },
  { title: 'Name', dataIndex: 'name' },
  { title: 'Desired', dataIndex: 'desiredReplicas' },
  { title: 'Current', dataIndex: 'currentReplicas' },
  { title: 'Ready', dataIndex: 'readyReplicas' },
  { title: 'Available', dataIndex: 'availableReplicas' },
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

export function WorkloadsReplicationControllersPage() {
  return (
    <ResourceListPage<ReplicationControllerResource>
      title={{ zh_CN: 'ReplicationControllers', en_US: 'ReplicationControllers' }}
      resourcePath="workloads/replicationcontrollers"
      columns={replicationControllerColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 ReplicationController', en_US: 'No replication controllers in the current scope' }}
      actionConfig={{
        resourceKind: 'ReplicationController',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
      }}
    />
  )
}

export function ConfigurationConfigMapsPage() {
  const { localeCode } = useI18n()
  const [createVisible, setCreateVisible] = useState(false)
  return (
    <div className="kc-page">
      <PageHeader
        title="ConfigMaps"
        description={localeCode === 'zh_CN' ? '查看当前 cluster / namespace scope 下的 ConfigMaps 资源和后续操作入口。' : 'Inspect ConfigMaps resources and follow-up operations in the current cluster and namespace scope.'}
        actions={(
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateVisible(true)}>
            {localeCode === 'zh_CN' ? '新增' : 'Create'}
          </Button>
        )}
      />
      <PlatformScopeToolbar />
      <ResourceTableCard<ConfigMapResource>
        columns={configMapColumns}
        resourcePath="configuration/configmaps"
        rowKey={(record) => `${record.namespace}/${record.name}`}
        emptyDescription={{ zh_CN: '当前范围没有 ConfigMaps', en_US: 'No configmaps in the current scope' }}
        actionConfig={{
          resourceKind: 'ConfigMap',
          getName: (record) => record.name,
          getNamespace: (record) => record.namespace,
        }}
      />
      <CreateResourceModal
        visible={createVisible}
        onClose={() => setCreateVisible(false)}
        kind="ConfigMap"
        resourcePath="configuration/configmaps"
        defaultTemplate={CONFIGMAP_DEFAULT_TEMPLATE}
        invalidationKeys={[['platform-resource', 'configuration/configmaps']]}
      />
    </div>
  )
}

export function ConfigurationSecretsPage() {
  const { localeCode } = useI18n()
  const [createVisible, setCreateVisible] = useState(false)
  return (
    <div className="kc-page">
      <PageHeader
        title="Secrets"
        description={localeCode === 'zh_CN' ? '查看当前 cluster / namespace scope 下的 Secrets 资源和后续操作入口。' : 'Inspect Secrets resources and follow-up operations in the current cluster and namespace scope.'}
        actions={(
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateVisible(true)}>
            {localeCode === 'zh_CN' ? '新增' : 'Create'}
          </Button>
        )}
      />
      <PlatformScopeToolbar />
      <ResourceTableCard<SecretResource>
        columns={secretColumns}
        resourcePath="configuration/secrets"
        rowKey={(record) => `${record.namespace}/${record.name}`}
        emptyDescription={{ zh_CN: '当前范围没有 Secrets', en_US: 'No secrets in the current scope' }}
        actionConfig={{
          resourceKind: 'Secret',
          getName: (record) => record.name,
          getNamespace: (record) => record.namespace,
        }}
      />
      <CreateResourceModal
        visible={createVisible}
        onClose={() => setCreateVisible(false)}
        kind="Secret"
        resourcePath="configuration/secrets"
        defaultTemplate={SECRET_DEFAULT_TEMPLATE}
        invalidationKeys={[['platform-resource', 'configuration/secrets']]}
      />
    </div>
  )
}

export function ConfigurationResourceQuotasPage() {
  return (
    <ResourceListPage<ResourceQuotaResource>
      title={{ zh_CN: 'ResourceQuotas', en_US: 'ResourceQuotas' }}
      resourcePath="configuration/resourcequotas"
      columns={resourceQuotaColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 ResourceQuota', en_US: 'No resource quotas in the current scope' }}
      actionConfig={{
        resourceKind: 'ResourceQuota',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
      }}
    />
  )
}

export function ConfigurationLimitRangesPage() {
  return (
    <ResourceListPage<LimitRangeResource>
      title={{ zh_CN: 'LimitRanges', en_US: 'LimitRanges' }}
      resourcePath="configuration/limitranges"
      columns={limitRangeColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 LimitRange', en_US: 'No limit ranges in the current scope' }}
      actionConfig={{
        resourceKind: 'LimitRange',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
      }}
    />
  )
}

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

export function ConfigurationPriorityClassesPage() {
  return (
    <ResourceListPage<PriorityClassResource>
      title={{ zh_CN: 'PriorityClasses', en_US: 'PriorityClasses' }}
      resourcePath="configuration/priorityclasses"
      columns={priorityClassColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 PriorityClass', en_US: 'No priority classes in this cluster' }}
      actionConfig={{ resourceKind: 'PriorityClass', getName: (record) => record.name }}
    />
  )
}

export function ConfigurationRuntimeClassesPage() {
  return (
    <ResourceListPage<RuntimeClassResource>
      title={{ zh_CN: 'RuntimeClasses', en_US: 'RuntimeClasses' }}
      resourcePath="configuration/runtimeclasses"
      columns={runtimeClassColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 RuntimeClass', en_US: 'No runtime classes in this cluster' }}
      actionConfig={{ resourceKind: 'RuntimeClass', getName: (record) => record.name }}
    />
  )
}

export function ConfigurationLeasesPage() {
  return (
    <ResourceListPage<LeaseResource>
      title={{ zh_CN: 'Leases', en_US: 'Leases' }}
      resourcePath="configuration/leases"
      columns={leaseColumns}
      rowKey={(record) => `${record.namespace}/${record.name}`}
      emptyDescription={{ zh_CN: '当前范围没有 Lease', en_US: 'No leases in the current scope' }}
      actionConfig={{
        resourceKind: 'Lease',
        getName: (record) => record.name,
        getNamespace: (record) => record.namespace,
      }}
    />
  )
}

export function ConfigurationMutatingWebhooksPage() {
  return (
    <ResourceListPage<MutatingWebhookConfigurationResource>
      title={{ zh_CN: 'MutatingWebhookConfigurations', en_US: 'MutatingWebhookConfigurations' }}
      resourcePath="configuration/mutatingwebhookconfigurations"
      columns={mutatingWebhookColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 MutatingWebhookConfiguration', en_US: 'No mutating webhook configurations in this cluster' }}
      actionConfig={{ resourceKind: 'MutatingWebhookConfiguration', getName: (record) => record.name }}
    />
  )
}

export function ConfigurationValidatingWebhooksPage() {
  return (
    <ResourceListPage<ValidatingWebhookConfigurationResource>
      title={{ zh_CN: 'ValidatingWebhookConfigurations', en_US: 'ValidatingWebhookConfigurations' }}
      resourcePath="configuration/validatingwebhookconfigurations"
      columns={validatingWebhookColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 ValidatingWebhookConfiguration', en_US: 'No validating webhook configurations in this cluster' }}
      actionConfig={{ resourceKind: 'ValidatingWebhookConfiguration', getName: (record) => record.name }}
    />
  )
}

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

export function NetworkIngressClassesPage() {
  return (
    <ResourceListPage<IngressClassResource>
      title={{ zh_CN: 'IngressClasses', en_US: 'IngressClasses' }}
      resourcePath="network/ingressclasses"
      columns={ingressClassColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 IngressClass', en_US: 'No ingress classes in this cluster' }}
      actionConfig={{ resourceKind: 'IngressClass', getName: (record) => record.name }}
    />
  )
}

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

export function NetworkPortForwardPage() {
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [modalVisible, setModalVisible] = useState(false)
  const [form, setForm] = useState<{ targetKind: string; targetName: string; namespace: string; localPort: number; remotePort: number }>({
    targetKind: 'Pod',
    targetName: '',
    namespace: namespace || 'default',
    localPort: 8080,
    remotePort: 80,
  })

  const listKey = ['port-forwards', clusterId]
  const query = useQuery({
    queryKey: listKey,
    queryFn: () => api.get<ApiResponse<PortForwardSession[]>>(`/clusters/${clusterId}/network/port-forwards`),
    enabled: !!clusterId,
  })

  const registerMutation = useMutation({
    mutationFn: (payload: typeof form) =>
      api.post<ApiResponse<PortForwardSession>>(`/clusters/${clusterId}/network/port-forwards`, payload),
    onSuccess: () => {
      setModalVisible(false)
      void message.success(localeCode === 'zh_CN' ? '已登记 Port Forward' : 'Port forward registered')
      queryClient.invalidateQueries({ queryKey: listKey })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const stopMutation = useMutation({
    mutationFn: (sessionId: string) =>
      api.delete(`/clusters/${clusterId}/network/port-forwards/${sessionId}`),
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? '已停止' : 'Stopped')
      queryClient.invalidateQueries({ queryKey: listKey })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const columns: TableColumnsType<PortForwardSession> = [
    { title: localeCode === 'zh_CN' ? '会话' : 'Session', dataIndex: 'sessionId', render: (value: string) => <Text code>{value.slice(0, 8)}</Text> },
    { title: 'Namespace', dataIndex: 'namespace' },
    { title: localeCode === 'zh_CN' ? '目标' : 'Target', dataIndex: 'targetName', render: (_: unknown, record: PortForwardSession) => `${record.targetKind}/${record.targetName}` },
    { title: localeCode === 'zh_CN' ? '本地端口' : 'Local', dataIndex: 'localPort' },
    { title: localeCode === 'zh_CN' ? '远端端口' : 'Remote', dataIndex: 'remotePort' },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status', render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{value}</Tag> },
    { title: localeCode === 'zh_CN' ? '创建时间' : 'Created', dataIndex: 'createdAt', render: (value: string) => formatDateTime(value) },
    {
      title: localeCode === 'zh_CN' ? '操作' : 'Actions',
      dataIndex: 'sessionId',
      width: 96,
      render: (value: string) => (
        <Button
          size="small"
          type="text"
          danger
          icon={<DeleteOutlined />}
          loading={stopMutation.isPending}
          onClick={() => stopMutation.mutate(value)}
        >
          {localeCode === 'zh_CN' ? '停止' : 'Stop'}
        </Button>
      ),
    },
  ]

  if (!clusterId) {
    return (
      <div className="kc-page">
        <PageHeader
          title={localeCode === 'zh_CN' ? 'Port Forward' : 'Port Forward'}
          description={localeCode === 'zh_CN' ? '查看当前已登记的 Port Forward 会话。' : 'Inspect registered port forward sessions.'}
        />
        <PlatformScopeToolbar />
        <Empty description={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'} />
      </div>
    )
  }

  return (
    <div className="kc-page">
      <PageHeader
        title={localeCode === 'zh_CN' ? 'Port Forward' : 'Port Forward'}
        description={localeCode === 'zh_CN' ? '仅登记转发会话，不执行实际端口转发。' : 'Registers forward sessions as records without performing real forwarding.'}
        actions={(
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalVisible(true)}>
            {localeCode === 'zh_CN' ? '新建 Port Forward' : 'New Port Forward'}
          </Button>
        )}
      />
      <PlatformScopeToolbar />
      <AdminTable
        columns={columns}
        dataSource={query.data?.data ?? []}
        rowKey="sessionId"
        loading={query.isLoading}
        pageSize={10}
        enableColumnSelection={false}
        empty={<Empty description={localeCode === 'zh_CN' ? '当前集群没有登记的 Port Forward' : 'No port forward sessions registered'} />}
      />
      <Modal
        title={localeCode === 'zh_CN' ? '新建 Port Forward' : 'New Port Forward'}
        open={modalVisible}
        onOk={() => registerMutation.mutate(form)}
        onCancel={() => setModalVisible(false)}
        confirmLoading={registerMutation.isPending}
      >
        <Space vertical align="start" style={{ width: '100%' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '目标类型' : 'Target kind'}</Text>
            <Select
              value={form.targetKind}
              onChange={(value) => setForm((prev) => ({ ...prev, targetKind: String(value) }))}
              style={{ flex: 1 }}
              options={[
                { value: 'Pod', label: 'Pod' },
                { value: 'Service', label: 'Service' },
              ]}
            />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>Namespace</Text>
            <Input value={form.namespace} onChange={(event) => setForm((prev) => ({ ...prev, namespace: event.target.value }))} style={{ flex: 1 }} />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '目标名称' : 'Target name'}</Text>
            <Input value={form.targetName} onChange={(event) => setForm((prev) => ({ ...prev, targetName: event.target.value }))} style={{ flex: 1 }} />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '本地端口' : 'Local port'}</Text>
            <InputNumber value={form.localPort} min={1} max={65535} onChange={(v) => setForm((prev) => ({ ...prev, localPort: Number(v) || 0 }))} style={{ flex: 1 }} />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '远端端口' : 'Remote port'}</Text>
            <InputNumber value={form.remotePort} min={1} max={65535} onChange={(v) => setForm((prev) => ({ ...prev, remotePort: Number(v) || 0 }))} style={{ flex: 1 }} />
          </div>
        </Space>
      </Modal>
    </div>
  )
}

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

export function PlatformAccessControlClusterRolesPage() {
  return (
    <ResourceListPage<ClusterRoleResource>
      title={{ zh_CN: 'ClusterRoles', en_US: 'ClusterRoles' }}
      resourcePath="access-control/clusterroles"
      columns={clusterRoleColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 ClusterRole', en_US: 'No cluster roles in this cluster' }}
      actionConfig={{ resourceKind: 'ClusterRole', getName: (record) => record.name }}
    />
  )
}

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

export function PlatformAccessControlClusterRoleBindingsPage() {
  return (
    <ResourceListPage<ClusterRoleBindingResource>
      title={{ zh_CN: 'ClusterRoleBindings', en_US: 'ClusterRoleBindings' }}
      resourcePath="access-control/clusterrolebindings"
      columns={clusterRoleBindingColumns}
      rowKey="name"
      emptyDescription={{ zh_CN: '当前集群没有 ClusterRoleBinding', en_US: 'No cluster role bindings in this cluster' }}
      actionConfig={{ resourceKind: 'ClusterRoleBinding', getName: (record) => record.name }}
    />
  )
}

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
