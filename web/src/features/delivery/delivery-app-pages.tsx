import { useEffect, useMemo, useState } from 'react'
import type { Key } from 'react'
import { Alert, App, Button, Card, Collapse, Descriptions, Dropdown, Form, Input, Modal, Popconfirm, Select, Space, Switch, Tag, Timeline, Typography } from 'antd'
import { ApiOutlined, CheckOutlined, CloseOutlined, DeleteOutlined, EditOutlined, FileTextOutlined, LinkOutlined, MoreOutlined, PlayCircleOutlined, PlusOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons'
import type { MenuProps, TableColumnsType } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  ManagementDetailHeader,
  ManagementIconButton,
  ManagementState,
} from '@/components/management-list'
import { DeliveryTable } from '@/features/delivery/delivery-table'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import {
  canCancelExecutionTask,
  canRetryExecutionTask,
  summarizeDeliveryBuildSignal,
  summarizeDeliveryValidationSignal,
  summarizeExecutionTaskArtifacts,
  summarizeExecutionTaskStatus,
  summarizeReleaseBundleArtifact,
  summarizeReleaseBundleStatus,
} from '@/features/delivery/delivery-status'
import {
  ApplicationManagementModals,
  defaultBuildSources,
  splitApplicationGroups,
  useApplicationManagementState,
} from '@/features/delivery/application-management-pages'
import type {
  ApprovalPolicy,
  ApiResponse,
  BuildSource,
  BuildTemplate,
  DeliveryApplication,
  DeliveryApplicationDetail,
  ExecutionArtifact,
  ExecutionTask,
  ReleaseBoardEntry,
  ReleaseBundle,
  WorkflowNodeRun,
  WorkflowRun,
} from '@/types'

const { Text } = Typography
type ColumnProps<T> = TableColumnsType<T>[number]
type ApplicationBindingRow = NonNullable<DeliveryApplicationDetail['bindings']>[number]
type ApplicationWorkspaceCard = {
  app: DeliveryApplication
  bindings: ReleaseBoardEntry[]
  deliverySignal: { color: string; label: string }
  gateSignal: { color: string; label: string }
  activeTargets: number
  latestEnvironmentName: string
}

function parseJSONObject(raw: unknown, field: string) {
  const value = typeof raw === 'string' ? raw.trim() : ''
  if (!value) return {}
  try {
    const parsed = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('invalid')
    }
    return parsed
  } catch {
    throw new Error(`${field} 需要是合法 JSON 对象`)
  }
}

function summarizeBuildSource(source?: BuildSource) {
  if (!source) return '-'
  switch (source.type) {
    case 'repo_dockerfile':
      return 'Repo Dockerfile'
    case 'platform_build_template':
      return 'Platform Template'
    case 'external_pipeline':
      return 'External Pipeline'
    default:
      return source.type
  }
}

function renderTargetSummary(targets?: ApplicationBindingRow['targets']) {
  if (!targets?.length) {
    return '-'
  }
  const visibleTargets = targets.slice(0, 2)
  return (
    <Space orientation="vertical" size={2}>
      {visibleTargets.map((target, index) => (
        <Text key={`${target.clusterId}-${target.namespace}-${target.workloadName}-${index}`}>
          {`${target.clusterId} / ${target.namespace} / ${target.workloadName}`}
        </Text>
      ))}
      {targets.length > visibleTargets.length ? (
        <Text type="secondary">{`+${targets.length - visibleTargets.length}`}</Text>
      ) : null}
    </Space>
  )
}

function summarizeApplicationRole(app: DeliveryApplication) {
  const language = app.language ? app.language.toUpperCase() : 'APP'
  return language
}

function summarizeEnvironmentCoverage(bindings: ReleaseBoardEntry[]) {
  const names = Array.from(
    new Set(bindings.map((item) => item.environmentName || item.environmentKey || item.environmentId).filter(Boolean)),
  )
  if (names.length === 0) return '尚未绑定环境'
  if (names.length === 1) return names[0]
  return `${names.slice(0, 2).join(' / ')}${names.length > 2 ? ` +${names.length - 2}` : ''}`
}

