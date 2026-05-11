import { useEffect, useState } from 'react'
import { App, Button, Card, Empty, Modal, Space, Tabs, Tag, Typography } from 'antd'
import { ArrowRightOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { PodLogViewer } from '@/components/pod-log-viewer'
import { PodTerminal } from '@/components/pod-terminal'
import { ResourceMetricsPanel } from '@/components/resource-metrics-panel'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { StatusTag } from '@/components/status-tag'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import type {
  ApiResponse,
  ApplicationRuntimeDetail,
  ApplicationRuntimeWorkload,
  ApplicationWorkloadRuntimeDetail,
  DeploymentDetail,
  Pod,
  ResourceMetrics,
} from '@/types'

const { Text } = Typography

function firstPodName(pods?: Pod[]) {
  return pods?.[0]?.name || ''
}

function summarizeStatus(item: ApplicationRuntimeWorkload | undefined) {
  if (!item) return 'unknown'
  return item.latestRelease?.status || item.latestWorkflow?.status || item.latestBuild?.status || 'unknown'
}

function DeploymentOverview({ deployment }: { deployment: DeploymentDetail }) {
  return (
    <Card className="kc-detail-card">
      <Space direction="vertical" style={{ width: '100%' }} size={12}>
        <div className="kc-application-runtime-overview">
          <div>
            <Text type="secondary">Workload</Text>
            <div className="kc-application-runtime-overview__title">{deployment.name}</div>
          </div>
          <Tag color="blue">{deployment.strategy}</Tag>
        </div>
        <div className="kc-application-runtime-statgrid">
          <Card size="small"><Text type="secondary">Desired</Text><div>{deployment.desiredReplicas}</div></Card>
          <Card size="small"><Text type="secondary">Ready</Text><div>{deployment.readyReplicas}</div></Card>
          <Card size="small"><Text type="secondary">Available</Text><div>{deployment.availableReplicas}</div></Card>
        </div>
        <Card size="small" title="Labels">
          <Space wrap>
            {Object.entries(deployment.labels ?? {}).map(([key, value]) => <Tag key={key}>{`${key}=${value}`}</Tag>)}
          </Space>
        </Card>
      </Space>
    </Card>
  )
}

export function ApplicationDetailPage() {
  const { applicationId } = useParams()
  const navigate = useNavigate()
  const [activeEnvironmentId, setActiveEnvironmentId] = useState('')

  const runtimeQuery = useQuery({
    queryKey: ['application-runtime', applicationId],
    queryFn: () => api.get<ApiResponse<ApplicationRuntimeDetail>>(`/applications/${applicationId}/runtime`),
    enabled: !!applicationId,
  })

  const runtime = runtimeQuery.data?.data
  const environments = runtime?.environments ?? []

  useEffect(() => {
    if (!environments.length) {
      setActiveEnvironmentId('')
      return
    }
    if (activeEnvironmentId && environments.some((item) => item.applicationEnvironmentId === activeEnvironmentId)) {
      return
    }
    setActiveEnvironmentId(environments[0].applicationEnvironmentId)
  }, [activeEnvironmentId, environments])

  const activeEnvironment = environments.find((item) => item.applicationEnvironmentId === activeEnvironmentId) ?? environments[0]
  const workloads = activeEnvironment?.workloads ?? []

  if (runtimeQuery.isLoading) {
    return <div className="kc-page"><Card>Loading...</Card></div>
  }

  if (!runtime) {
    return <div className="kc-page"><Card><Empty description="Application not found" /></Card></div>
  }

  return (
    <div className="kc-page">
      <PageHeader
        title={runtime.application.name}
        description="按环境查看应用卡片，并进入对应服务详情。"
        actions={<Button onClick={() => navigate('/applications')}>返回应用中心</Button>}
      />
      <Card className="kc-detail-card">
        <Space wrap>
          {environments.map((item) => (
            <Tag
              key={item.applicationEnvironmentId}
              color={activeEnvironmentId === item.applicationEnvironmentId ? 'blue' : undefined}
              onClick={() => setActiveEnvironmentId(item.applicationEnvironmentId)}
            >
              {item.environmentName || item.environmentKey || item.environmentId}
            </Tag>
          ))}
        </Space>
      </Card>
      <div className="kc-application-runtime-grid">
        {workloads.length > 0 ? workloads.map((workload) => (
          <Card
            key={`${workload.clusterId}/${workload.namespace}/${workload.workloadName}`}
            hoverable
            className="kc-application-runtime-card"
            onClick={() => navigate(`/applications/${runtime.application.id}/application-environments/${workload.applicationEnvironmentId}/workloads/${encodeURIComponent(workload.workloadName)}`)}
            actions={[
              <Button
                key="open"
                type="link"
                icon={<ArrowRightOutlined />}
                onClick={(event) => {
                  event.stopPropagation()
                  navigate(`/applications/${runtime.application.id}/application-environments/${workload.applicationEnvironmentId}/workloads/${encodeURIComponent(workload.workloadName)}`)
                }}
              >
                进入详情
              </Button>,
            ]}
          >
            <Space direction="vertical" style={{ width: '100%' }}>
              <div className="kc-application-runtime-card__head">
                <strong>{workload.workloadName}</strong>
                <StatusTag value={summarizeStatus(workload)} />
              </div>
              <Text type="secondary">{`${workload.workloadKind} · ${workload.namespace}`}</Text>
              <Space wrap>
                <Tag>Desired {workload.desiredReplicas}</Tag>
                <Tag>Ready {workload.readyReplicas}</Tag>
                <Tag>{workload.clusterId}</Tag>
              </Space>
            </Space>
          </Card>
        )) : (
          <Card className="kc-application-runtime-empty"><Empty description="当前环境下没有可显示的服务/Deployment" /></Card>
        )}
      </div>
    </div>
  )
}

export function ApplicationWorkloadDetailPage() {
  const { applicationId, applicationEnvironmentId, workloadName } = useParams()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState('overview')
  const [selectedPodName, setSelectedPodName] = useState('')
  const [terminalVisible, setTerminalVisible] = useState(false)
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.application.update')

  const detailQuery = useQuery({
    queryKey: ['application-workload-runtime', applicationId, applicationEnvironmentId, workloadName],
    queryFn: () => api.get<ApiResponse<ApplicationWorkloadRuntimeDetail>>(`/applications/${applicationId}/application-environments/${applicationEnvironmentId}/workloads/${encodeURIComponent(workloadName || '')}/runtime`),
    enabled: !!applicationId && !!applicationEnvironmentId && !!workloadName,
  })

  const detail = detailQuery.data?.data
  const podList = detail?.pods ?? []
  const serviceList = detail?.services ?? []
  const ingressList = detail?.ingresses ?? []
  const deployment = detail?.deployment

  useEffect(() => {
    if (!selectedPodName) {
      setSelectedPodName(firstPodName(podList))
    }
  }, [podList, selectedPodName])

  const selectedPod = podList.find((item) => item.name === selectedPodName) ?? podList[0]

  const metricsQuery = useQuery({
    queryKey: ['application-workload-metrics', detail?.workload.clusterId, detail?.workload.namespace, detail?.workload.workloadName],
    queryFn: () => api.get<ApiResponse<ResourceMetrics>>(
      buildClusterScopedPath(detail!.workload.clusterId, `workloads/deployments/${encodeURIComponent(detail!.workload.workloadName)}/metrics`, detail!.workload.namespace, { rangeMinutes: 60 }),
    ),
    enabled: !!detail && activeTab === 'metrics',
  })

  const restartMutation = useMutation({
    mutationFn: () => api.post(`/clusters/${detail!.workload.clusterId}/workloads/deployments/restart`, {
      namespace: detail!.workload.namespace,
      name: detail!.workload.workloadName,
    }),
    onSuccess: () => {
      message.success('已触发重启')
      void queryClient.invalidateQueries({ queryKey: ['application-workload-runtime', applicationId, applicationEnvironmentId, workloadName] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  if (detailQuery.isLoading) {
    return <div className="kc-page"><Card>Loading...</Card></div>
  }
  if (!detail || !deployment) {
    return <div className="kc-page"><Card><Empty description="未找到运行详情" /></Card></div>
  }

  const tabItems = [
    {
      key: 'overview',
      label: '概览',
      children: <DeploymentOverview deployment={deployment} />,
    },
    {
      key: 'pods',
      label: 'Pods',
      children: (
        <AdminTable
          columns={[
            { title: '名称', dataIndex: 'name' },
            { title: '状态', dataIndex: 'phase' },
            { title: '节点', dataIndex: 'nodeName', render: (value?: string) => value || '-' },
            { title: '重启', dataIndex: 'restarts' },
          ]}
          dataSource={podList}
          rowKey="name"
          pageSize={10}
          onRow={(record: Pod) => ({
            onClick: () => setSelectedPodName(record.name),
          })}
        />
      ),
    },
    {
      key: 'network',
      label: '网络',
      children: (
        <div className="kc-application-runtime-network">
          <Card title="Services">
            <AdminTable
              columns={[
                { title: '名称', dataIndex: 'name' },
                { title: '类型', dataIndex: 'type' },
                { title: 'Cluster IP', dataIndex: 'clusterIp', render: (value?: string) => value || '-' },
              ]}
              dataSource={serviceList}
              rowKey="name"
              pageSize={10}
            />
          </Card>
          <Card title="Ingresses">
            <AdminTable
              columns={[
                { title: '名称', dataIndex: 'name' },
                { title: '主机', dataIndex: 'hosts', render: (value?: string[]) => (value ?? []).join(', ') || '-' },
                { title: '后端', dataIndex: 'backendServices', render: (value?: string[]) => (value ?? []).join(', ') || '-' },
              ]}
              dataSource={ingressList}
              rowKey="name"
              pageSize={10}
            />
          </Card>
        </div>
      ),
    },
    {
      key: 'logs',
      label: '日志',
      children: selectedPod ? (
        <PodLogViewer
          clusterId={detail.workload.clusterId}
          namespace={detail.workload.namespace}
          podName={selectedPod.name}
        />
      ) : (
        <Empty description="请选择一个 Pod" />
      ),
    },
    {
      key: 'terminal',
      label: '终端',
      children: selectedPod ? (
        <Space direction="vertical" style={{ width: '100%' }}>
          <Button type="primary" onClick={() => setTerminalVisible(true)}>打开终端</Button>
          <Card title={selectedPod.name}>
            <Text type="secondary">终端会复用当前 Pod 的 cluster / namespace / workload 上下文。</Text>
          </Card>
          <Modal title={`Terminal: ${selectedPod.name}`} open={terminalVisible} onCancel={() => setTerminalVisible(false)} footer={null} width={1080}>
            <PodTerminal clusterId={detail.workload.clusterId} namespace={detail.workload.namespace} podName={selectedPod.name} />
          </Modal>
        </Space>
      ) : (
        <Empty description="请选择一个 Pod" />
      ),
    },
    {
      key: 'metrics',
      label: '监控',
      children: (
        <ResourceMetricsPanel
          title="Deployment Metrics"
          data={metricsQuery.data?.data}
          loading={metricsQuery.isLoading}
          rangeMinutes={60}
          compact
        />
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title={detail.application.name}
        description={`${detail.environment?.name || detail.binding.environmentKey || detail.binding.environmentId} · ${detail.workload.workloadName}`}
        actions={(
          <Space>
            <Button onClick={() => navigate(`/applications/${detail.application.id}`)}>返回应用</Button>
            {canManage ? <Button icon={<ReloadOutlined />} onClick={() => restartMutation.mutate()}>重启</Button> : null}
          </Space>
        )}
      />
      <Tabs items={tabItems} activeKey={activeTab} onChange={setActiveTab} />
    </div>
  )
}
