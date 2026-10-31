import {
  ResourceDetailHeader,
  ResourceDetailTabs,
  useResourceDetailTab,
} from '@/features/platform/shared/resource-detail-header'
import { ResourceMonitoringLink } from '../resource-monitoring'
import { AdminTable } from '@/components/admin-table'
import { ManagementDetailHeader, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useAIPageContext } from '@/features/copilot'
import {
  NodeResourcePanel,
  parseStringMap,
  parseTaints,
  stringifyMap,
  stringifyTaints,
} from '@/features/platform/node-resource-utils'
import { PlatformResourceOverview } from '@/features/platform/shared/resource-overview'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import '@/features/platform/styles/base.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Card, Form, Input, Space, Spin, Tag, Typography } from 'antd'
import { lazy, Suspense, useEffect, useMemo } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useClusterCapabilityForCluster } from '../cluster-capabilities'
import { nodeMutations } from './mutations'
import { nodeQueries } from './queries'
import { toClusterScope } from './scope'
import type { NodeCondition, NodePod } from './types'

const { Text } = Typography

const NodeYAMLPanel = lazy(() => import('./node-yaml-panel'))

function buildPodDetailPath(name: string, namespace: string, clusterId?: string | null) {
  const params = new URLSearchParams()
  if (namespace) params.set('namespace', namespace)
  if (clusterId) params.set('clusterId', clusterId)
  const query = params.toString()
  const path = `/workloads/pods/${encodeURIComponent(name)}`
  return query ? `${path}?${query}` : path
}