function metadataText(metadata: Record<string, unknown> | undefined, ...keys: string[]) {
  if (!metadata) return ''
  for (const key of keys) {
    const value = metadata[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
    if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  }
  return ''
}

function workflowGatewayTrace(run: WorkflowRun) {
  return {
    approvalRequestId: metadataText(run.metadata, 'aiGatewayApprovalRequestId', 'approvalRequestId'),
    approvalPolicyRef: metadataText(run.metadata, 'aiGatewayApprovalPolicyRef'),
    policyId: metadataText(run.metadata, 'aiGatewayPolicyId'),
    toolName: metadataText(run.metadata, 'aiGatewayToolName'),
    skillId: metadataText(run.metadata, 'aiGatewaySkillId'),
    aiClientId: metadataText(run.metadata, 'aiGatewayAIClientId'),
  }
}

function workflowGatewayPath(approvalRequestId: string) {
  const search = new URLSearchParams()
  if (approvalRequestId) search.set('approvalRequestId', approvalRequestId)
  const suffix = search.toString()
  return `/ai-gateway/governance${suffix ? `?${suffix}` : ''}`
}

function workflowManualApprovalNode(run: WorkflowRun) {
  return run.nodeRuns?.find((item) => item.type === 'manual_approval') ?? null
}

function workflowNodeTimelineColor(status: string) {
  const normalized = status.toLowerCase()
  if (['completed', 'success', 'approved'].includes(normalized)) return '#52c41a'
  if (['failed', 'rejected', 'canceled', 'callback_timeout'].includes(normalized)) return '#ff4d4f'
  if (['waiting_approval', 'pending_approval', 'pending'].includes(normalized)) return '#faad14'
  if (['running', 'dispatching'].includes(normalized)) return '#1677ff'
  return '#8c8c8c'
}

function workflowNodeSummary(node: WorkflowNodeRun) {
  return node.summary || [node.type, node.status].filter(Boolean).join(' / ') || '-'
}

function WorkflowNodeTimeline({ nodes }: { nodes?: WorkflowNodeRun[] }) {
  if (!nodes?.length) {
    return <ManagementState bordered={false} compact description="No workflow nodes" />
  }
  return (
    <Timeline
      mode="start"
      items={nodes.map((node) => ({
        color: workflowNodeTimelineColor(node.status),
        title: <Text type="secondary">{node.finishedAt ? formatDateTime(node.finishedAt) : node.startedAt ? formatDateTime(node.startedAt) : '-'}</Text>,
        content: (
          <Space orientation="vertical" size={2}>
            <Space size={6} wrap>
              <Text strong>{node.name || node.nodeId}</Text>
              <Tag>{node.type}</Tag>
              <StatusTag value={node.status} />
            </Space>
            <Text type="secondary">{node.nodeId}</Text>
            <Text>{workflowNodeSummary(node)}</Text>
          </Space>
        ),
      }))}
    />
  )
}

function WorkflowManualApprovalDetail({ run }: { run: WorkflowRun }) {
  const trace = workflowGatewayTrace(run)
  const approvalNode = workflowManualApprovalNode(run)
  if (!approvalNode) {
    return <ManagementState bordered={false} compact description="No manual approval node" />
  }
  return (
    <Descriptions
      size="small"
      bordered
      column={3}
      items={[
        { key: 'nodeId', label: 'Node ID', children: approvalNode.nodeId },
        { key: 'name', label: 'Name', children: approvalNode.name || '-' },
        { key: 'type', label: 'Type', children: approvalNode.type || '-' },
        { key: 'status', label: 'Status', children: <StatusTag value={approvalNode.status} /> },
        { key: 'startedAt', label: 'Started', children: approvalNode.startedAt ? formatDateTime(approvalNode.startedAt) : '-' },
        { key: 'finishedAt', label: 'Finished', children: approvalNode.finishedAt ? formatDateTime(approvalNode.finishedAt) : '-' },
        { key: 'summary', label: 'Summary', span: 3, children: approvalNode.summary || '-' },
        { key: 'approvalRequestId', label: 'Gateway Approval', children: trace.approvalRequestId || '-' },
        { key: 'policy', label: 'Policy', children: trace.approvalPolicyRef || trace.policyId || '-' },
        { key: 'tool', label: 'Tool', children: trace.toolName || '-' },
        { key: 'skill', label: 'Skill', children: trace.skillId || '-' },
        { key: 'client', label: 'AI Client', children: trace.aiClientId || '-' },
      ]}
    />
  )
}

function WorkflowGatewayTracePanel({ run }: { run: WorkflowRun }) {
  const navigate = useNavigate()
  const trace = workflowGatewayTrace(run)
  const approvalNode = workflowManualApprovalNode(run)
  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Descriptions size="small" bordered column={3} items={[
        { key: 'approval', label: 'Gateway Approval', children: trace.approvalRequestId ? <Button size="small" type="link" icon={<LinkOutlined />} onClick={() => navigate(workflowGatewayPath(trace.approvalRequestId))}>{trace.approvalRequestId}</Button> : '-' },
        { key: 'policy', label: 'Policy', children: trace.approvalPolicyRef || trace.policyId || '-' },
        { key: 'tool', label: 'Tool', children: trace.toolName || '-' },
        { key: 'skill', label: 'Skill', children: trace.skillId || '-' },
        { key: 'client', label: 'AI Client', children: trace.aiClientId || '-' },
        { key: 'manualNode', label: 'Manual Approval Node', children: approvalNode ? `${approvalNode.name || approvalNode.nodeId} / ${approvalNode.status}` : '-' },
      ]} />
      <Collapse
        size="small"
        defaultActiveKey={['manual-approval', 'node-timeline']}
        items={[
          {
            key: 'manual-approval',
            label: 'Manual approval detail',
            children: <WorkflowManualApprovalDetail run={run} />,
          },
          {
            key: 'node-timeline',
            label: 'Workflow node timeline',
            children: <WorkflowNodeTimeline nodes={run.nodeRuns} />,
          },
          {
            key: 'raw-trace',
            label: 'Raw trace',
            children: <pre className="soha-json-block">{JSON.stringify({ id: run.id, metadata: run.metadata, nodeRuns: run.nodeRuns }, null, 2)}</pre>,
          },
        ]}
      />
    </Space>
  )
}

