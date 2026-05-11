import { useEffect, useMemo, useState } from 'react'
import { App, Button, Card, Descriptions, Form, Input, Modal, Popconfirm, Select, Space, Switch, Tabs, Tag, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType, TabsProps } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type {
  ApiResponse,
  ApplicationEnvironment,
  BuildSource,
  Cluster,
  DeliveryApplication,
  DeliveryEnvironment,
  DeliveryTargetCandidate,
  WorkflowTemplate,
  BusinessLine,
} from '@/types'

const { Text } = Typography

type ColumnProps<T> = TableColumnsType<T>[number]

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

function defaultBuildSources() {
  return [
    {
      id: '',
      name: 'Repository Dockerfile',
      type: 'repo_dockerfile' as const,
      enabled: true,
      isDefault: true,
      buildImage: '',
      defaultTag: '',
      config: { contextDir: '.', dockerfilePath: 'Dockerfile', builderKind: 'docker' },
    },
  ] satisfies BuildSource[]
}

function useApplicationManagementState() {
  const { applicationId } = useParams()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const [appForm] = Form.useForm<Record<string, unknown>>()
  const [bindingForm] = Form.useForm<Record<string, unknown>>()
  const [appModalVisible, setAppModalVisible] = useState(false)
  const [bindingModalVisible, setBindingModalVisible] = useState(false)
  const [editingApp, setEditingApp] = useState<DeliveryApplication | null>(null)
  const [editingBinding, setEditingBinding] = useState<ApplicationEnvironment | null>(null)
  const [buildSources, setBuildSources] = useState<BuildSource[]>([])
  const [selectedApplicationId, setSelectedApplicationId] = useState<string>('')

  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<DeliveryApplication[]>>('/applications'),
  })
  const businessLinesQuery = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
  })
  const bindingsQuery = useQuery({
    queryKey: ['application-environments'],
    queryFn: () => api.get<ApiResponse<ApplicationEnvironment[]>>('/application-environments'),
  })
  const environmentsQuery = useQuery({
    queryKey: ['delivery-environments'],
    queryFn: () => api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery-environments'),
  })
  const workflowTemplatesQuery = useQuery({
    queryKey: ['workflow-templates'],
    queryFn: () => api.get<ApiResponse<WorkflowTemplate[]>>('/workflow-templates'),
  })
  const clustersQuery = useQuery({
    queryKey: ['clusters'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
  })

  useEffect(() => {
    const appList = applicationsQuery.data?.data ?? []
    if (applicationId && appList.some((item) => item.id === applicationId)) {
      if (selectedApplicationId !== applicationId) {
        setSelectedApplicationId(applicationId)
      }
      return
    }
    if (!selectedApplicationId && appList.length > 0) {
      setSelectedApplicationId(appList[0].id)
    }
  }, [applicationId, applicationsQuery.data, selectedApplicationId])

  const selectedApplication = useMemo(
    () => (applicationsQuery.data?.data ?? []).find((item) => item.id === selectedApplicationId) ?? null,
    [applicationsQuery.data, selectedApplicationId],
  )
  const filteredBindings = useMemo(
    () => (bindingsQuery.data?.data ?? []).filter((item) => !selectedApplicationId || item.applicationId === selectedApplicationId),
    [bindingsQuery.data, selectedApplicationId],
  )
  const environmentMap = useMemo(
    () => Object.fromEntries((environmentsQuery.data?.data ?? []).map((item) => [item.id, item])),
    [environmentsQuery.data],
  )
  const businessLineMap = useMemo(
    () => Object.fromEntries((businessLinesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [businessLinesQuery.data],
  )
  const workflowTemplateMap = useMemo(
    () => Object.fromEntries((workflowTemplatesQuery.data?.data ?? []).map((item) => [item.id, item])),
    [workflowTemplatesQuery.data],
  )

  const selectedClusterId = Form.useWatch('targetClusterId', bindingForm) as string | undefined
  const selectedNamespace = Form.useWatch('targetNamespace', bindingForm) as string | undefined

  const targetCandidatesQuery = useQuery({
    queryKey: ['management-target-candidates', selectedClusterId, selectedNamespace],
    queryFn: () => api.get<ApiResponse<DeliveryTargetCandidate[]>>(`/application-environments/target-candidates?clusterId=${encodeURIComponent(selectedClusterId || '')}&namespace=${encodeURIComponent(selectedNamespace || '')}`),
    enabled: !!selectedClusterId && !!selectedNamespace && bindingModalVisible,
  })

  const canCreateApplication = hasPermission(permissionSnapshot, 'delivery.application.create')
  const canUpdateApplication = hasPermission(permissionSnapshot, 'delivery.application.update')
  const canDeleteApplication = hasPermission(permissionSnapshot, 'delivery.application.delete')
  const canManageBindings = hasPermission(permissionSnapshot, 'delivery.application-environments.manage')

  const createAppMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post<ApiResponse<DeliveryApplication>>('/applications', values),
    onSuccess: (result: ApiResponse<DeliveryApplication>) => {
      message.success('应用创建成功')
      void queryClient.invalidateQueries({ queryKey: ['applications'] })
      setSelectedApplicationId(result.data?.id || '')
      setAppModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateAppMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/applications/${id}`, values),
    onSuccess: () => {
      message.success('应用更新成功')
      void queryClient.invalidateQueries({ queryKey: ['applications'] })
      setAppModalVisible(false)
      setEditingApp(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteAppMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/applications/${id}`),
    onSuccess: () => {
      message.success('应用已删除')
      void queryClient.invalidateQueries({ queryKey: ['applications'] })
      void queryClient.invalidateQueries({ queryKey: ['application-environments'] })
      setSelectedApplicationId('')
    },
    onError: (err: Error) => message.error(err.message),
  })

  const createBindingMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/application-environments', values),
    onSuccess: () => {
      message.success('环境绑定创建成功')
      void queryClient.invalidateQueries({ queryKey: ['application-environments'] })
      setBindingModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateBindingMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/application-environments/${id}`, values),
    onSuccess: () => {
      message.success('环境绑定更新成功')
      void queryClient.invalidateQueries({ queryKey: ['application-environments'] })
      setBindingModalVisible(false)
      setEditingBinding(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteBindingMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/application-environments/${id}`),
    onSuccess: () => {
      message.success('环境绑定已删除')
      void queryClient.invalidateQueries({ queryKey: ['application-environments'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  return {
    navigate,
    appForm,
    bindingForm,
    appModalVisible,
    setAppModalVisible,
    bindingModalVisible,
    setBindingModalVisible,
    editingApp,
    setEditingApp,
    editingBinding,
    setEditingBinding,
    buildSources,
    setBuildSources,
    applicationsQuery,
    businessLinesQuery,
    bindingsQuery,
    environmentsQuery,
    workflowTemplatesQuery,
    clustersQuery,
    targetCandidatesQuery,
    selectedApplicationId,
    setSelectedApplicationId,
    selectedApplication,
    filteredBindings,
    environmentMap,
    businessLineMap,
    workflowTemplateMap,
    canCreateApplication,
    canUpdateApplication,
    canDeleteApplication,
    canManageBindings,
    createAppMutation,
    updateAppMutation,
    deleteAppMutation,
    createBindingMutation,
    updateBindingMutation,
    deleteBindingMutation,
  }
}

function ApplicationManagementCore() {
  const state = useApplicationManagementState()

  const bindingColumns: ColumnProps<ApplicationEnvironment>[] = [
    { title: '环境', dataIndex: 'environmentId', render: (value: string) => state.environmentMap[value]?.name || value },
    { title: '发布流程模板', dataIndex: 'workflowTemplateId', render: (value?: string) => state.workflowTemplateMap[value || '']?.name || value || '-' },
    { title: '构建来源', dataIndex: 'buildPolicy', render: (value: ApplicationEnvironment['buildPolicy']) => value?.sourceId || '-' },
    { title: '选择器', dataIndex: 'resourceSelector', render: (value: ApplicationEnvironment['resourceSelector']) => Object.entries(value?.matchLabels ?? {}).map(([key, labelValue]) => `${key}=${labelValue}`).join(', ') || '-' },
    { title: '目标数', dataIndex: 'targets', render: (targets?: ApplicationEnvironment['targets']) => targets?.length ?? 0 },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: ApplicationEnvironment) => (
        <Space>
          {state.canManageBindings ? (
            <Button
              icon={<EditOutlined />}
              type="text"
              size="small"
              onClick={() => {
                state.setEditingBinding(record)
                state.setBindingModalVisible(true)
              }}
            />
          ) : null}
          {state.canManageBindings ? (
            <Popconfirm title="确认删除？" onConfirm={() => state.deleteBindingMutation.mutate(record.id)}>
              <Button icon={<DeleteOutlined />} type="text" danger size="small" />
            </Popconfirm>
          ) : null}
        </Space>
      ),
    },
  ]

  const tabs: TabsProps['items'] = state.selectedApplication ? [
    {
      key: 'bindings',
      label: '环境选择与发布',
      children: (
        <Card className="kc-detail-card" extra={state.canManageBindings ? <Button icon={<PlusOutlined />} type="primary" onClick={() => { state.setEditingBinding(null); state.setBindingModalVisible(true) }}>新建绑定</Button> : null}>
          <Descriptions
            column={1}
            items={[
              { key: 'group', label: '分组', children: state.selectedApplication.group || '-' },
              { key: 'language', label: '语言', children: state.selectedApplication.language || '-' },
              { key: 'status', label: '状态', children: <StatusTag value={state.selectedApplication.enabled ? 'enabled' : 'disabled'} /> },
            ]}
          />
          <div style={{ marginTop: 16 }}>
            <AdminTable
              title={<Text strong>环境绑定列表</Text>}
              columns={bindingColumns}
              dataSource={state.filteredBindings}
              rowKey="id"
              loading={state.bindingsQuery.isLoading}
            />
          </div>
        </Card>
      ),
    },
    {
      key: 'pipelines',
      label: '构建与流水线配置',
      children: (
        <Card className="kc-detail-card">
          <Descriptions
            column={1}
            items={[
              {
                key: 'build-sources',
                label: '构建来源',
                children: (
                  <Space wrap>
                    {(state.selectedApplication.buildSources ?? []).map((item) => (
                      <Tag key={item.id || item.name}>{`${item.name} · ${summarizeBuildSource(item)}`}</Tag>
                    ))}
                  </Space>
                ),
              },
              {
                key: 'workflow-templates',
                label: '已接入发布流程',
                children: (
                  <Space wrap>
                    {Array.from(new Set(state.filteredBindings.map((item) => item.workflowTemplateId).filter(Boolean))).map((templateID) => (
                      <Tag key={templateID}>{state.workflowTemplateMap[templateID || '']?.name || templateID}</Tag>
                    ))}
                  </Space>
                ),
              },
            ]}
          />
        </Card>
      ),
    },
  ] : []

  const selectedTargetCandidate = (record: Record<string, unknown>) =>
    (state.targetCandidatesQuery.data?.data ?? []).find((item) => `${item.clusterId}/${item.namespace}/${item.workloadName}` === record.targetWorkload)

  const appModal = (
    <Modal title={state.editingApp ? '编辑应用' : '新建应用'} open={state.appModalVisible} onCancel={() => { state.setAppModalVisible(false); state.setEditingApp(null) }} footer={null} destroyOnHidden width={860}>
      <Form
        form={state.appForm}
        key={state.editingApp?.id ?? 'application-management-app'}
        layout="vertical"
        initialValues={state.editingApp ? { ...state.editingApp, enabled: state.editingApp.enabled } : { enabled: true, language: 'go', defaultBranch: 'main' }}
        onFinish={(values) => {
          const payload = { ...values, buildSources: state.buildSources }
          if (state.editingApp) {
            state.updateAppMutation.mutate({ id: state.editingApp.id, values: payload })
          } else {
            state.createAppMutation.mutate(payload)
          }
        }}
      >
        <Form.Item name="name" label="应用名称" rules={[{ required: true, message: '请输入应用名称' }]}><Input /></Form.Item>
        <Form.Item name="key" label="应用 Key" rules={[{ required: true, message: '请输入应用 Key' }]}><Input /></Form.Item>
        <Form.Item name="group" label="应用分组" rules={[{ required: true, message: '请输入应用分组' }]}><Input /></Form.Item>
        <Form.Item name="businessLineId" label="业务线" rules={[{ required: true, message: '请选择业务线' }]}><Select options={(state.businessLinesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} /></Form.Item>
        <Form.Item name="language" label="语言"><Select options={[{ value: 'go', label: 'Go' }, { value: 'java', label: 'Java' }, { value: 'node', label: 'Node.js' }, { value: 'python', label: 'Python' }]} /></Form.Item>
        <Form.Item name="repositoryPath" label="代码仓库路径"><Input /></Form.Item>
        <Form.Item name="defaultBranch" label="默认分支"><Input /></Form.Item>
        <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        <div className="kc-form-actions">
          <Button onClick={() => state.setAppModalVisible(false)}>取消</Button>
          <Button htmlType="submit" type="primary" loading={state.createAppMutation.isPending || state.updateAppMutation.isPending}>保存</Button>
        </div>
      </Form>
    </Modal>
  )

  const bindingModal = (
    <Modal title={state.editingBinding ? '编辑环境绑定' : '新建环境绑定'} open={state.bindingModalVisible} onCancel={() => { state.setBindingModalVisible(false); state.setEditingBinding(null) }} footer={null} destroyOnHidden width={760}>
        <Form
          form={state.bindingForm}
          key={state.editingBinding?.id ?? `binding-${state.selectedApplicationId || 'new'}`}
          layout="vertical"
          initialValues={state.editingBinding ? {
            environmentId: state.editingBinding.environmentId,
            workflowTemplateId: state.editingBinding.workflowTemplateId,
            buildSourceId: state.editingBinding.buildPolicy?.sourceId,
            actionKind: state.editingBinding.releasePolicy?.actionKind || 'deploy',
            requiresApproval: state.editingBinding.releasePolicy?.requiresApproval,
            targetClusterId: state.editingBinding.targets?.[0]?.clusterId,
            targetNamespace: state.editingBinding.targets?.[0]?.namespace,
            targetWorkload: state.editingBinding.targets?.[0] ? `${state.editingBinding.targets[0].clusterId}/${state.editingBinding.targets[0].namespace}/${state.editingBinding.targets[0].workloadName}` : undefined,
            targetContainer: state.editingBinding.targets?.[0]?.containerName,
            resourceSelectorText: JSON.stringify(state.editingBinding.resourceSelector?.matchLabels ?? {}, null, 2),
          } : {
            actionKind: 'deploy',
            requiresApproval: false,
            resourceSelectorText: '{}',
          }}
          onFinish={(values) => {
            if (!state.selectedApplication) return
            const target = selectedTargetCandidate(values)
            const matchLabels = parseJSONObject(values.resourceSelectorText, '选择器标签')
            const payload: Record<string, unknown> = {
              applicationId: state.selectedApplication.id,
              environmentId: values.environmentId,
              workflowTemplateId: values.workflowTemplateId,
              buildPolicy: {
                sourceId: values.buildSourceId,
                refType: 'branch',
                refValue: '',
                imageTagMode: 'input',
                imageTagTemplate: '',
                variables: {},
                buildArgs: {},
              },
              releasePolicy: {
                actionKind: values.actionKind || 'deploy',
                requiresApproval: Boolean(values.requiresApproval),
                approverRoles: [],
                autoRollback: false,
                rolloutTimeoutSeconds: 300,
                verificationMode: 'workflow',
              },
              resourceSelector: {
                matchLabels,
              },
              targets: target ? [{
                clusterId: target.clusterId,
                namespace: target.namespace,
                targetKind: 'k8s_workload',
                executorKind: 'k8s_job_runner',
                workloadKind: target.workloadKind,
                workloadName: target.workloadName,
                containerName: String(values.targetContainer || ''),
                metadata: {},
                enabled: true,
              }] : [],
            }
            if (state.editingBinding) {
              state.updateBindingMutation.mutate({ id: state.editingBinding.id, values: payload })
            } else {
              state.createBindingMutation.mutate(payload)
            }
          }}
        >
          <Form.Item label="应用">
            <Input value={state.selectedApplication?.name || ''} disabled />
          </Form.Item>
          <Form.Item name="environmentId" label="环境" rules={[{ required: true, message: '请选择环境' }]}>
            <Select options={(state.environmentsQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item name="workflowTemplateId" label="发布流程模板">
            <Select allowClear options={(state.workflowTemplatesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item name="buildSourceId" label="构建来源">
            <Select allowClear options={(state.selectedApplication?.buildSources ?? []).map((item) => ({ value: item.id, label: item.name || summarizeBuildSource(item) }))} />
          </Form.Item>
          <Form.Item name="actionKind" label="动作">
            <Select options={[{ value: 'deploy', label: 'deploy' }, { value: 'release', label: 'release' }]} />
          </Form.Item>
          <Form.Item name="requiresApproval" label="需要审批" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="targetClusterId" label="目标集群">
            <Select allowClear options={(state.clustersQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item name="targetNamespace" label="目标命名空间">
            <Input />
          </Form.Item>
          <Form.Item name="targetWorkload" label="目标服务 / Deployment">
            <Select
              allowClear
              showSearch
              options={(state.targetCandidatesQuery.data?.data ?? []).map((item) => ({
                value: `${item.clusterId}/${item.namespace}/${item.workloadName}`,
                label: `${item.workloadName} · ${item.namespace}`,
              }))}
            />
          </Form.Item>
          <Form.Item name="targetContainer" label="容器">
            <Input />
          </Form.Item>
          <Form.Item name="resourceSelectorText" label="资源选择器标签(JSON)">
            <Input.TextArea rows={4} placeholder={`{\n  "app": "erp-front"\n}`} />
          </Form.Item>
        <div className="kc-form-actions">
          <Button onClick={() => state.setBindingModalVisible(false)}>取消</Button>
          <Button htmlType="submit" type="primary" loading={state.createBindingMutation.isPending || state.updateBindingMutation.isPending}>保存</Button>
        </div>
      </Form>
    </Modal>
  )

  return (
    <div className="kc-page">
      <PageHeader
        title={state.selectedApplication?.name || '应用管理详情'}
        description="维护当前应用的环境选择、发布绑定和流水线配置。"
        actions={<Button onClick={() => state.navigate('/application-management')}>返回应用列表</Button>}
      />
      {state.selectedApplication ? (
        <Card className="kc-ai-hub-lane" title={state.selectedApplication.name}>
          <Tabs items={tabs} />
        </Card>
      ) : (
        <Card className="kc-ai-hub-lane">
          <Text type="secondary">未找到当前应用，请返回应用列表重新选择。</Text>
        </Card>
      )}
      {appModal}
      {bindingModal}
    </div>
  )
}

export function ApplicationManagementPage() {
  const state = useApplicationManagementState()

  const appColumns: ColumnProps<DeliveryApplication>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (value: string, record: DeliveryApplication) => (
        <Button
          className="kc-application-management-name"
          type="link"
          onClick={() => state.navigate(`/application-management/${record.id}`)}
        >
          {value}
        </Button>
      ),
    },
    { title: '分组', dataIndex: 'group' },
    { title: '业务线', dataIndex: 'businessLineId', render: (value: string) => state.businessLineMap[value] || value || '-' },
    { title: '语言', dataIndex: 'language' },
    { title: '构建源', dataIndex: 'buildSources', render: (value: BuildSource[]) => summarizeBuildSource((value ?? []).find((item) => item.isDefault)) },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: DeliveryApplication) => (
        <Space>
          <Button type="text" size="small" onClick={() => state.navigate(`/application-management/${record.id}`)}>
            打开
          </Button>
          {state.canUpdateApplication ? (
            <Button
              icon={<EditOutlined />}
              type="text"
              size="small"
              onClick={() => {
                state.setEditingApp(record)
                state.setBuildSources(record.buildSources ?? [])
                state.setAppModalVisible(true)
              }}
            />
          ) : null}
          {state.canDeleteApplication ? (
            <Popconfirm title="确认删除？" onConfirm={() => state.deleteAppMutation.mutate(record.id)}>
              <Button icon={<DeleteOutlined />} type="text" danger size="small" />
            </Popconfirm>
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title="应用管理"
        description="负责新增应用、维护分组、环境选择和发布流水线配置。"
        actions={state.canCreateApplication ? <Button icon={<PlusOutlined />} type="primary" onClick={() => { state.setEditingApp(null); state.setBuildSources(defaultBuildSources()); state.setAppModalVisible(true) }}>新建应用</Button> : null}
      />
      <Card className="kc-ai-hub-lane" title="应用列表">
        <AdminTable
          columns={appColumns}
          dataSource={state.applicationsQuery.data?.data ?? []}
          rowKey="id"
          loading={state.applicationsQuery.isLoading}
        />
      </Card>
    </div>
  )
}

export function ApplicationManagementDetailPage() {
  return <ApplicationManagementCore />
}
