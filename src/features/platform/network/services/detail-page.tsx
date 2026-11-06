import { WorkloadRelationships } from '@/features/platform/shared/resource-insights/workload-relationships'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  ResourceDetailHeader,
  useResourceDetailTab,
} from '@/features/platform/shared/resource-detail-header'
import { ResourceGitOpsStatus } from '@/features/platform/shared/resource-gitops-status'
import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import { WorkloadRefresh, useWorkloadRefresh } from '../../workloads/shared/workload-refresh'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType, TabsProps } from 'antd'
import { Alert, Button, Card, Typography } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useNetworkDetailScope } from '../shared/detail-scope'
import { NetworkDetailShell } from '../shared/detail-shell'
import { networkMutations } from '../shared/mutations'
import { networkQueries } from '../shared/queries'
import { ServiceDiagnosticsPanel } from './diagnostics-panel'
import { serviceConfiguration } from './detail-model'
import { ServicePortDisplay, ServiceTypeTag } from './port-display'
import { serviceQueries } from './queries'
import { ServiceEditModal } from './service-edit-modal'
import { ServiceSelectorDisplay } from './selector-display'
import type { Service, ServiceBackendPod, ServiceEndpoint } from './types'
import './styles.css'

const tabs = ['overview', 'backends', 'diagnostics', 'relationships', 'yaml']