export function ApplicationsPage() {
  const navigate = useNavigate()
  const managementState = useApplicationManagementState()
  const [activeGroup, setActiveGroup] = useState<string>('all')
  const [deleteConfirmApp, setDeleteConfirmApp] = useState<DeliveryApplication | null>(null)

  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<DeliveryApplication[]>>('/applications'),
  })
  const releaseBoardQuery = useQuery({
    queryKey: ['delivery-release-board'],
    queryFn: () => api.get<ApiResponse<ReleaseBoardEntry[]>>('/delivery/release-board'),
  })

  const boardByApp = useMemo(() => {
    const items = releaseBoardQuery.data?.data ?? []
    return items.reduce<Record<string, ReleaseBoardEntry[]>>((acc, item) => {
      acc[item.applicationId] = [...(acc[item.applicationId] ?? []), item]
      return acc
    }, {})
  }, [releaseBoardQuery.data])
  const applicationCards = useMemo<ApplicationWorkspaceCard[]>(() => {
    return (applicationsQuery.data?.data ?? []).map((app) => {
      const bindings = boardByApp[app.id] ?? []
      return {
        app,
        bindings,
        deliverySignal: summarizeDeliveryBuildSignal(bindings, { completedLabel: '最近已构建' }),
        gateSignal: summarizeDeliveryValidationSignal(bindings, { readyLabel: '可验证' }),
        activeTargets: bindings.reduce((sum, item) => sum + (item.targets?.length ?? 0), 0),
        latestEnvironmentName: summarizeEnvironmentCoverage(bindings),
      }
    })
  }, [applicationsQuery.data, boardByApp])
  const groupOptions = useMemo(() => {
    return ['all', ...managementState.applicationGroupOptions]
  }, [managementState.applicationGroupOptions])
  const visibleApplicationCards = useMemo(
    () => applicationCards.filter(({ app }) => activeGroup === 'all' || splitApplicationGroups(app.group).includes(activeGroup)),
    [activeGroup, applicationCards],
  )
  useEffect(() => {
    if (!groupOptions.includes(activeGroup)) {
      setActiveGroup('all')
    }
  }, [activeGroup, groupOptions])
  const openCreateApplication = () => {
    managementState.setEditingApp(null)
    managementState.setBuildSources(defaultBuildSources())
    managementState.setAppModalVisible(true)
  }
  const openEditApplication = (app: DeliveryApplication) => {
    managementState.setEditingApp(app)
    managementState.setBuildSources(app.buildSources ?? [])
    managementState.setAppModalVisible(true)
  }

  return (
    <div className="soha-page">
      <div className="soha-application-center-toolbar">
        <div className="soha-application-group-tags">
          {groupOptions.map((group) => (
            <Tag
              key={group}
              className={`soha-application-group-tag ${activeGroup === group ? 'is-active' : ''}`}
              variant="filled"
              onClick={() => setActiveGroup(group)}
            >
              {group === 'all' ? '全部' : group}
            </Tag>
          ))}
        </div>
        <Button type="primary" icon={<PlusOutlined />} disabled={!managementState.canCreateApplication} onClick={openCreateApplication}>
          新建应用
        </Button>
      </div>

      {applicationsQuery.isLoading || releaseBoardQuery.isLoading ? (
        <ManagementState kind="loading" />
      ) : visibleApplicationCards.length > 0 ? (
        <div className="soha-application-card-list">
          {visibleApplicationCards.map(({ app, bindings, deliverySignal, gateSignal, activeTargets, latestEnvironmentName }) => {
            const actionMenuItems: MenuProps['items'] = [
              ...(managementState.canUpdateApplication ? [{ key: 'edit', icon: <EditOutlined />, label: '编辑' }] : []),
              ...(managementState.canDeleteApplication ? [{ key: 'delete', danger: true, icon: <DeleteOutlined />, label: '删除' }] : []),
            ]
            return (
              <Card
                key={app.id}
                className="soha-application-card"
                hoverable
                onClick={() => navigate(`/applications/${app.id}`)}
              >
                <div className="soha-application-card__header">
                  <div className="soha-application-card__title-wrap">
                    <div className="soha-application-card__title-row">
                      <h3 className="soha-application-card__title">{app.name}</h3>
                    </div>
                    <div className="soha-application-card__subline">
                      <Text type="secondary">{summarizeApplicationRole(app)}</Text>
                      <Tag>{app.enabled ? 'enabled' : 'disabled'}</Tag>
                    </div>
                  </div>
                  <div className="soha-application-card__header-actions">
                    {actionMenuItems.length > 0 ? (
                      <Popconfirm
                        title="确认删除应用？"
                        description={deleteConfirmApp?.id === app.id ? `删除 ${app.name} 后不可恢复。` : undefined}
                        open={deleteConfirmApp?.id === app.id}
                        okText="删除"
                        cancelText="取消"
                        okButtonProps={{ danger: true }}
                        placement="bottomRight"
                        onCancel={(event) => {
                          event?.stopPropagation()
                          setDeleteConfirmApp(null)
                        }}
                        onConfirm={(event) => {
                          event?.stopPropagation()
                          managementState.deleteAppMutation.mutate(app.id)
                          setDeleteConfirmApp(null)
                        }}
                        onOpenChange={(open) => {
                          if (!open && deleteConfirmApp?.id === app.id) {
                            setDeleteConfirmApp(null)
                          }
                        }}
                      >
                        <Dropdown
                          trigger={['click']}
                          placement="bottomRight"
                          menu={{
                            items: actionMenuItems,
                            onClick: ({ key, domEvent }) => {
                              domEvent.stopPropagation()
                              if (key === 'edit') {
                                openEditApplication(app)
                              }
                              if (key === 'delete') {
                                setDeleteConfirmApp(app)
                              }
                            },
                          }}
                        >
                          <Button
                            aria-label={`${app.name} 操作`}
                            className="soha-application-card__more"
                            icon={<MoreOutlined />}
                            size="small"
                            type="text"
                            onClick={(event) => event.stopPropagation()}
                          />
                        </Dropdown>
                      </Popconfirm>
                    ) : null}
                  </div>
                </div>

                <div className="soha-application-card__signals">
                  <Tag color={deliverySignal.color}>{`交付: ${deliverySignal.label}`}</Tag>
                  <Tag color={gateSignal.color}>{`门禁: ${gateSignal.label}`}</Tag>
                </div>

                <div className="soha-application-card__stats">
                  <div className="soha-application-card__stat">
                    <span className="soha-application-card__stat-label">环境覆盖</span>
                    <span className="soha-application-card__stat-value">{bindings.length || app.environmentCount || 0}</span>
                  </div>
                  <div className="soha-application-card__stat">
                    <span className="soha-application-card__stat-label">部署目标</span>
                    <span className="soha-application-card__stat-value">{activeTargets}</span>
                  </div>
                  <div className="soha-application-card__stat is-wide">
                    <span className="soha-application-card__stat-label">环境描述</span>
                    <span className="soha-application-card__stat-value">{latestEnvironmentName}</span>
                  </div>
                </div>
              </Card>
            )
          })}
        </div>
      ) : (
        <Card className="soha-application-empty-card">
          <ManagementState bordered={false} compact title={activeGroup === 'all' ? '暂无应用' : '分组暂无应用'} description="" />
        </Card>
      )}
      <ApplicationManagementModals state={managementState} />
    </div>
  )
}

