import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { App, Button, Card, Descriptions, Form, Input, Space, Spin, Tabs, Tag, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { ManagementDetailHeader, ManagementState } from '@/components/management-list'
import { PlatformClusterScopeHint } from '@/components/platform-cluster-scope-hint'
import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import {
  NodeResourcePanel,
  parseStringMap,
  parseTaints,
  stringifyMap,
  stringifyTaints,
} from '@/features/platform/node-resource-utils'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import type { ApiResponse, NodeDetail, NodePod, ResourceYAMLView, WorkloadCondition } from '@/types'
import type { TableColumnsType } from 'antd'
import './platform-pages.css'

const { Text } = Typography

const K8sYamlEditor = lazy(async () => {
  const mod = await import('@/components/k8s-yaml-editor')
  return { default: mod.K8sYamlEditor }
})

function buildPodDetailPath(name: string, namespace: string) {
  const params = new URLSearchParams()
  if (namespace) {
    params.set('namespace', namespace)
  }
  const query = params.toString()
  return query ? `/workloads/pods/${name}?${query}` : `/workloads/pods/${name}`
}

export function NodeDetailPage() {
  const { localeCode, t } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { nodeName } = useParams()
  const [searchParams] = useSearchParams()
  const { clusterId: scopedClusterId, setClusterId } = usePlatformScopeStore()
  const requestedClusterId = searchParams.get('clusterId')
  const clusterId = requestedClusterId || scopedClusterId

  useEffect(() => {
    if (requestedClusterId && requestedClusterId !== scopedClusterId) {
      setClusterId(requestedClusterId)
    }
  }, [requestedClusterId, scopedClusterId, setClusterId])

  const nodeDetailPath = clusterId && nodeName
    ? `/clusters/${clusterId}/infrastructure/nodes/${nodeName}/detail`
    : null
  const nodeYAMLPath = clusterId && nodeName
    ? `/clusters/${clusterId}/infrastructure/nodes/${nodeName}/yaml`
    : null

  const nodeDetailQuery = useQuery({
    queryKey: ['cluster-node-detail-page', clusterId, nodeName],
    queryFn: () => api.get<ApiResponse<NodeDetail>>(nodeDetailPath!),
    enabled: !!nodeDetailPath,
  })

  const nodeYAMLQuery = useQuery({
    queryKey: ['cluster-node-yaml', clusterId, nodeName],
    queryFn: () => api.get<ApiResponse<ResourceYAMLView>>(nodeYAMLPath!),
    enabled: !!nodeYAMLPath,
  })

  const yamlServerValue = nodeYAMLQuery.data?.data?.content ?? ''
  const yamlDraftStorageKey = useMemo(
    () => (clusterId && nodeName ? `kc:yaml-draft:${clusterId}:node:${nodeName}` : ''),
    [clusterId, nodeName],
  )
  const [yamlDraft, setYamlDraft] = useState('')

  useEffect(() => {
    if (!nodeYAMLPath) return
    const draft = yamlDraftStorageKey ? window.localStorage.getItem(yamlDraftStorageKey) : null
    setYamlDraft(draft ?? yamlServerValue)
  }, [nodeYAMLPath, yamlDraftStorageKey, yamlServerValue])

  const updateNodeMutation = useMutation({
    mutationFn: async (values: Record<string, unknown>) => {
      if (!clusterId || !nodeName) return
      return api.put<ApiResponse<NodeDetail>>(`/clusters/${clusterId}/infrastructure/nodes/${nodeName}`, {
        labels: parseStringMap(values.labels, 'Labels'),
        taints: parseTaints(values.taints),
      })
    },
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? '节点配置已更新' : 'Node configuration updated')
      queryClient.invalidateQueries({ queryKey: ['cluster-nodes', clusterId] })
      queryClient.invalidateQueries({ queryKey: ['cluster-node-detail', clusterId, nodeName] })
      queryClient.invalidateQueries({ queryKey: ['cluster-node-detail-page', clusterId, nodeName] })
      queryClient.invalidateQueries({ queryKey: ['cluster-node-yaml', clusterId, nodeName] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const applyYAMLMutation = useMutation({
    mutationFn: () => api.put<ApiResponse<ResourceYAMLView>>(nodeYAMLPath!, { content: yamlDraft }),
    onSuccess: (response) => {
      if (yamlDraftStorageKey) {
        window.localStorage.removeItem(yamlDraftStorageKey)
      }
      setYamlDraft(response.data?.content ?? yamlDraft)
      void message.success(t('yamlEditor.applySuccess', 'YAML applied'))
      queryClient.invalidateQueries({ queryKey: ['cluster-nodes', clusterId] })
      queryClient.invalidateQueries({ queryKey: ['cluster-node-detail', clusterId, nodeName] })
      queryClient.invalidateQueries({ queryKey: ['cluster-node-detail-page', clusterId, nodeName] })
      queryClient.invalidateQueries({ queryKey: ['cluster-node-yaml', clusterId, nodeName] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const nodeDetail = nodeDetailQuery.data?.data
  const nodeFormInitValues = useMemo(() => {
    if (!nodeDetail) {
      return { labels: '{}', taints: '[]' }
    }
    return {
      labels: stringifyMap(nodeDetail.labels),
      taints: stringifyTaints(nodeDetail.taints),
    }
  }, [nodeDetail])

  const podColumns: TableColumnsType<NodePod> = [
    {
      title: 'Pod',
      dataIndex: 'name',
      render: (value: string, record: NodePod) => (
        <Button type="text" onClick={() => navigate(buildPodDetailPath(value, record.namespace))}>
          {value}
        </Button>
      ),
    },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'phase', render: (value: string) => <StatusTag value={value} /> },
    { title: 'Ready', dataIndex: 'readyContainers' },
    { title: localeCode === 'zh_CN' ? '重启次数' : 'Restarts', dataIndex: 'restarts' },
    { title: 'CPU', dataIndex: 'cpu', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '内存' : 'Memory', dataIndex: 'memory', render: (value: string) => value || '-' },
    { title: 'IP', dataIndex: 'podIp', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '存活时长' : 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  const conditionColumns: TableColumnsType<WorkloadCondition> = [
    { title: localeCode === 'zh_CN' ? '类型' : 'Type', dataIndex: 'type' },
    { title: localeCode === 'zh_CN' ? '状态' : 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    { title: localeCode === 'zh_CN' ? '原因' : 'Reason', dataIndex: 'reason', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '消息' : 'Message', dataIndex: 'message', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '最近变更' : 'Last Transition', dataIndex: 'lastTransitionTime', render: (value: string) => value ? formatDateTime(value) : '-' },
  ]

  if (!clusterId) {
    return (
      <div className="soha-page">
        <ManagementDetailHeader
          title={localeCode === 'zh_CN' ? '节点详情' : 'Node Detail'}
          description={localeCode === 'zh_CN' ? '需要先选定集群，才能查看独立节点详情。' : 'Select a cluster before opening a standalone node detail page.'}
        />
        <ManagementState compact kind="select-scope" title={t('common.pleaseSelectCluster', 'Please select a cluster')} />
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
          description={localeCode === 'zh_CN' ? '当前节点不存在或详情不可用。' : 'The node was not found or its detail is unavailable.'}
          actions={<Button onClick={() => navigate('/cluster-resources/nodes')}>{t('common.back', 'Back')}</Button>}
        />
        <Card>
          <Text type="secondary">{t('common.notFound', 'Not found')}</Text>
        </Card>
      </div>
    )
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title={`${localeCode === 'zh_CN' ? '节点详情' : 'Node Detail'}: ${nodeDetail.name}`}
        description={localeCode === 'zh_CN' ? '查看节点资源分配、污点、YAML 与承载 Pod，并支持独立编辑。' : 'Inspect node allocation, taints, YAML, and scheduled pods with standalone editing support.'}
        actions={(
          <Space>
            <Button onClick={() => navigate(`/clusters/${clusterId}`)}>{localeCode === 'zh_CN' ? '集群详情' : 'Cluster Detail'}</Button>
            <Button type="primary" onClick={() => navigate('/cluster-resources/nodes')}>{t('common.back', 'Back')}</Button>
          </Space>
        )}
      />
      <PlatformClusterScopeHint resourceLabel={localeCode === 'zh_CN' ? '节点详情' : 'Node'} />
      <Tabs
        items={[
          {
            key: 'overview',
            label: t('common.overview', 'Overview'),
            children: (
              <>
                <div className="grid gap-4 xl:grid-cols-2">
                  <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '基础信息' : 'Summary'}>
                    <Descriptions
                      items={[
                        { key: t('common.name', 'Name'), label: t('common.name', 'Name'), children: nodeDetail.name },
                        { key: t('common.cluster', 'Cluster'), label: t('common.cluster', 'Cluster'), children: clusterId },
                        { key: t('common.status', 'Status'), label: t('common.status', 'Status'), children: <StatusTag value={nodeDetail.status || 'unknown'} /> },
                        { key: localeCode === 'zh_CN' ? '版本' : 'Version', label: localeCode === 'zh_CN' ? '版本' : 'Version', children: nodeDetail.version || '-' },
                        { key: 'IP', label: 'IP', children: nodeDetail.internalIp || '-' },
                        { key: localeCode === 'zh_CN' ? 'Pod 数量' : 'Pods', label: localeCode === 'zh_CN' ? 'Pod 数量' : 'Pods', children: nodeDetail.podCount },
                        { key: localeCode === 'zh_CN' ? '存活时长' : 'Age', label: localeCode === 'zh_CN' ? '存活时长' : 'Age', children: formatAgeSeconds(nodeDetail.ageSeconds) },
                      ]}
                    />
                    <div className="soha-detail-meta">
                      <Text strong>{localeCode === 'zh_CN' ? '节点角色:' : 'Roles:'}</Text>
                      {nodeDetail.roles?.length ? (
                        <div className="soha-tag-list">
                          {nodeDetail.roles.map((role) => <Tag key={role}>{role}</Tag>)}
                        </div>
                      ) : (
                        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '未声明角色标签' : 'No explicit node role labels'}</Text>
                      )}
                    </div>
                    <div className="soha-detail-meta">
                      <Text strong>{t('common.labels', 'Labels')}:</Text>
                      {Object.keys(nodeDetail.labels || {}).length ? (
                        <div className="soha-tag-list">
                          {Object.entries(nodeDetail.labels || {}).map(([key, value]) => (
                            <Tag key={key}>{key}={value}</Tag>
                          ))}
                        </div>
                      ) : (
                        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '未配置标签' : 'No labels configured'}</Text>
                      )}
                    </div>
                    <div className="soha-detail-meta">
                      <Text strong>{localeCode === 'zh_CN' ? '污点:' : 'Taints:'}</Text>
                      {nodeDetail.taints?.length ? (
                        <div className="soha-tag-list">
                          {nodeDetail.taints.map((item) => (
                            <Tag key={`${item.key}:${item.effect}:${item.value || ''}`}>
                              {item.key}{item.value ? `=${item.value}` : ''}:{item.effect}
                            </Tag>
                          ))}
                        </div>
                      ) : (
                        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '当前节点没有污点' : 'No taints configured on this node'}</Text>
                      )}
                    </div>
                    {nodeDetail.annotations && Object.keys(nodeDetail.annotations).length > 0 ? (
                      <div className="soha-detail-meta">
                        <Text strong>{localeCode === 'zh_CN' ? '注解:' : 'Annotations:'}</Text>
                        <pre className="soha-json-block">{JSON.stringify(nodeDetail.annotations, null, 2)}</pre>
                      </div>
                    ) : null}
                  </Card>

                  <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '资源分配' : 'Resource Allocation'}>
                    <NodeResourcePanel node={nodeDetail} />
                    {nodeDetail.metricsMessage ? (
                      <div className="soha-detail-meta">
                        <Text type="secondary">{nodeDetail.metricsMessage}</Text>
                      </div>
                    ) : null}
                  </Card>
                </div>

                <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '快速编辑 Labels / 污点' : 'Quick Edit Labels / Taints'}>
                  <Form
                    key={`node-edit:${clusterId}:${nodeName}:${nodeDetailQuery.dataUpdatedAt}`}
                    layout="vertical"
                    initialValues={nodeFormInitValues}
                    onFinish={(values) => updateNodeMutation.mutate(values)}
                  >
                    <Form.Item name="labels" label="Labels(JSON)">
                      <Input.TextArea rows={8} />
                    </Form.Item>
                    <Form.Item name="taints" label="Taints(JSON Array)">
                      <Input.TextArea rows={8} />
                    </Form.Item>
                    <div className="soha-form-actions">
                      <Button
                        onClick={() => {
                          queryClient.invalidateQueries({ queryKey: ['cluster-node-detail-page', clusterId, nodeName] })
                        }}
                      >
                        {t('common.refresh', 'Refresh')}
                      </Button>
                      <Button htmlType="submit" type="primary" loading={updateNodeMutation.isPending}>
                        {t('common.save', 'Save')}
                      </Button>
                    </div>
                  </Form>
                </Card>

                <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? `承载 Pods (${nodeDetail.pods?.length ?? 0})` : `Scheduled Pods (${nodeDetail.pods?.length ?? 0})`}>
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columns={podColumns}
                    dataSource={nodeDetail.pods ?? []}
                    rowKey={(record) => `${record.namespace}/${record.name}`}
                    pageSize={10}
                    enableColumnSelection={false}
                  />
                </Card>

                <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '节点 Conditions' : 'Node Conditions'}>
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columns={conditionColumns}
                    dataSource={nodeDetail.conditions ?? []}
                    rowKey={(record) => `${record.type}:${record.lastTransitionTime || ''}`}
                    pageSize={10}
                    enableColumnSelection={false}
                  />
                </Card>
              </>
            ),
          },
          {
            key: 'yaml',
            label: t('common.yaml', 'YAML'),
            children: nodeYAMLQuery.isLoading ? (
              <Card className="soha-detail-card">
                <div className="flex items-center justify-center h-64">
                  <Spin size="large" />
                </div>
              </Card>
            ) : nodeYAMLQuery.isError ? (
              <Card className="soha-detail-card">
                <Text type="warning">{(nodeYAMLQuery.error as Error)?.message || (localeCode === 'zh_CN' ? '节点 YAML 暂不可用' : 'Node YAML is unavailable')}</Text>
              </Card>
            ) : (
              <Suspense fallback={<Card className="soha-detail-card"><Spin size="large" /></Card>}>
                <K8sYamlEditor
                  value={yamlDraft}
                  onChange={setYamlDraft}
                  onReset={() => {
                    if (yamlDraftStorageKey) {
                      window.localStorage.removeItem(yamlDraftStorageKey)
                    }
                    setYamlDraft(yamlServerValue)
                    void message.success(t('yamlEditor.resetSuccess', 'YAML draft reset'))
                  }}
                  onSave={() => {
                    if (!yamlDraftStorageKey) return
                    window.localStorage.setItem(yamlDraftStorageKey, yamlDraft)
                    void message.success(t('yamlEditor.saveSuccess', 'YAML draft saved locally'))
                  }}
                  onApply={() => applyYAMLMutation.mutate()}
                  saveDisabled={!yamlDraftStorageKey}
                  applyDisabled={!nodeYAMLPath || !yamlDraft.trim()}
                  applying={applyYAMLMutation.isPending}
                />
              </Suspense>
            ),
          },
        ]}
      />
    </div>
  )
}