export function ServiceDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const navigate = useNavigate(),
    client = useQueryClient()
  const { serviceName = '' } = useParams()
  const scope = useNetworkDetailScope()
  const { clusterId, namespace: detailNamespace } = scope
  const [tab, setTab] = useResourceDetailTab()
  const activeTab = tabs.includes(tab) ? tab : 'overview'
  useEffect(() => {
    if (!tabs.includes(tab)) setTab('overview')
  }, [tab, setTab])
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(permissions, 'platform.network.services.view')
  const canReadPods = hasPermission(permissions, 'platform.pods.view')
  const canReadNodes = hasPermission(permissions, 'platform.nodes.view')
  const refresh = useWorkloadRefresh()
  const detailOptions = serviceQueries.detail(scope, serviceName)
  const detailQuery = useQuery({
    ...detailOptions,
    enabled: Boolean(detailOptions.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const returned = canRead ? readableQueryData(detailQuery) : undefined
  const service =
    returned?.name === serviceName && returned.namespace === detailNamespace ? returned : undefined
  const yamlOptions = networkQueries.yaml('services', scope, serviceName)
  const configurationQuery = useQuery({
    ...yamlOptions,
    enabled: Boolean(yamlOptions.enabled) && canRead && activeTab === 'overview',
    refetchInterval: refresh.interval,
  })
  const configurationData = readableQueryData(configurationQuery)
  const configuration = useMemo(() => {
    const yaml = configurationData
    if (!yaml) return {}
    try {
      return { data: serviceConfiguration(yaml.content, serviceName, detailNamespace || '') }
    } catch (error) {
      return { error: error instanceof Error ? error.message : String(error) }
    }
  }, [configurationData, serviceName, detailNamespace])
  const hasSelector = Boolean(service?.selector && Object.keys(service.selector).length)
  const [editingIdentity, setEditingIdentity] = useState<string>()
  const identity = JSON.stringify([clusterId, detailNamespace, serviceName])
  const remove = usePlatformMutation(networkMutations.remove('services', client), {
    label: ['删除 Service', 'Delete Service'],
    target: platformResourceTarget,
  })
  const canUpdate =
    canRead &&
    hasPermission(permissions, 'platform.network.services.update') &&
    hasAllowedAction(service?.allowedActions, 'update')
  const canDelete =
    canRead &&
    hasPermission(permissions, 'platform.network.services.delete') &&
    hasAllowedAction(service?.allowedActions, 'delete')
  const actions = useKubernetesResourceActions<Service>({
    resourceKey: 'service-detail',
    resourceLabel: 'Service',
    records: service ? [service] : [],
    getName: (r) => r.name,
    getKey: (r) => `${r.namespace}/${r.name}`,
    scopeKey: identity,
    actions: (record) =>
      canDelete
        ? [
            {
              key: 'delete',
              label: zh ? '删除' : 'Delete',
              danger: true,
              disabled: remove.blocked,
              confirm: kubernetesDeleteConfirmation(
                record,
                localeCode,
                zh
                  ? '删除此 Service 会移除其访问入口，后端 Pod 不会被删除。'
                  : 'Deleting this Service removes its access entry; backend Pods remain.',
              ),
              prepare: () => remove.reset(),
              feedback: remove.feedback,
              run: async () => {
                await remove.mutateAsync({ scope, name: record.name })
                navigate(
                  `/network/services?${new URLSearchParams({ clusterId: clusterId || '', namespace: record.namespace })}`,
                )
              },
            },
          ]
        : [],
  })
  const activeQueries = [
    detailQuery,
    ...(configurationQuery.isEnabled || activeTab === 'yaml'
      ? [{ ...configurationQuery, isEnabled: true }]
      : []),
  ]
  const readFeedback = (
    <>
      <ReadFeedback canRead={canRead} query={detailQuery} label="Service" />
      {activeTab === 'overview' && (
        <ReadFeedback
          canRead={configurationQuery.isEnabled}
          query={configurationQuery}
          label={zh ? '网络配置' : 'Network configuration'}
        />
      )}
    </>
  )
  const denied = (description: string) => (
    <ManagementState compact kind="unsupported" description={description} />
  )
  const unknownData = (description: string) => (
    <Alert
      showIcon
      type="info"
      title={zh ? '后端数据未确认' : 'Backend data not confirmed'}
      description={description}
    />
  )
  const podPath = (name: string, ns = detailNamespace || '') =>
    `/workloads/pods/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId: clusterId || '', namespace: ns })}`
  const nodeName = (name?: string) =>
    name ? (
      canReadNodes ? (
        <Link
          to={`/cluster-resources/nodes/${encodeURIComponent(name)}?clusterId=${encodeURIComponent(clusterId || '')}`}
        >
          {name}
        </Link>
      ) : (
        name
      )
    ) : (
      '—'
    )
  const podName = (name: string, ns: string) =>
    canReadPods ? <Link to={podPath(name, ns)}>{name}</Link> : name
  const backendPodColumns: TableColumnsType<ServiceBackendPod> = [
    {
      title: 'Pod',
      dataIndex: 'name',
      width: 270,
      render: (name: string, r) => podName(name, r.namespace),
    },
    {
      title: zh ? '状态' : 'Status',
      dataIndex: 'phase',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: zh ? '就绪' : 'Ready', dataIndex: 'readyContainers', width: 90 },
    { title: zh ? '节点' : 'Node', dataIndex: 'nodeName', width: 210, render: nodeName },
    { title: 'Pod IP', dataIndex: 'podIp', width: 150, render: (v?: string) => v || '—' },
    { title: zh ? '重启' : 'Restarts', dataIndex: 'restarts', width: 80 },
    { title: zh ? '时长' : 'Age', dataIndex: 'ageSeconds', width: 100, render: formatAgeSeconds },
  ]
  const condition = (value?: boolean) => (value === undefined ? '—' : <BooleanTag value={value} />)
  const endpointColumns: TableColumnsType<ServiceEndpoint> = [
    { title: zh ? '地址' : 'Address', dataIndex: 'address', width: 200 },
    ...['ready', 'serving', 'terminating'].map((key) => ({
      title: key[0].toUpperCase() + key.slice(1),
      dataIndex: key,
      width: 110,
      render: condition,
    })),
    {
      title: zh ? '目标' : 'Target',
      dataIndex: 'targetRef',
      width: 270,
      render: (target?: string) => target || '—',
    },
    { title: zh ? '节点' : 'Node', dataIndex: 'nodeName', width: 210, render: nodeName },
    {
      title: zh ? '区域' : 'Zone',
      dataIndex: 'zone',
      width: 120,
      render: (value?: string) => value || '—',
    },
  ]
  const pods = service?.backendPods,
    endpoints = service?.endpoints
  const podTable = (preview = false) =>
    pods === undefined ? (
      unknownData(
        zh
          ? '未返回匹配的 Pod 数据，可能为空或当前不可读取。'
          : 'Matching Pod data was not returned; it may be empty or currently inaccessible.',
      )
    ) : (
      <AdminTable
        columns={
          preview
            ? backendPodColumns.filter((column) =>
                ['name', 'phase', 'nodeName', 'restarts'].includes(
                  String('dataIndex' in column ? column.dataIndex : ''),
                ),
              )
            : backendPodColumns
        }
        dataSource={preview ? pods.slice(0, 3) : pods}
        rowKey={(r) => `${r.namespace}/${r.name}`}
        pagination={false}
        scroll={{ x: preview ? 670 : 1010, y: preview ? undefined : 520 }}
        tableSize="small"
        enableColumnSelection={false}
      />
    )
  if (!clusterId || !detailNamespace)
    return (
      <ManagementState
        kind="select-scope"
        description={zh ? '请选择集群和命名空间' : 'Select a cluster and namespace'}
      />
    )
  if (detailQuery.isLoading) return <Card loading />
  if (!service)
    return (
      <>
        {readFeedback}
        {!canRead
          ? denied(zh ? '需要 Service 查看权限' : 'Service view permission required')
          : !detailQuery.isError && (
              <ManagementState
                kind="not-found"
                description={zh ? '未找到 Service' : 'Service not found'}
              />
            )}
      </>
    )
  const config = configuration.data
  const address = (values?: string[]) =>
    values?.length
      ? values.map((v) => (
          <Typography.Paragraph copyable key={v}>
            {v}
          </Typography.Paragraph>
        ))
      : '—'
  const overview = (
    <WorkloadOverview
      aside={
        <>
          <Card title={zh ? '网络配置' : 'Network configuration'} className="soha-detail-card">
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '选择器' : 'Selector',
                  children: <ServiceSelectorDisplay selector={service.selector} />,
                },
                {
                  label: zh ? '地址族' : 'IP families',
                  children: config?.ipFamilies.join(', ') || '—',
                },
                {
                  label: zh ? 'IP 分配策略' : 'IP family policy',
                  children: config?.ipFamilyPolicy || '—',
                },
                {
                  label: zh ? '内部流量策略' : 'Internal traffic',
                  children: config?.internalTrafficPolicy || '—',
                },
                {
                  label: zh ? '外部流量策略' : 'External traffic',
                  children: config?.externalTrafficPolicy || '—',
                },
                {
                  label: zh ? '会话保持' : 'Session affinity',
                  children: config?.sessionAffinity || '—',
                },
                {
                  label: zh ? '发布未就绪端点' : 'Publish not ready',
                  children: condition(config?.publishNotReadyAddresses),
                },
              ]}
            />
          </Card>
          <ResourceGitOpsStatus
            clusterId={clusterId}
            kind="Service"
            name={service.name}
            namespace={service.namespace}
          />
        </>
      }
    >
      <Card title={zh ? '访问地址' : 'Addresses'} className="soha-detail-card">
        {configuration.error && <Alert showIcon type="warning" title={configuration.error} />}
        <WorkloadOverviewConfiguration
          items={[
            { label: zh ? '类型' : 'Type', children: <ServiceTypeTag type={service.type} /> },
            ...(service.type === 'ExternalName'
              ? [{ label: 'External name', children: config?.externalName || '—' }]
              : [
                  {
                    label: 'Cluster IP',
                    children: config?.headless
                      ? 'Headless · None'
                      : address(
                          config?.clusterIPs.length
                            ? config.clusterIPs
                            : service.clusterIp
                              ? [service.clusterIp]
                              : [],
                        ),
                  },
                ]),
            ...(service.type === 'LoadBalancer'
              ? [{ label: 'LoadBalancer', children: address(config?.loadBalancerAddresses) }]
              : []),
            ...(config?.externalIPs.length
              ? [{ label: 'External IPs', children: address(config.externalIPs) }]
              : []),
          ]}
        />
      </Card>
      <Card
        title={zh ? '端口映射' : 'Port mappings'}
        className="soha-detail-card"
        extra={<Typography.Text type="secondary">Service → TargetPort</Typography.Text>}
      >
        <ServicePortDisplay ports={service.ports} portMappings={service.portMappings} detailed />
      </Card>
      <Card
        title={
          zh
            ? `后端 Pods${pods ? ` · ${pods.length}` : ''}`
            : `Backend Pods${pods ? ` · ${pods.length}` : ''}`
        }
        className="soha-detail-card"
        extra={
          <Button type="link" onClick={() => setTab('backends')}>
            {zh ? '查看全部后端' : 'View all backends'}
          </Button>
        }
      >
        {!hasSelector && (
          <Typography.Paragraph type="secondary">
            {zh
              ? '此 Service 没有选择器，端点可由外部管理；不推测 Pod 归属。'
              : 'This Service has no selector. Endpoints may be externally managed; Pod ownership is not inferred.'}
          </Typography.Paragraph>
        )}
        {podTable(true)}
      </Card>
      <WorkloadOverviewMetadata labels={service.labels} annotations={service.annotations} />
    </WorkloadOverview>
  )
  const extraTabs: NonNullable<TabsProps['items']> = [
    {
      key: 'backends',
      label: zh ? '后端' : 'Backends',
      children: (
        <div className="soha-detail-stack">
          <Card
            title={`Endpoints${endpoints ? ` · ${endpoints.length}` : ''}`}
            className="soha-detail-card"
            extra={
              hasPermission(permissions, 'platform.network.endpoint-slices.view') ? (
                <Link
                  to={`/network/endpointslices?${new URLSearchParams({ clusterId, namespace: detailNamespace })}`}
                >
                  EndpointSlices
                </Link>
              ) : null
            }
          >
            {endpoints === undefined ? (
              unknownData(
                zh
                  ? '未返回端点数据，可能为空或当前不可读取。'
                  : 'Endpoint data was not returned; it may be empty or currently inaccessible.',
              )
            ) : (
              <AdminTable
                columns={endpointColumns}
                dataSource={endpoints}
                rowKey={(r) => `${r.address}/${r.targetRef || ''}`}
                pagination={false}
                scroll={{ x: 1230, y: 520 }}
                tableSize="small"
                enableColumnSelection={false}
              />
            )}
          </Card>
          <Card title={zh ? '匹配的 Pods' : 'Matching Pods'} className="soha-detail-card">
            {podTable()}
          </Card>
        </div>
      ),
    },
    {
      key: 'diagnostics',
      label: zh ? '诊断' : 'Diagnostics',
      children:
        activeTab === 'diagnostics' ? (
          <Card title={zh ? '连通性诊断' : 'Connectivity diagnostics'} className="soha-detail-card">
            <ServiceDiagnosticsPanel
              key={identity}
              clusterId={clusterId}
              namespace={detailNamespace}
              portMappings={service.portMappings}
              serviceName={service.name}
            />
          </Card>
        ) : null,
    },
  ]
  return (
    <div className="soha-service-detail">
      {readFeedback}
      {actions.confirmation}
      {!actions.isConfirming && remove.feedback}
      <NetworkDetailShell
        activeTabKey={activeTab}
        detail={service}
        target={{ scope, name: serviceName }}
        kind="services"
        label="Service"
        onTabChange={setTab}
        extraTabs={extraTabs}
        tabOrder={tabs}
        preserveYaml
        canUpdateYAML={canUpdate}
        overview={overview}
        header={
          <ResourceDetailHeader
            kind="Service"
            name={service.name}
            namespace={service.namespace}
            ageSeconds={service.ageSeconds}
            facts={<ServiceTypeTag type={service.type} />}
            actions={
              <>
                <WorkloadRefresh
                  settings={refresh}
                  queries={activeQueries}
                  onRefresh={() => Promise.all(activeQueries.map((q) => q.refetch()))}
                  resourceLabel="Service"
                  beforeRefresh={
                    canUpdate && (
                      <Button
                        aria-label={zh ? '编辑 Service' : 'Edit Service'}
                        disabled={remove.blocked}
                        onClick={() => setEditingIdentity(identity)}
                      >
                        {zh ? '编辑' : 'Edit'}
                      </Button>
                    )
                  }
                />
                {canDelete && (
                  <Button
                    aria-label={zh ? '删除 Service' : 'Delete Service'}
                    danger
                    disabled={remove.blocked}
                    onClick={() => actions.selectAction(service, 'delete')}
                  >
                    {zh ? '删除' : 'Delete'}
                  </Button>
                )}
              </>
            }
          />
        }
        relatedResources={
          activeTab === 'relationships' ? (
            <WorkloadRelationships
              key={identity}
              kind="Service"
              scope={scope}
              name={service.name}
              active={activeTab === 'relationships'}
            />
          ) : null
        }
      />
      {canUpdate && editingIdentity === identity && (
        <ServiceEditModal
          key={`edit-${identity}`}
          clusterId={clusterId}
          service={service}
          onClose={() => setEditingIdentity(undefined)}
        />
      )}
    </div>
  )
}