export function ApplicationDetailPage() {
  const { applicationId } = useParams()
  const navigate = useNavigate()
  const detailQuery = useQuery({
    queryKey: ['application-detail', applicationId],
    queryFn: () => api.get<ApiResponse<DeliveryApplicationDetail>>(`/applications/${applicationId}/detail`),
    enabled: !!applicationId,
  })
  const detail = detailQuery.data?.data
  const application = detail?.application

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title={application?.name || 'Application Detail'}
        description="应用总览、构建来源、环境矩阵与最近执行记录。"
        actions={<Button onClick={() => navigate('/applications')}>返回应用中心</Button>}
      />
      <Card className="soha-management-panel-card">
        <Descriptions items={[
          { key: 'group', label: '分组', children: application?.group || '-' },
          { key: 'language', label: '语言', children: application?.language || '-' },
          { key: 'status', label: '最近状态', children: <StatusTag value={detail?.latestRelease?.status || detail?.latestWorkflow?.status || detail?.latestBuild?.status || 'unknown'} /> },
        ]} />
      </Card>
      <DeliveryTable
        title="构建来源"
        rowKey="id"
        pagination={false}
        dataSource={application?.buildSources ?? []}
        loading={detailQuery.isLoading}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '类型', dataIndex: 'type', render: (value: string) => <Tag>{value}</Tag> },
          { title: '镜像', dataIndex: 'buildImage', render: (value: string) => value || '-' },
          { title: '默认', dataIndex: 'isDefault', render: (value: boolean) => <BooleanTag value={value} /> },
          { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
        ]}
      />
      <DeliveryTable
        title="环境矩阵"
        rowKey="applicationEnvironmentId"
        pagination={false}
        dataSource={detail?.bindings ?? []}
        loading={detailQuery.isLoading}
        columns={[
          { title: '环境', dataIndex: 'environmentName', render: (value: string, record: ApplicationBindingRow) => value || record.environmentKey || record.environmentId },
          { title: '部署目标', dataIndex: 'targets', render: (value: ApplicationBindingRow['targets']) => renderTargetSummary(value) },
          { title: '动作', dataIndex: 'actionKind', render: (value: string) => value || 'deploy' },
          { title: '构建来源', dataIndex: 'buildSource', render: (value: BuildSource | undefined) => summarizeBuildSource(value) },
          { title: '目标数', dataIndex: 'targetCount' },
          { title: '审批', dataIndex: 'requiresApproval', render: (value: boolean) => <BooleanTag value={value} /> },
          { title: 'Bundle', dataIndex: 'latestBundle', render: (value: ApplicationBindingRow['latestBundle']) => <StatusTag value={value?.status || 'unknown'} /> },
          { title: 'Task', dataIndex: 'latestExecutionTask', render: (value: ApplicationBindingRow['latestExecutionTask']) => <StatusTag value={value?.status || 'unknown'} /> },
          { title: 'Workflow', dataIndex: 'latestWorkflow', render: (value: WorkflowRun | undefined) => <StatusTag value={value?.status || 'unknown'} /> },
          { title: 'Release', dataIndex: 'latestRelease', render: (value: ApplicationBindingRow['latestRelease']) => <StatusTag value={value?.status || 'unknown'} /> },
          {
            ...tableColumnPresets.action,
            title: '操作',
            dataIndex: 'applicationEnvironmentId',
            render: () => (
              <ManagementIconButton
                aria-label="查看应用配置"
                icon={<LinkOutlined />}
                size="small"
                tooltip="应用配置"
                onClick={() => navigate(`/applications/${applicationId ?? ''}`)}
              />
            ),
          },
        ]}
      />
    </div>
  )
}