export function NodeDetailPage() {
  const { localeCode, t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { nodeName = '' } = useParams()
  const [searchParams] = useSearchParams()
  const { clusterId: scopedClusterId, setClusterId } = usePlatformScopeStore()
  const requestedClusterId = searchParams.get('clusterId')
  const clusterId = requestedClusterId || scopedClusterId
  const scope = toClusterScope(clusterId)
  const [activeTab, setActiveTab] = useResourceDetailTab()
  const nodeDetailQuery = useQuery(nodeQueries.detail(scope, nodeName))
  const visibleNodeDetailQueryData = readableQueryData(nodeDetailQuery)
  const updateNodeMutation = usePlatformMutation(nodeMutations.update(queryClient), {
    label: ['更新节点', 'Update node'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = updateNodeMutation.blocked
  const yamlCapability = useClusterCapabilityForCluster('resource.yaml.view', localeCode, clusterId)
  const yamlAvailable = !yamlCapability.disabled && !yamlCapability.isLoading
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: localeCode === 'zh_CN' ? '节点详情' : 'Node Detail',
    entityKind: 'Node',
    entityName: nodeName,
    clusterId: clusterId || undefined,
    node: nodeName,
  })

  useEffect(() => {
    if (requestedClusterId && requestedClusterId !== scopedClusterId) {
      setClusterId(requestedClusterId)
    }
  }, [requestedClusterId, scopedClusterId, setClusterId])

  const nodeDetail = visibleNodeDetailQueryData
  const nodeFormInitValues = useMemo(() => {
    if (!nodeDetail) return { labels: '{}', taints: '[]' }
    return {
      labels: stringifyMap(nodeDetail.labels),
      taints: stringifyTaints(nodeDetail.taints),
    }
  }, [nodeDetail])

  const podColumns: TableColumnsType<NodePod> = [
    {
      title: 'Pod',
      dataIndex: 'name',
      render: (value: string, record) => (
        <Button
          type="text"
          onClick={() => navigate(buildPodDetailPath(value, record.namespace, clusterId))}
        >
          {value}
        </Button>
      ),
    },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'phase',
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: 'Ready', dataIndex: 'readyContainers' },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restarts' },
    { title: 'CPU', dataIndex: 'cpu', render: (value: string) => value || '-' },
    {
      title: localeCode === 'zh_CN' ? '内存' : 'Memory',
      dataIndex: 'memory',
      render: (value: string) => value || '-',
    },
    { title: 'IP', dataIndex: 'podIp', render: (value: string) => value || '-' },
    {
      title: localeCode === 'zh_CN' ? '存活时长' : 'Age',
      dataIndex: 'ageSeconds',
      render: (value: number) => formatAgeSeconds(value),
    },
  ]

  const conditionColumns: TableColumnsType<NodeCondition> = [
    { title: localeCode === 'zh_CN' ? '类型' : 'Type', dataIndex: 'type' },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '原因' : 'Reason',
      dataIndex: 'reason',
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '消息' : 'Message',
      dataIndex: 'message',
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '最近变更' : 'Last Transition',
      dataIndex: 'lastTransitionTime',
      render: (value: string) => (value ? formatDateTime(value) : '-'),
    },
  ]

  if (!clusterId) {
    return (
      <div className="soha-page">
        <ManagementDetailHeader
          title={localeCode === 'zh_CN' ? '节点详情' : 'Node Detail'}
          description={
            localeCode === 'zh_CN'
              ? '需要先选定集群，才能查看独立节点详情。'
              : 'Select a cluster before opening a standalone node detail page.'
          }
        />
        <ManagementState
          compact
          kind="select-scope"
          title={t('common.pleaseSelectCluster', 'Please select a cluster')}
        />
      </div>
    )
  }

  if (nodeDetailQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  }

  if (!nodeDetail || !nodeName) {
    return (
      <div className="soha-page">
        <ManagementDetailHeader
          title={localeCode === 'zh_CN' ? '节点详情' : 'Node Detail'}
          description={
            localeCode === 'zh_CN'
              ? '当前节点不存在或详情不可用。'
              : 'The node was not found or its detail is unavailable.'
          }
          actions={
            <Button onClick={() => navigate('/cluster-resources/nodes')}>
              {t('common.back', 'Back')}
            </Button>
          }
        />
        <Card>
          <Text type="secondary">{t('common.notFound', 'Not found')}</Text>
        </Card>
      </div>
    )
  }

  const updateNode = (values: Record<string, unknown>) =>
    updateNodeMutation.mutate(
      {
        scope,
        name: nodeName,
        input: {
          labels: parseStringMap(values.labels, 'Labels'),
          taints: parseTaints(values.taints),
        },
      },
      {},
    )

  const namespaceCount = new Set((nodeDetail.pods ?? []).map((pod) => pod.namespace)).size

  return (
    <div className="soha-page soha-workload-detail-page soha-k8s-detail-page">
      <ResourceDetailHeader
        kind="Node"
        name={nodeDetail.name}
        status={nodeDetail.status}
        ageSeconds={nodeDetail.ageSeconds}
        queryKey={nodeQueries.detail(scope, nodeName).queryKey}
      />
      <ReadFeedback canRead={nodeDetailQuery.isEnabled} query={nodeDetailQuery} label="节点详情" />
      <ResourceMonitoringLink clusterId={clusterId || ''} nodeName={nodeName} />

      {updateNodeMutation.feedback}

      <ResourceDetailTabs
        className="soha-resource-tabs soha-workload-detail-tabs"
        activeKey={activeTab}
        destroyOnHidden
        onChange={setActiveTab}
        items={[
          {
            key: 'overview',
            label: t('common.overview', 'Overview'),
            children: (
              <div className="soha-detail-stack">
                <PlatformResourceOverview
                  compact
                  name={nodeDetail.name}
                  ageSeconds={nodeDetail.ageSeconds}
                  labels={nodeDetail.labels}
                  annotations={nodeDetail.annotations}
                  facts={[
                    {
                      key: 'status',
                      label: t('common.status', 'Status'),
                      value: <StatusTag value={nodeDetail.status || 'unknown'} />,
                    },
                    {
                      key: 'version',
                      label: localeCode === 'zh_CN' ? '版本' : 'Version',
                      value: nodeDetail.version || '-',
                    },
                    { key: 'ip', label: 'IP', value: nodeDetail.internalIp || '-' },
                    {
                      key: 'pods',
                      label: localeCode === 'zh_CN' ? 'Pod 数量' : 'Pods',
                      value: nodeDetail.podCount,
                    },
                    {
                      key: 'namespaces',
                      label: localeCode === 'zh_CN' ? '承载命名空间' : 'Namespaces',
                      value: namespaceCount,
                    },
                    {
                      key: 'roles',
                      label: localeCode === 'zh_CN' ? '节点角色' : 'Roles',
                      value: nodeDetail.roles?.length ? (
                        <Space size={[4, 4]} wrap>
                          {nodeDetail.roles.map((role) => (
                            <Tag key={role}>{role}</Tag>
                          ))}
                        </Space>
                      ) : (
                        '-'
                      ),
                    },
                  ]}
                />

                <div className="grid gap-4 xl:grid-cols-2">
                  <Card
                    className="soha-detail-card"
                    title={localeCode === 'zh_CN' ? '资源分配' : 'Resource Allocation'}
                  >
                    <NodeResourcePanel node={nodeDetail} />
                    {nodeDetail.metricsMessage ? (
                      <div className="soha-detail-meta">
                        <Text type="secondary">{nodeDetail.metricsMessage}</Text>
                      </div>
                    ) : null}
                  </Card>

                  <Card
                    className="soha-detail-card"
                    title={localeCode === 'zh_CN' ? '标签与污点' : 'Labels & Taints'}
                  >
                    <div className="soha-detail-meta mb-4">
                      {nodeDetail.taints?.length ? (
                        <div className="soha-tag-list">
                          {nodeDetail.taints.map((item) => (
                            <Tag key={`${item.key}:${item.effect}:${item.value || ''}`}>
                              {item.key}
                              {item.value ? `=${item.value}` : ''}:{item.effect}
                            </Tag>
                          ))}
                        </div>
                      ) : (
                        <Text type="secondary" className="text-xs">
                          {localeCode === 'zh_CN'
                            ? '当前节点没有污点'
                            : 'No taints configured on this node'}
                        </Text>
                      )}
                    </div>
                    <Form
                      key={`node-edit:${clusterId}:${nodeName}:${nodeDetailQuery.dataUpdatedAt}`}
                      layout="vertical"
                      initialValues={nodeFormInitValues}
                      onFinish={updateNode}
                      disabled={platformWriteBlocked || Boolean(nodeDetailQuery.error)}
                    >
                      <div className="grid gap-4 xl:grid-cols-2">
                        <Form.Item name="labels" label="Labels(JSON)">
                          <Input.TextArea rows={8} />
                        </Form.Item>
                        <Form.Item name="taints" label="Taints(JSON Array)">
                          <Input.TextArea rows={8} />
                        </Form.Item>
                      </div>
                      <div className="soha-form-actions">
                        <Button onClick={() => void nodeDetailQuery.refetch()}>
                          {t('common.refresh', 'Refresh')}
                        </Button>
                        <Button
                          htmlType="submit"
                          type="primary"
                          loading={updateNodeMutation.isPending}
                        >
                          {t('common.save', 'Save')}
                        </Button>
                      </div>
                    </Form>
                  </Card>
                </div>
              </div>
            ),
          },
          {
            key: 'pods',
            label: `Pods (${nodeDetail.pods?.length ?? 0})`,
            children: (
              <Card
                className="soha-detail-card"
                title={
                  localeCode === 'zh_CN'
                    ? `承载 Pods (${nodeDetail.pods?.length ?? 0})`
                    : `Scheduled Pods (${nodeDetail.pods?.length ?? 0})`
                }
              >
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columns={podColumns}
                  dataSource={nodeDetail.pods ?? []}
                  rowKey={(record) => `${record.namespace}/${record.name}`}
                  pagination={false}
                  virtual
                  scroll={{ x: 1200, y: 520 }}
                  enableColumnSelection={false}
                />
              </Card>
            ),
          },
          {
            key: 'conditions',
            label: localeCode === 'zh_CN' ? '运行条件' : 'Conditions',
            children: (
              <Card
                className="soha-detail-card"
                title={localeCode === 'zh_CN' ? '节点 Conditions' : 'Node Conditions'}
              >
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columns={conditionColumns}
                  dataSource={nodeDetail.conditions ?? []}
                  rowKey={(record) => `${record.type}:${record.lastTransitionTime || ''}`}
                  pagination={false}
                  scroll={{ x: 900, y: 520 }}
                  enableColumnSelection={false}
                />
              </Card>
            ),
          },
          {
            key: 'yaml',
            label: t('common.yaml', 'YAML'),
            children:
              activeTab === 'yaml' ? (
                yamlCapability.isLoading ? (
                  <Card className="soha-detail-card">
                    <Spin size="large" />
                  </Card>
                ) : yamlAvailable ? (
                  <Suspense
                    fallback={
                      <Card className="soha-detail-card">
                        <Spin size="large" />
                      </Card>
                    }
                  >
                    <NodeYAMLPanel scope={scope} nodeName={nodeName} />
                  </Suspense>
                ) : (
                  <ManagementState
                    compact
                    kind="unsupported"
                    title={localeCode === 'zh_CN' ? '节点 YAML 暂不可用' : 'Node YAML unavailable'}
                    description={yamlCapability.reason}
                  />
                )
              ) : null,
          },
        ]}
      />
    </div>
  )
}