export function BuildTemplatesPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.build-templates.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<BuildTemplate | null>(null)

  const templatesQuery = useQuery({
    queryKey: ['build-templates'],
    queryFn: () => api.get<ApiResponse<BuildTemplate[]>>('/build-templates'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/build-templates', values),
    onSuccess: () => {
      message.success('构建模板创建成功')
      queryClient.invalidateQueries({ queryKey: ['build-templates'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/build-templates/${id}`, values),
    onSuccess: () => {
      message.success('构建模板更新成功')
      queryClient.invalidateQueries({ queryKey: ['build-templates'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/build-templates/${id}`),
    onSuccess: () => {
      message.success('构建模板已删除')
      queryClient.invalidateQueries({ queryKey: ['build-templates'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <div className="soha-page">
      <DeliveryTable
        rowKey="id"
        actions={canManage ? (
          <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>
            新建模板
          </Button>
        ) : null}
        refreshing={templatesQuery.isFetching}
        onRefresh={() => void templatesQuery.refetch()}
        loading={templatesQuery.isLoading}
        dataSource={templatesQuery.data?.data ?? []}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: 'Key', dataIndex: 'key' },
          { title: 'Builder', dataIndex: 'builderKind' },
          { title: '命令数', dataIndex: 'buildCommands', render: (value: string[]) => value?.length ?? 0 },
          { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
          {
            ...tableColumnPresets.action,
            title: '操作',
            dataIndex: 'id',
            render: (_: unknown, record: BuildTemplate) => (
              <Space className="soha-row-action-icons" size={2}>
                {canManage ? (
                  <ManagementIconButton
                    aria-label="编辑构建模板"
                    icon={<EditOutlined />}
                    size="small"
                    tooltip="编辑"
                    onClick={() => { setEditing(record); setModalVisible(true) }}
                  />
                ) : null}
                {canManage ? (
                  <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)} placement="topRight">
                    <ManagementIconButton
                      aria-label="删除构建模板"
                      danger
                      icon={<DeleteOutlined />}
                      size="small"
                      tooltip="删除"
                    />
                  </Popconfirm>
                ) : null}
              </Space>
            ),
          },
        ]}
      />
      <Modal title={editing ? '编辑构建模板' : '新建构建模板'} open={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null} destroyOnHidden width={960}>
        <Form
          form={form}
          key={editing?.id ?? 'build-template'}
          layout="vertical"
          initialValues={editing ? { ...editing, buildCommandsText: (editing.buildCommands ?? []).join('\n'), variableSchemaText: JSON.stringify(editing.variableSchema ?? {}, null, 2), defaultVariablesText: JSON.stringify(editing.defaultVariables ?? {}, null, 2) } : { enabled: true, builderKind: 'custom', variableSchemaText: '{}', defaultVariablesText: '{}' }}
          onFinish={(values) => {
            let payload: Record<string, unknown>
            try {
              payload = {
                ...values,
                buildCommands: String(values.buildCommandsText || '').split('\n').map((item) => item.trim()).filter(Boolean),
                variableSchema: parseJSONObject(values.variableSchemaText, '变量 Schema'),
                defaultVariables: parseJSONObject(values.defaultVariablesText, '默认变量'),
              }
            } catch (err) {
              message.error((err as Error).message)
              return
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
        >
          <Form.Item name="key" label="模板 Key" rules={[{ required: true, message: '请输入模板 Key' }]}><Input /></Form.Item>
          <Form.Item name="name" label="模板名称" rules={[{ required: true, message: '请输入模板名称' }]}><Input /></Form.Item>
          <Form.Item name="builderKind" label="Builder Kind"><Select options={[{ value: 'docker', label: 'docker' }, { value: 'buildx', label: 'buildx' }, { value: 'kaniko', label: 'kaniko' }, { value: 'custom', label: 'custom' }]} /></Form.Item>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Form.Item name="dockerfileTemplate" label="Dockerfile 模板"><Input.TextArea rows={8} /></Form.Item>
          <Form.Item name="buildCommandsText" label="构建命令"><Input.TextArea rows={8} placeholder="one command per line" /></Form.Item>
          <Form.Item name="variableSchemaText" label="变量 Schema(JSON)"><Input.TextArea rows={6} /></Form.Item>
          <Form.Item name="defaultVariablesText" label="默认变量(JSON)"><Input.TextArea rows={6} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <div className="soha-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>保存</Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

export function WorkflowsPage() {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const focusedWorkflowRunId = searchParams.get('workflowRunId')?.trim() ?? ''
  const focusedGatewayApprovalRequestId = searchParams.get('gatewayApprovalRequestId')?.trim() ?? ''
  const [expandedWorkflowRunIds, setExpandedWorkflowRunIds] = useState<string[]>([])
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canTriggerWorkflow = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.workflows.trigger')

  const workflowsQuery = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.get<ApiResponse<WorkflowRun[]>>('/workflows'),
  })

  const triggerMutation = useMutation({
    mutationFn: (record: WorkflowRun) => api.post('/workflows/trigger', {
      applicationId: record.applicationId,
      workflowName: record.workflowName,
      clusterId: record.clusterId,
      namespace: record.namespace,
      deploymentName: record.deploymentName,
      triggerBuild: true,
      triggerRelease: true,
    }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['workflows'] }),
    onError: (err: Error) => message.error(err.message),
  })
  const approveMutation = useMutation({
    mutationFn: (record: WorkflowRun) => api.post(`/workflows/${record.id}/approve`, { comment: 'Approved from console' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['workflows'] }),
    onError: (err: Error) => message.error(err.message),
  })
  const rejectMutation = useMutation({
    mutationFn: (record: WorkflowRun) => api.post(`/workflows/${record.id}/reject`, { comment: 'Rejected from console' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['workflows'] }),
    onError: (err: Error) => message.error(err.message),
  })

  const workflows = workflowsQuery.data?.data ?? []
  const focusedRun = focusedWorkflowRunId ? workflows.find((item) => item.id === focusedWorkflowRunId) : undefined
  useEffect(() => {
    if (!focusedWorkflowRunId) return
    setExpandedWorkflowRunIds((current) => current.includes(focusedWorkflowRunId) ? current : [focusedWorkflowRunId, ...current])
  }, [focusedWorkflowRunId])

  const columns: ColumnProps<WorkflowRun>[] = [
    {
      title: t('common.workflow', 'Workflow'),
      dataIndex: 'workflowName',
      render: (value: string, record: WorkflowRun) => (
        <Space orientation="vertical" size={0}>
          <Space size={6} wrap>
            <Text strong>{value}</Text>
            {record.id === focusedWorkflowRunId ? <Tag color="blue">已定位</Tag> : null}
          </Space>
          <Text type="secondary">{record.id}</Text>
        </Space>
      ),
    },
    { title: t('common.application', 'Application'), dataIndex: 'applicationId' },
    { title: t('common.cluster', 'Cluster'), dataIndex: 'clusterId' },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace' },
    { ...tableColumnPresets.status, title: t('common.status', 'Status'), dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    {
      title: t('page.delivery.workflows.nodeProgress', 'Node Progress'),
      dataIndex: 'nodeRuns',
      render: (value: WorkflowRun['nodeRuns']) => `${value?.filter((item) => item.status !== 'pending').length ?? 0}/${value?.length ?? 0}`,
    },
    {
      title: 'Gateway',
      key: 'gateway',
      width: 220,
      render: (_: unknown, record: WorkflowRun) => {
        const trace = workflowGatewayTrace(record)
        return trace.approvalRequestId ? (
          <Space orientation="vertical" size={0}>
            <Button size="small" type="link" icon={<LinkOutlined />} onClick={() => navigate(workflowGatewayPath(trace.approvalRequestId))}>
              {trace.approvalRequestId}
            </Button>
            <Text type="secondary">{trace.toolName || 'AI Gateway'}</Text>
          </Space>
        ) : <Text type="secondary">-</Text>
      },
    },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '最近运行' : 'Last Run', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: t('common.actions', 'Actions'),
            dataIndex: 'id',
            render: (_: unknown, record: WorkflowRun) => (
        <Space className="soha-row-action-icons" size={2}>
          {canTriggerWorkflow ? (
            <ManagementIconButton
              aria-label={localeCode === 'zh_CN' ? '触发工作流' : 'Trigger workflow'}
              icon={<PlayCircleOutlined />}
              size="small"
              tooltip={localeCode === 'zh_CN' ? '触发' : 'Trigger'}
              onClick={() => triggerMutation.mutate(record)}
            />
          ) : null}
          {canTriggerWorkflow && record.status === 'waiting_approval' ? (
            <ManagementIconButton
              aria-label="批准工作流"
              icon={<CheckOutlined />}
              size="small"
              tooltip="批准"
              onClick={() => approveMutation.mutate(record)}
            />
          ) : null}
          {canTriggerWorkflow && record.status === 'waiting_approval' ? (
            <ManagementIconButton
              aria-label="拒绝工作流"
              danger
              icon={<CloseOutlined />}
              size="small"
              tooltip="拒绝"
              onClick={() => rejectMutation.mutate(record)}
            />
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page">
      {focusedWorkflowRunId || focusedGatewayApprovalRequestId ? (
        <Alert
          type={focusedWorkflowRunId && !workflowsQuery.isLoading && !focusedRun ? 'warning' : 'info'}
          showIcon
          title={focusedRun ? `已定位工作流 ${focusedRun.id}` : 'Gateway 审批关联工作流定位'}
          description={[
            focusedWorkflowRunId ? `workflowRunId=${focusedWorkflowRunId}` : '',
            focusedGatewayApprovalRequestId ? `gatewayApprovalRequestId=${focusedGatewayApprovalRequestId}` : '',
          ].filter(Boolean).join(' / ') || undefined}
        />
      ) : null}
      <DeliveryTable
        refreshing={workflowsQuery.isFetching}
        onRefresh={() => void workflowsQuery.refetch()}
        columns={columns}
        dataSource={workflows}
        rowKey="id"
        loading={workflowsQuery.isLoading}
        expandable={{
          expandedRowKeys: expandedWorkflowRunIds,
          expandedRowRender: (record: WorkflowRun) => <WorkflowGatewayTracePanel run={record} />,
          onExpandedRowsChange: (keys: readonly Key[]) => setExpandedWorkflowRunIds(keys.map(String)),
        }}
      />
    </div>
  )
}

export function ReleaseBundlesPage() {
  const bundlesQuery = useQuery({
    queryKey: ['release-bundles'],
    queryFn: () => api.get<ApiResponse<ReleaseBundle[]>>('/delivery/release-bundles'),
  })
  const bundles = bundlesQuery.data?.data ?? []
  const bundleSummary = useMemo(() => summarizeReleaseBundleStatus(bundles), [bundles])

  return (
    <div className="soha-page">
      <div className="soha-release-bundle-summary">
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">候选版本</Text>
          <strong>{bundleSummary.total}</strong>
          <Text type="secondary">{bundleSummary.ready} 个可验证 / 可推广</Text>
        </Card>
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">阻塞版本</Text>
          <strong>{bundleSummary.blocked}</strong>
          <Text type="secondary">构建或发布失败</Text>
        </Card>
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">交付物</Text>
          <strong>{bundleSummary.artifacts}</strong>
          <Text type="secondary">镜像 / 包 / digest</Text>
        </Card>
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">缺少交付物</Text>
          <strong>{bundleSummary.missingArtifacts}</strong>
          <Text type="secondary">需要回填 artifact</Text>
        </Card>
      </div>
      <DeliveryTable
        rowKey="id"
        refreshing={bundlesQuery.isFetching}
        onRefresh={() => void bundlesQuery.refetch()}
        loading={bundlesQuery.isLoading}
        dataSource={bundles}
        columns={[
          {
            title: 'Version',
            dataIndex: 'version',
            render: (value: string, record: ReleaseBundle) => (
              <Space orientation="vertical" size={0}>
                <Text strong>{value}</Text>
                <Text type="secondary">{record.id}</Text>
              </Space>
            ),
          },
          { title: 'Application', dataIndex: 'applicationId' },
          { title: 'Environment Binding', dataIndex: 'applicationEnvironmentId', render: (value: string) => value || '-' },
          { title: 'Source', dataIndex: 'sourceType' },
          { title: 'Artifact', dataIndex: 'artifactRef', render: (_: unknown, record: ReleaseBundle) => summarizeReleaseBundleArtifact(record) },
          { title: 'Digest', dataIndex: 'artifactDigest', render: (value: string) => value || '-' },
          { title: 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
          { ...tableColumnPresets.datetime, title: 'Updated', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
        ]}
      />
    </div>
  )
}

export function ExecutionTasksPage() {
  const { message } = App.useApp()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.execution-tasks.manage')
  const [selectedTask, setSelectedTask] = useState<ExecutionTask | null>(null)
  const tasksQuery = useQuery({
    queryKey: ['execution-tasks'],
    queryFn: () => api.get<ApiResponse<ExecutionTask[]>>('/delivery/execution-tasks'),
    refetchInterval: 5000,
  })
  const logsQuery = useQuery({
    queryKey: ['execution-task-logs', selectedTask?.id],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; logLevel: string; message: string; createdAt: string }>>>(`/delivery/execution-tasks/${selectedTask!.id}/logs`),
    enabled: !!selectedTask?.id,
    refetchInterval: selectedTask?.id ? 5000 : false,
  })
  const callbackMutation = useMutation({
    mutationFn: (task: ExecutionTask) => api.post('/delivery/execution-callbacks', {
      callbackToken: task.callbackToken,
      status: 'completed',
      payload: {
        logs: [`manual callback for ${task.id}`],
      },
    }),
    onSuccess: () => {
      message.success('回调已记录')
      tasksQuery.refetch()
      if (selectedTask?.id) {
        logsQuery.refetch()
      }
    },
    onError: (err: Error) => message.error(err.message),
  })
  const cancelMutation = useMutation({
    mutationFn: (task: ExecutionTask) => api.post(`/delivery/execution-tasks/${task.id}/cancel`, {
      reason: 'Canceled from execution tasks console',
    }),
    onSuccess: () => {
      message.success('任务已取消')
      tasksQuery.refetch()
      if (selectedTask?.id) {
        logsQuery.refetch()
      }
    },
    onError: (err: Error) => message.error(err.message),
  })
  const retryMutation = useMutation({
    mutationFn: (task: ExecutionTask) => api.post(`/delivery/execution-tasks/${task.id}/retry`, {
      reason: 'Retried from execution tasks console',
    }),
    onSuccess: () => {
      message.success('任务已重新入队')
      tasksQuery.refetch()
      if (selectedTask?.id) {
        logsQuery.refetch()
      }
    },
    onError: (err: Error) => message.error(err.message),
  })
  const executionTasks = tasksQuery.data?.data ?? []
  const executionSummary = useMemo(() => summarizeExecutionTaskStatus(executionTasks), [executionTasks])

  return (
    <div className="soha-page">
      <div className="soha-execution-task-summary">
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">任务总数</Text>
          <strong>{executionSummary.total}</strong>
          <Text type="secondary">{executionSummary.active} 个执行中</Text>
        </Card>
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">阻塞任务</Text>
          <strong>{executionSummary.blocked}</strong>
          <Text type="secondary">{executionSummary.retryable} 个可重试</Text>
        </Card>
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">交付物线索</Text>
          <strong>{executionSummary.artifacts}</strong>
          <Text type="secondary">来自任务结果</Text>
        </Card>
        <Card className="soha-management-panel-card" size="small">
          <Text type="secondary">回调可用</Text>
          <strong>{executionSummary.callbackReady}</strong>
          <Text type="secondary">agent / callback token</Text>
        </Card>
      </div>
      <DeliveryTable
        rowKey="id"
        refreshing={tasksQuery.isFetching}
        onRefresh={() => void tasksQuery.refetch()}
        loading={tasksQuery.isLoading}
        dataSource={executionTasks}
        columns={[
          {
            title: 'Task',
            dataIndex: 'taskKind',
            render: (value: string, record: ExecutionTask) => (
              <Space orientation="vertical" size={0}>
                <Text strong>{value}</Text>
                <Text type="secondary">{record.id}</Text>
              </Space>
            ),
          },
          { title: 'Provider', dataIndex: 'providerKind' },
          { title: 'Target', dataIndex: 'targetKind' },
          {
            title: 'Application',
            dataIndex: 'applicationId',
            render: (value: string, record: ExecutionTask) => (
              <Space orientation="vertical" size={0}>
                <Text>{value}</Text>
                <Text type="secondary">{record.applicationEnvironmentId || '-'}</Text>
              </Space>
            ),
          },
          { title: 'Bundle', dataIndex: 'releaseBundleId', render: (value: string) => value || '-' },
          { title: 'Artifacts', dataIndex: 'artifacts', render: (value?: ExecutionArtifact[]) => summarizeExecutionTaskArtifacts(value) },
          { title: 'Status', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
          { title: 'Retries', dataIndex: 'attemptCount', render: (value: number, record: ExecutionTask) => `${value}/${record.maxRetries}` },
          { title: 'Timeout(s)', dataIndex: 'timeoutSeconds' },
          { ...tableColumnPresets.datetime, title: 'Heartbeat', dataIndex: 'lastHeartbeatAt', render: (value?: string) => value ? formatDateTime(value) : '-' },
          { ...tableColumnPresets.datetime, title: 'Updated', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
          {
            ...tableColumnPresets.action,
            title: '操作',
            dataIndex: 'id',
            render: (_: unknown, record: ExecutionTask) => (
              <Space className="soha-row-action-icons" size={2}>
                <ManagementIconButton
                  aria-label="查看执行日志"
                  icon={<FileTextOutlined />}
                  size="small"
                  tooltip="日志"
                  onClick={() => setSelectedTask(record)}
                />
                {canManage && canCancelExecutionTask(record) ? (
                  <Popconfirm title="确认取消该任务？" onConfirm={() => cancelMutation.mutate(record)}>
                    <ManagementIconButton
                      aria-label="取消执行任务"
                      danger
                      icon={<StopOutlined />}
                      size="small"
                      tooltip="取消"
                    />
                  </Popconfirm>
                ) : null}
                {canManage && canRetryExecutionTask(record) ? (
                  <ManagementIconButton
                    aria-label="重试执行任务"
                    icon={<ReloadOutlined />}
                    size="small"
                    tooltip="重试"
                    onClick={() => retryMutation.mutate(record)}
                  />
                ) : null}
                {canManage && record.providerKind !== 'k8s_job_runner' && record.callbackToken ? (
                  <ManagementIconButton
                    aria-label="模拟执行回调"
                    icon={<ApiOutlined />}
                    size="small"
                    tooltip="模拟回调"
                    onClick={() => callbackMutation.mutate(record)}
                  />
                ) : null}
              </Space>
            ),
          },
        ]}
      />
      <Modal
        title={selectedTask ? `任务日志 · ${selectedTask.id}` : '任务日志'}
        open={!!selectedTask}
        onCancel={() => setSelectedTask(null)}
        footer={null}
        width={920}
        destroyOnHidden
      >
        <Descriptions
          items={selectedTask ? [
            { key: 'provider', label: 'Provider', children: selectedTask.providerKind },
            { key: 'status', label: 'Status', children: <StatusTag value={selectedTask.status} /> },
            { key: 'bundle', label: 'Bundle', children: selectedTask.releaseBundleId || '-' },
            { key: 'heartbeat', label: 'Last Heartbeat', children: selectedTask.lastHeartbeatAt ? formatDateTime(selectedTask.lastHeartbeatAt) : '-' },
            { key: 'callback', label: 'Callback Token', children: selectedTask.callbackToken || '-' },
          ] : []}
        />
        {canManage && selectedTask ? (
          <Space style={{ marginBottom: 12 }}>
            {canCancelExecutionTask(selectedTask) ? (
              <Button
                danger
                icon={<StopOutlined />}
                loading={cancelMutation.isPending}
                onClick={() => cancelMutation.mutate(selectedTask)}
                size="small"
              >
                取消任务
              </Button>
            ) : null}
            {canRetryExecutionTask(selectedTask) ? (
              <Button
                icon={<ReloadOutlined />}
                loading={retryMutation.isPending}
                onClick={() => retryMutation.mutate(selectedTask)}
                size="small"
              >
                重新入队
              </Button>
            ) : null}
          </Space>
        ) : null}
        <Card className="soha-management-panel-card" size="small" title="Execution Logs">
          <pre className="soha-json-block">
            {logsQuery.data?.data?.map((item) => `[${item.createdAt}] ${item.logLevel.toUpperCase()} ${item.message}`).join('\n') || 'No logs'}
          </pre>
        </Card>
        <Card className="soha-management-panel-card" size="small" title="Artifacts">
          <pre className="soha-json-block">{JSON.stringify(selectedTask?.artifacts ?? [], null, 2)}</pre>
        </Card>
        <Card className="soha-management-panel-card" size="small" title="Result">
          <pre className="soha-json-block">{JSON.stringify(selectedTask?.result ?? {}, null, 2)}</pre>
        </Card>
      </Modal>
    </div>
  )
}

export function ApprovalPoliciesPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManage = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.approval-policies.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<ApprovalPolicy | null>(null)

  const policiesQuery = useQuery({
    queryKey: ['approval-policies'],
    queryFn: () => api.get<ApiResponse<ApprovalPolicy[]>>('/delivery/approval-policies'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/delivery/approval-policies', values),
    onSuccess: () => {
      message.success('审批策略创建成功')
      queryClient.invalidateQueries({ queryKey: ['approval-policies'] })
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/delivery/approval-policies/${id}`, values),
    onSuccess: () => {
      message.success('审批策略更新成功')
      queryClient.invalidateQueries({ queryKey: ['approval-policies'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/delivery/approval-policies/${id}`),
    onSuccess: () => {
      message.success('审批策略已删除')
      queryClient.invalidateQueries({ queryKey: ['approval-policies'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  return (
    <div className="soha-page">
      <DeliveryTable
        rowKey="id"
        actions={canManage ? (
          <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setModalVisible(true) }}>
            新建策略
          </Button>
        ) : null}
        refreshing={policiesQuery.isFetching}
        onRefresh={() => void policiesQuery.refetch()}
        loading={policiesQuery.isLoading}
        dataSource={policiesQuery.data?.data ?? []}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: 'Key', dataIndex: 'key' },
          { title: '模式', dataIndex: 'mode', render: (value: string) => value || 'single' },
          { title: '审批数', dataIndex: 'requiredApprovals' },
          { title: 'SLA(min)', dataIndex: 'slaMinutes' },
          { title: '角色', dataIndex: 'approverRoles', render: (value: string[]) => value?.join(', ') || '-' },
          { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
          {
            ...tableColumnPresets.action,
            title: '操作',
            dataIndex: 'id',
            render: (_: unknown, record: ApprovalPolicy) => (
              <Space className="soha-row-action-icons" size={2}>
                {canManage ? (
                  <ManagementIconButton
                    aria-label="编辑审批策略"
                    icon={<EditOutlined />}
                    size="small"
                    tooltip="编辑"
                    onClick={() => { setEditing(record); setModalVisible(true) }}
                  />
                ) : null}
                {canManage ? (
                  <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)} placement="topRight">
                    <ManagementIconButton
                      aria-label="删除审批策略"
                      danger
                      icon={<DeleteOutlined />}
                      size="small"
                      tooltip="删除"
                    />
                  </Popconfirm>
                ) : null}
              </Space>
            ),
          },
        ]}
      />
      <Modal title={editing ? '编辑审批策略' : '新建审批策略'} open={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null} destroyOnHidden width={860}>
        <Form
          form={form}
          key={editing?.id ?? 'approval-policy'}
          layout="vertical"
          initialValues={editing ? { ...editing, approverRolesText: (editing.approverRoles ?? []).join(', '), changeWindowText: JSON.stringify(editing.changeWindow ?? {}, null, 2), metadataText: JSON.stringify(editing.metadata ?? {}, null, 2) } : { enabled: true, mode: 'single', requiredApprovals: 1, slaMinutes: 60, changeWindowText: '{}', metadataText: '{}' }}
          onFinish={(values) => {
            let payload: Record<string, unknown>
            try {
              payload = {
                ...values,
                approverRoles: String(values.approverRolesText || '').split(',').map((item) => item.trim()).filter(Boolean),
                changeWindow: parseJSONObject(values.changeWindowText, 'Change Window'),
                metadata: parseJSONObject(values.metadataText, 'Metadata'),
              }
            } catch (err) {
              message.error((err as Error).message)
              return
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
        >
          <Form.Item name="key" label="策略 Key" rules={[{ required: true, message: '请输入策略 Key' }]}><Input /></Form.Item>
          <Form.Item name="name" label="策略名称" rules={[{ required: true, message: '请输入策略名称' }]}><Input /></Form.Item>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Form.Item name="mode" label="模式"><Select options={[{ value: 'single', label: 'single' }, { value: 'multi', label: 'multi' }, { value: 'all', label: 'all' }]} /></Form.Item>
          <Form.Item name="requiredApprovals" label="需要审批数"><Input type="number" /></Form.Item>
          <Form.Item name="slaMinutes" label="SLA(分钟)"><Input type="number" /></Form.Item>
          <Form.Item name="approverRolesText" label="审批角色"><Input placeholder="release-manager, ops-lead" /></Form.Item>
          <Form.Item name="changeWindowText" label="变更窗口(JSON)"><Input.TextArea rows={5} /></Form.Item>
          <Form.Item name="metadataText" label="Metadata(JSON)"><Input.TextArea rows={5} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <div className="soha-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" type="primary" loading={createMutation.isPending || updateMutation.isPending}>保存</Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
