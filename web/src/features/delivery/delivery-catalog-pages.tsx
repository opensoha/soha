import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { Button, Modal, Form, Tag, Toast, Popconfirm, Space, Card, Typography, Descriptions, Empty, Input, Select } from '@douyinfe/semi-ui'
import { IconPlus, IconEdit, IconDelete, IconArrowRight, IconPlay, IconRefresh, IconSend } from '@douyinfe/semi-icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { PageHeader } from '@/components/page-header'
import { useI18n } from '@/i18n'
import {
  createDefaultReleaseDagDefinition,
  normalizeReleaseDagDefinition,
  countReleaseDagNodes,
  type ReleaseDagDefinition,
} from '@/components/release-flow-dag-definition'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, ApplicationEnvironment, BusinessLine, DeliveryEnvironment, WorkflowTemplate } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

function stringifyJSON(value: unknown, fallback: string) {
  return JSON.stringify(value ?? JSON.parse(fallback), null, 2)
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

function parseJSONArray(raw: unknown, field: string) {
  const value = typeof raw === 'string' ? raw.trim() : ''
  if (!value) return []
  try {
    const parsed = JSON.parse(value)
    if (!Array.isArray(parsed)) {
      throw new Error('invalid')
    }
    return parsed
  } catch {
    throw new Error(`${field} 需要是合法 JSON 数组`)
  }
}

interface ApplicationOption {
  id: string
  name: string
  businessLineId?: string
}

interface ApplicationDetail extends ApplicationOption {
  key: string
  group: string
  buildImage?: string
  defaultTag?: string
}

interface BuildRecord {
  id: string
  applicationId: string
  status: string
  createdAt: string
}

interface WorkflowRecord {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  updatedAt: string
}

interface ReleaseRecord {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  createdAt: string
}

interface RolloutHistoryRecord {
  name: string
  namespace: string
  revision: string
  images?: string[]
  replicas: number
  readyReplicas: number
  createdAt?: string
}

const RELEASE_TEMPLATE_CATEGORY_OPTIONS = [
  { value: 'release', label: 'Release Flow' },
  { value: 'verification', label: 'Verification' },
  { value: 'promotion', label: 'Promotion' },
]

const { Text } = Typography

const ReleaseFlowDagEditor = lazy(async () => {
  const mod = await import('@/components/release-flow-dag-editor')
  return { default: mod.ReleaseFlowDagEditor }
})

function matchesBindingTarget(
  target: NonNullable<ApplicationEnvironment['targets']>[number] | undefined,
  clusterId: string,
  namespace: string,
  deploymentName: string,
) {
  if (!target) return false
  return target.clusterId === clusterId
    && target.namespace === namespace
    && target.workloadName === deploymentName
    && target.workloadKind.toLowerCase() === 'deployment'
    && target.enabled !== false
}

function pickLatest<T>(items: T[], matcher: (item: T) => boolean, timeSelector: (item: T) => string) {
  return items
    .filter(matcher)
    .sort((left, right) => new Date(timeSelector(right)).getTime() - new Date(timeSelector(left)).getTime())[0]
}

function findLatestBuild(applicationId: string, builds: BuildRecord[]) {
  return pickLatest(builds, (item) => item.applicationId === applicationId, (item) => item.createdAt)
}

function findLatestWorkflow(binding: ApplicationEnvironment, workflows: WorkflowRecord[]) {
  return pickLatest(
    workflows,
    (item) =>
      item.applicationId === binding.applicationId
        && (binding.targets ?? []).some((target) => matchesBindingTarget(target, item.clusterId, item.namespace, item.deploymentName)),
    (item) => item.updatedAt,
  )
}

function findLatestRelease(binding: ApplicationEnvironment, releases: ReleaseRecord[]) {
  return pickLatest(
    releases,
    (item) =>
      item.applicationId === binding.applicationId
        && (binding.targets ?? []).some((target) => matchesBindingTarget(target, item.clusterId, item.namespace, item.deploymentName)),
    (item) => item.createdAt,
  )
}

function findLatestWorkflowForTarget(target: NonNullable<ApplicationEnvironment['targets']>[number], binding: ApplicationEnvironment, workflows: WorkflowRecord[]) {
  return pickLatest(
    workflows,
    (item) => item.applicationId === binding.applicationId && matchesBindingTarget(target, item.clusterId, item.namespace, item.deploymentName),
    (item) => item.updatedAt,
  )
}

function findLatestReleaseForTarget(target: NonNullable<ApplicationEnvironment['targets']>[number], binding: ApplicationEnvironment, releases: ReleaseRecord[]) {
  return pickLatest(
    releases,
    (item) => item.applicationId === binding.applicationId && matchesBindingTarget(target, item.clusterId, item.namespace, item.deploymentName),
    (item) => item.createdAt,
  )
}

function summarizeLatestActivity(localeCode: 'zh_CN' | 'en_US', build?: BuildRecord, workflow?: WorkflowRecord, release?: ReleaseRecord) {
  if (release?.createdAt) return localeCode === 'zh_CN' ? `最近发布 ${formatDateTime(release.createdAt)}` : `Latest release ${formatDateTime(release.createdAt)}`
  if (workflow?.updatedAt) return localeCode === 'zh_CN' ? `最近工作流 ${formatDateTime(workflow.updatedAt)}` : `Latest workflow ${formatDateTime(workflow.updatedAt)}`
  if (build?.createdAt) return localeCode === 'zh_CN' ? `最近构建 ${formatDateTime(build.createdAt)}` : `Latest build ${formatDateTime(build.createdAt)}`
  return localeCode === 'zh_CN' ? '暂无执行记录' : 'No execution history'
}

export function BusinessLinesPage() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<BusinessLine | null>(null)
  const canManageBusinessLines = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.business-lines.manage')

  const { data, isLoading } = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/business-lines', values),
    onSuccess: () => {
      Toast.success('业务线创建成功')
      queryClient.invalidateQueries({ queryKey: ['business-lines'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/business-lines/${id}`, values),
    onSuccess: () => {
      Toast.success('业务线更新成功')
      queryClient.invalidateQueries({ queryKey: ['business-lines'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/business-lines/${id}`),
    onSuccess: () => {
      Toast.success('业务线已删除')
      queryClient.invalidateQueries({ queryKey: ['business-lines'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const columns: ColumnProps<BusinessLine>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: 'Key', dataIndex: 'key' },
    { title: '描述', dataIndex: 'description', ellipsis: true },
    {
      title: 'Owners',
      dataIndex: 'owners',
      render: (owners: string[]) => owners?.map((item) => <Tag key={item}>{item}</Tag>) ?? '-',
    },
    { title: '排序', dataIndex: 'sortOrder' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: BusinessLine) => (
        <Space>
          {canManageBusinessLines ? <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => { setEditing(record); setModalVisible(true) }} /> : null}
          {canManageBusinessLines ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
            </Popconfirm>
          ) : null}
          {!canManageBusinessLines ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title={t('page.delivery.businessLines.title', 'Business Lines')}
        description={t('page.delivery.businessLines.desc', 'Maintain business-line master data used by applications, environment bindings, and scope grants.')}
        actions={canManageBusinessLines ? <Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>新建业务线</Button> : null}
      />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
      <Modal title={editing ? '编辑业务线' : '新建业务线'} visible={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null}>
        <Form
          onSubmit={(values) => {
            const payload = {
              ...values,
              owners: String(values.owners ?? '').split(',').map((item) => item.trim()).filter(Boolean),
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
          initValues={editing ? { ...editing, owners: editing.owners?.join(', ') } : { enabled: true, sortOrder: 0 }}
        >
          <Form.Input field="key" label="业务线 Key" rules={[{ required: true, message: '请输入业务线 Key' }]} />
          <Form.Input field="name" label="业务线名称" rules={[{ required: true, message: '请输入业务线名称' }]} />
          <Form.Input field="description" label="描述" />
          <Form.Input field="owners" label="Owners" placeholder="alice,bob" />
          <Form.InputNumber field="sortOrder" label="排序" />
          <Form.Switch field="enabled" label="启用" />
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? '更新' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

export function DeliveryEnvironmentsPage() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<DeliveryEnvironment | null>(null)
  const canManageEnvironments = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.environments.manage')

  const { data, isLoading } = useQuery({
    queryKey: ['delivery-environments'],
    queryFn: () => api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery-environments'),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/delivery-environments', values),
    onSuccess: () => {
      Toast.success('环境创建成功')
      queryClient.invalidateQueries({ queryKey: ['delivery-environments'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/delivery-environments/${id}`, values),
    onSuccess: () => {
      Toast.success('环境更新成功')
      queryClient.invalidateQueries({ queryKey: ['delivery-environments'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/delivery-environments/${id}`),
    onSuccess: () => {
      Toast.success('环境已删除')
      queryClient.invalidateQueries({ queryKey: ['delivery-environments'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const columns: ColumnProps<DeliveryEnvironment>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: 'Key', dataIndex: 'key' },
    { title: 'Tier', dataIndex: 'tier' },
    { title: 'Stage', dataIndex: 'stageLevel' },
    { title: '排序', dataIndex: 'sortOrder' },
    { title: '生产', dataIndex: 'isProduction', render: (value: boolean) => <BooleanTag value={value} /> },
    { title: '需审批', dataIndex: 'requiresApproval', render: (value: boolean) => <BooleanTag value={value} /> },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: DeliveryEnvironment) => (
        <Space>
          {canManageEnvironments ? <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => { setEditing(record); setModalVisible(true) }} /> : null}
          {canManageEnvironments ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
            </Popconfirm>
          ) : null}
          {!canManageEnvironments ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title={t('page.delivery.environments.title', 'Environments')}
        description={t('page.delivery.environments.desc', 'Maintain delivery environment master data, including ordering, production flags, and approval requirements.')}
        actions={canManageEnvironments ? <Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>新建环境</Button> : null}
      />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
      <Modal title={editing ? '编辑环境' : '新建环境'} visible={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null}>
        <Form onSubmit={(values) => {
          if (editing) {
            updateMutation.mutate({ id: editing.id, values })
          } else {
            createMutation.mutate(values)
          }
        }} initValues={editing ?? { enabled: true, sortOrder: 0, stageLevel: 0, isProduction: false, requiresApproval: false }}>
          <Form.Input field="key" label="环境 Key" rules={[{ required: true, message: '请输入环境 Key' }]} />
          <Form.Input field="name" label="环境名称" rules={[{ required: true, message: '请输入环境名称' }]} />
          <Form.Input field="tier" label="Tier" />
          <Form.InputNumber field="stageLevel" label="Stage Level" />
          <Form.InputNumber field="sortOrder" label="排序" />
          <Form.Switch field="isProduction" label="生产环境" />
          <Form.Switch field="requiresApproval" label="发布需审批" />
          <Form.Switch field="enabled" label="启用" />
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? '更新' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

export function ApplicationEnvironmentsPage() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<ApplicationEnvironment | null>(null)
  const canManageBindings = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.application-environments.manage')

  const bindingsQuery = useQuery({
    queryKey: ['application-environments'],
    queryFn: () => api.get<ApiResponse<ApplicationEnvironment[]>>('/application-environments'),
  })
  const appsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<ApplicationOption[]>>('/applications'),
  })
  const environmentsQuery = useQuery({
    queryKey: ['delivery-environments'],
    queryFn: () => api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery-environments'),
  })
  const workflowTemplatesQuery = useQuery({
    queryKey: ['workflow-templates'],
    queryFn: () => api.get<ApiResponse<WorkflowTemplate[]>>('/workflow-templates'),
  })

  const appNameMap = useMemo(
    () => Object.fromEntries((appsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [appsQuery.data],
  )
  const environmentNameMap = useMemo(
    () => Object.fromEntries((environmentsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [environmentsQuery.data],
  )

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/application-environments', values),
    onSuccess: () => {
      Toast.success('应用环境绑定创建成功')
      queryClient.invalidateQueries({ queryKey: ['application-environments'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/application-environments/${id}`, values),
    onSuccess: () => {
      Toast.success('应用环境绑定更新成功')
      queryClient.invalidateQueries({ queryKey: ['application-environments'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/application-environments/${id}`),
    onSuccess: () => {
      Toast.success('应用环境绑定已删除')
      queryClient.invalidateQueries({ queryKey: ['application-environments'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const columns: ColumnProps<ApplicationEnvironment>[] = [
    { title: '应用', dataIndex: 'applicationId', render: (value: string) => appNameMap[value] || value },
    { title: '环境', dataIndex: 'environmentId', render: (value: string) => environmentNameMap[value] || value },
    { title: '发布流程模板', dataIndex: 'workflowTemplate', render: (_: WorkflowTemplate, record: ApplicationEnvironment) => record.workflowTemplate?.name || record.workflowTemplateId || '-' },
    { title: '目标数', dataIndex: 'targets', render: (targets: ApplicationEnvironment['targets']) => targets?.length ?? 0 },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: ApplicationEnvironment) => (
        <Space>
          {canManageBindings ? <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => { setEditing(record); setModalVisible(true) }} /> : null}
          {canManageBindings ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
            </Popconfirm>
          ) : null}
          {!canManageBindings ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title={t('page.delivery.bindings.title', 'Application Environment Bindings')}
        description={t('page.delivery.bindings.desc', 'Bind applications, environments, and release targets so workflows and deployments can be linked.')}
        actions={canManageBindings ? <Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>新建绑定</Button> : null}
      />
      <AdminTable columns={columns} dataSource={bindingsQuery.data?.data ?? []} rowKey="id" loading={bindingsQuery.isLoading} />
      <Modal title={editing ? '编辑应用环境绑定' : '新建应用环境绑定'} visible={modalVisible} onCancel={() => { setModalVisible(false); setEditing(null) }} footer={null} width={760}>
        <Form
          onSubmit={(values) => {
            let payload: Record<string, unknown>
            try {
              payload = {
                ...values,
                buildPolicy: parseJSONObject(values.buildPolicy, 'Build Policy'),
                releasePolicy: parseJSONObject(values.releasePolicy, 'Release Policy'),
                targets: parseJSONArray(values.targets, 'Release Targets'),
              }
            } catch (err) {
              Toast.error((err as Error).message)
              return
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
          initValues={editing ? {
            applicationId: editing.applicationId,
            environmentId: editing.environmentId,
            workflowTemplateId: editing.workflowTemplateId,
            buildPolicy: stringifyJSON(editing.buildPolicy, '{}'),
            releasePolicy: stringifyJSON(editing.releasePolicy, '{}'),
            targets: stringifyJSON(editing.targets, '[]'),
          } : { buildPolicy: '{}', releasePolicy: '{}', targets: '[]' }}
        >
          <Form.Select
            field="applicationId"
            label="应用"
            optionList={(appsQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
            rules={[{ required: true, message: '请选择应用' }]}
          />
          <Form.Select
            field="environmentId"
            label="环境"
            optionList={(environmentsQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
            rules={[{ required: true, message: '请选择环境' }]}
          />
          <Form.Select
            field="workflowTemplateId"
            label="发布流程模板"
            optionList={(workflowTemplatesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))}
          />
          <Form.TextArea field="buildPolicy" label="Build Policy(JSON)" rows={4} />
          <Form.TextArea field="releasePolicy" label="Release Policy(JSON)" rows={4} />
          <Form.TextArea field="targets" label="Release Targets(JSON Array)" rows={8} />
          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>取消</Button>
            <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? '更新' : '创建'}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}

export function ReleaseBoardPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const businessLinesQuery = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
  })
  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<ApplicationOption[]>>('/applications'),
  })
  const environmentsQuery = useQuery({
    queryKey: ['delivery-environments'],
    queryFn: () => api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery-environments'),
  })
  const bindingsQuery = useQuery({
    queryKey: ['application-environments'],
    queryFn: () => api.get<ApiResponse<ApplicationEnvironment[]>>('/application-environments'),
  })
  const buildsQuery = useQuery({
    queryKey: ['builds'],
    queryFn: () => api.get<ApiResponse<BuildRecord[]>>('/builds'),
  })
  const workflowsQuery = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.get<ApiResponse<WorkflowRecord[]>>('/workflows'),
  })
  const releasesQuery = useQuery({
    queryKey: ['releases'],
    queryFn: () => api.get<ApiResponse<ReleaseRecord[]>>('/releases'),
  })

  const businessLineMap = useMemo(
    () => Object.fromEntries((businessLinesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [businessLinesQuery.data],
  )
  const environments = useMemo(
    () => [...(environmentsQuery.data?.data ?? [])].sort((left, right) => left.sortOrder - right.sortOrder),
    [environmentsQuery.data],
  )
  const rows = useMemo(() => {
    const bindings = bindingsQuery.data?.data ?? []
    return (applicationsQuery.data?.data ?? []).map((application) => {
      const bindingMap = Object.fromEntries(
        bindings
          .filter((item) => item.applicationId === application.id)
          .map((item) => [item.environmentId, item]),
      )
      return {
        ...application,
        __bindings: bindingMap,
      }
    })
  }, [applicationsQuery.data, bindingsQuery.data])

  const builds = buildsQuery.data?.data ?? []
  const workflows = workflowsQuery.data?.data ?? []
  const releases = releasesQuery.data?.data ?? []

  const columns: ColumnProps<any>[] = [
    { title: t('common.application', 'Application'), dataIndex: 'name' },
    { title: localeCode === 'zh_CN' ? '业务线' : 'Business Line', dataIndex: 'businessLineId', render: (value: string) => businessLineMap[value] || value || '-' },
    ...environments.map((environment) => ({
      title: environment.name,
      dataIndex: environment.id,
      render: (_: unknown, record: any) => {
        const binding = record.__bindings?.[environment.id] as ApplicationEnvironment | undefined
        if (!binding) return <Text type="tertiary">{localeCode === 'zh_CN' ? '未绑定' : 'Unbound'}</Text>
        if (!binding.targets?.length) return <Tag color="orange">{localeCode === 'zh_CN' ? '无目标' : 'No Targets'}</Tag>
        const latestBuild = findLatestBuild(binding.applicationId, builds)
        const latestWorkflow = findLatestWorkflow(binding, workflows)
        const latestRelease = findLatestRelease(binding, releases)
        return (
          <div className="flex flex-col gap-1">
            {binding.targets.slice(0, 2).map((target) => (
              <Tag key={target.id} color="blue">{`${target.clusterId} / ${target.namespace} / ${target.workloadName}`}</Tag>
            ))}
            {binding.targets.length > 2 ? <Text type="tertiary" size="small">{`+${binding.targets.length - 2} ${localeCode === 'zh_CN' ? '更多' : 'more'}`}</Text> : null}
            <Text type="tertiary" size="small">{`${localeCode === 'zh_CN' ? '模板' : 'Template'}: ${binding.workflowTemplate?.name || '-'}`}</Text>
            <div className="flex flex-wrap gap-1">
              {latestBuild ? <Tag color="cyan">{`Build: ${latestBuild.status}`}</Tag> : <Tag color="grey">Build: -</Tag>}
              {latestWorkflow ? <Tag color="purple">{`Workflow: ${latestWorkflow.status}`}</Tag> : <Tag color="grey">Workflow: -</Tag>}
              {latestRelease ? <Tag color="green">{`Release: ${latestRelease.status}`}</Tag> : <Tag color="grey">Release: -</Tag>}
            </div>
            <Text type="tertiary" size="small">{summarizeLatestActivity(localeCode, latestBuild, latestWorkflow, latestRelease)}</Text>
            <Button
              icon={<IconArrowRight />}
              theme="borderless"
              type="primary"
              size="small"
              onClick={() => navigate(`/application-environments/${binding.id}`)}
            >
              {localeCode === 'zh_CN' ? '环境详情' : 'Environment Detail'}
            </Button>
          </div>
        )
      },
    })),
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.releaseBoard.title', 'Release Board')} description={t('page.releaseBoard.desc', 'Inspect target bindings by application and environment as the entry point for workflow and deployment linkage.')} />
      <Card>
        <AdminTable columns={columns} dataSource={rows} rowKey="id" loading={applicationsQuery.isLoading || environmentsQuery.isLoading || bindingsQuery.isLoading || businessLinesQuery.isLoading || buildsQuery.isLoading || workflowsQuery.isLoading || releasesQuery.isLoading} />
      </Card>
    </div>
  )
}

export function ApplicationEnvironmentDetailPage() {
  const { t, localeCode } = useI18n()
  const { applicationEnvironmentId } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [selectedTargetId, setSelectedTargetId] = useState<string>('')
  const [imageTag, setImageTag] = useState('')
  const [releaseName, setReleaseName] = useState('')
  const [containerName, setContainerName] = useState('')
  const [rollbackRevision, setRollbackRevision] = useState('')
  const canTriggerWorkflow = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.workflows.trigger')
  const canTriggerRelease = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.releases.trigger')
  const canRollbackDeployment = hasPermission(permissionSnapshotQuery.data?.data, 'platform.deployment.rollback')

  const bindingQuery = useQuery({
    queryKey: ['application-environment', applicationEnvironmentId],
    queryFn: () => api.get<ApiResponse<ApplicationEnvironment>>(`/application-environments/${applicationEnvironmentId}`),
    enabled: !!applicationEnvironmentId,
  })
  const businessLinesQuery = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
  })
  const applicationQuery = useQuery({
    queryKey: ['application', bindingQuery.data?.data?.applicationId],
    queryFn: () => api.get<ApiResponse<ApplicationDetail>>(`/applications/${bindingQuery.data!.data.applicationId}`),
    enabled: !!bindingQuery.data?.data?.applicationId,
  })
  const environmentsQuery = useQuery({
    queryKey: ['delivery-environments'],
    queryFn: () => api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery-environments'),
  })
  const buildsQuery = useQuery({
    queryKey: ['builds'],
    queryFn: () => api.get<ApiResponse<BuildRecord[]>>('/builds'),
  })
  const workflowsQuery = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.get<ApiResponse<WorkflowRecord[]>>('/workflows'),
  })
  const releasesQuery = useQuery({
    queryKey: ['releases'],
    queryFn: () => api.get<ApiResponse<ReleaseRecord[]>>('/releases'),
  })

  const binding = bindingQuery.data?.data
  const businessLineMap = useMemo(
    () => Object.fromEntries((businessLinesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [businessLinesQuery.data],
  )
  const environmentMap = useMemo(
    () => Object.fromEntries((environmentsQuery.data?.data ?? []).map((item) => [item.id, item])),
    [environmentsQuery.data],
  )
  const latestBuild = binding ? findLatestBuild(binding.applicationId, buildsQuery.data?.data ?? []) : undefined
  const latestWorkflow = binding ? findLatestWorkflow(binding, workflowsQuery.data?.data ?? []) : undefined
  const latestRelease = binding ? findLatestRelease(binding, releasesQuery.data?.data ?? []) : undefined

  useEffect(() => {
    if (!binding?.targets?.length) {
      setSelectedTargetId('')
      return
    }
    if (selectedTargetId && binding.targets.some((target) => target.id === selectedTargetId)) {
      return
    }
    setSelectedTargetId(binding.targets[0].id)
  }, [binding, selectedTargetId])

  const selectedTarget = useMemo(
    () => binding?.targets?.find((target) => target.id === selectedTargetId) ?? binding?.targets?.[0],
    [binding, selectedTargetId],
  )

  useEffect(() => {
    if (!selectedTarget) {
      setContainerName('')
      return
    }
    setContainerName(selectedTarget.containerName || '')
  }, [selectedTarget])

  useEffect(() => {
    if (!applicationQuery.data?.data?.defaultTag || imageTag) return
    setImageTag(applicationQuery.data.data.defaultTag)
  }, [applicationQuery.data, imageTag])

  const rolloutHistoryQuery = useQuery({
    queryKey: ['deployment-rollouts', selectedTarget?.clusterId, selectedTarget?.namespace, selectedTarget?.workloadName],
    queryFn: () =>
      api.get<ApiResponse<RolloutHistoryRecord[]>>(
        `/clusters/${selectedTarget!.clusterId}/workloads/deployments/${selectedTarget!.workloadName}/rollouts?namespace=${encodeURIComponent(selectedTarget!.namespace)}`,
      ),
    enabled: !!selectedTarget && selectedTarget.workloadKind.toLowerCase() === 'deployment',
  })

  useEffect(() => {
    const rollouts = rolloutHistoryQuery.data?.data ?? []
    if (rollbackRevision && rollouts.some((item) => item.revision === rollbackRevision)) {
      return
    }
    if (rollouts.length > 1) {
      setRollbackRevision(rollouts[1].revision)
      return
    }
    setRollbackRevision('')
  }, [rollbackRevision, rolloutHistoryQuery.data])

  const targetRows = useMemo(() => {
    if (!binding) return []
    return (binding.targets ?? []).map((target) => ({
      ...target,
      latestWorkflow: findLatestWorkflowForTarget(target, binding, workflowsQuery.data?.data ?? []),
      latestRelease: findLatestReleaseForTarget(target, binding, releasesQuery.data?.data ?? []),
    }))
  }, [binding, workflowsQuery.data, releasesQuery.data])

  const targetColumns: ColumnProps<(typeof targetRows)[number]>[] = [
    { title: t('common.cluster', 'Cluster'), dataIndex: 'clusterId' },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace' },
    { title: localeCode === 'zh_CN' ? '工作负载' : 'Workload', dataIndex: 'workloadName', render: (_: string, record) => `${record.workloadKind} / ${record.workloadName}` },
    { title: t('common.container', 'Container'), dataIndex: 'containerName', render: (value: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '启用' : 'Enabled', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
    { title: 'Workflow', dataIndex: 'latestWorkflow', render: (_: unknown, record) => <StatusTag value={record.latestWorkflow?.status || 'unknown'} /> },
    { title: 'Release', dataIndex: 'latestRelease', render: (_: unknown, record) => <StatusTag value={record.latestRelease?.status || 'unknown'} /> },
    {
      title: localeCode === 'zh_CN' ? '最近动作' : 'Latest Activity',
      dataIndex: 'latestRelease',
      render: (_: unknown, record) =>
        summarizeLatestActivity(localeCode, undefined, record.latestWorkflow, record.latestRelease),
    },
  ]

  const workflowMutation = useMutation({
    mutationFn: async () => {
      if (!binding || !selectedTarget) throw new Error(t('common.selectTarget', 'Select a release target'))
      return api.post('/workflows/trigger', {
        applicationId: binding.applicationId,
        workflowName: binding.workflowTemplate?.key || binding.workflowTemplate?.name || 'build-release-verify',
        clusterId: selectedTarget.clusterId,
        namespace: selectedTarget.namespace,
        deploymentName: selectedTarget.workloadName,
        triggerBuild: true,
        triggerRelease: false,
      })
    },
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? '工作流已触发' : 'Workflow triggered')
      queryClient.invalidateQueries({ queryKey: ['workflows'] })
      queryClient.invalidateQueries({ queryKey: ['application-environment', applicationEnvironmentId] })
      queryClient.invalidateQueries({ queryKey: ['release-board'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const releaseMutation = useMutation({
    mutationFn: async () => {
      if (!binding || !selectedTarget) throw new Error(t('common.selectTarget', 'Select a release target'))
      const effectiveImageTag = imageTag.trim() || applicationQuery.data?.data?.defaultTag || ''
      if (!effectiveImageTag) {
        throw new Error(localeCode === 'zh_CN' ? '请提供 Image Tag，或先在应用中配置默认 Tag' : 'Provide an image tag, or configure a default tag on the application first')
      }
      return api.post('/releases/trigger', {
        applicationId: binding.applicationId,
        clusterId: selectedTarget.clusterId,
        namespace: selectedTarget.namespace,
        deploymentName: selectedTarget.workloadName,
        containerName: containerName.trim() || selectedTarget.containerName || '',
        imageTag: effectiveImageTag,
        releaseName: releaseName.trim(),
      })
    },
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? '发布已触发' : 'Release triggered')
      queryClient.invalidateQueries({ queryKey: ['releases'] })
      queryClient.invalidateQueries({ queryKey: ['application-environment', applicationEnvironmentId] })
      queryClient.invalidateQueries({ queryKey: ['release-board'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const rollbackMutation = useMutation({
    mutationFn: async () => {
      if (!selectedTarget) throw new Error(t('common.selectTarget', 'Select a release target'))
      if (!rollbackRevision) throw new Error(t('common.selectRevision', 'Select a revision to roll back to'))
      return api.post(
        `/clusters/${selectedTarget.clusterId}/workloads/deployments/rollback`,
        {
          namespace: selectedTarget.namespace,
          name: selectedTarget.workloadName,
          revision: rollbackRevision,
        },
      )
    },
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? '回滚已触发' : 'Rollback triggered')
      queryClient.invalidateQueries({ queryKey: ['deployment-rollouts', selectedTarget?.clusterId, selectedTarget?.namespace, selectedTarget?.workloadName] })
      queryClient.invalidateQueries({ queryKey: ['application-environment', applicationEnvironmentId] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  if (bindingQuery.isLoading) {
    return (
      <div className="kc-page">
        <PageHeader title={localeCode === 'zh_CN' ? '环境详情' : 'Environment Detail'} description={localeCode === 'zh_CN' ? '加载应用环境绑定详情。' : 'Loading application-environment binding details.'} />
        <Card><Text type="tertiary">{t('common.loading', 'Loading...')}</Text></Card>
      </div>
    )
  }

  if (!binding) {
    return (
      <div className="kc-page">
        <PageHeader
          title={localeCode === 'zh_CN' ? '环境详情' : 'Environment Detail'}
          description={localeCode === 'zh_CN' ? '当前绑定不存在或已被删除。' : 'The current binding does not exist or has been removed.'}
          actions={<Button onClick={() => navigate('/release-board')}>{localeCode === 'zh_CN' ? '返回发布看板' : 'Back to Release Board'}</Button>}
        />
        <Card><Empty description={localeCode === 'zh_CN' ? '未找到应用环境绑定' : 'Application-environment binding not found'} /></Card>
      </div>
    )
  }

  const application = applicationQuery.data?.data
  const environment = environmentMap[binding.environmentId]
  const selectedTargetIsDeployment = selectedTarget?.workloadKind.toLowerCase() === 'deployment'
  const targetOptions = (binding.targets ?? []).map((target) => ({
    value: target.id,
    label: `${target.clusterId} / ${target.namespace} / ${target.workloadName}`,
  }))
  const rolloutOptions = (rolloutHistoryQuery.data?.data ?? [])
    .filter((item) => item.revision)
    .map((item) => ({
      value: item.revision,
      label: `${item.revision}${item.createdAt ? ` · ${formatDateTime(item.createdAt)}` : ''}`,
    }))
  const releaseImagePreview = application?.buildImage && (imageTag.trim() || application.defaultTag)
    ? `${application.buildImage}:${imageTag.trim() || application.defaultTag}`
    : ''

  return (
    <div className="kc-page">
      <PageHeader
        title={`${application?.name || binding.applicationId} / ${environment?.name || binding.environmentKey || binding.environmentId}`}
        description={localeCode === 'zh_CN' ? '查看单个应用环境绑定的工作流模板、发布目标和最新执行状态。' : 'Inspect the workflow template, release targets, and latest execution state for a single application-environment binding.'}
        actions={<Button onClick={() => navigate('/release-board')}>{localeCode === 'zh_CN' ? '返回发布看板' : 'Back to Release Board'}</Button>}
      />
      <Card>
        <Descriptions data={[
          { key: localeCode === 'zh_CN' ? '业务线' : 'Business Line', value: businessLineMap[binding.businessLineId || ''] || binding.businessLineId || '-' },
          { key: localeCode === 'zh_CN' ? '环境 Key' : 'Environment Key', value: binding.environmentKey || environment?.key || '-' },
          { key: localeCode === 'zh_CN' ? '发布流程模板' : 'Workflow Template', value: binding.workflowTemplate?.name || '-' },
          { key: localeCode === 'zh_CN' ? '模板分类' : 'Template Category', value: binding.workflowTemplate?.category || '-' },
          { key: localeCode === 'zh_CN' ? '最新 Build' : 'Latest Build', value: <StatusTag value={latestBuild?.status || 'unknown'} /> },
          { key: localeCode === 'zh_CN' ? '最新 Workflow' : 'Latest Workflow', value: <StatusTag value={latestWorkflow?.status || 'unknown'} /> },
          { key: localeCode === 'zh_CN' ? '最新 Release' : 'Latest Release', value: <StatusTag value={latestRelease?.status || 'unknown'} /> },
          { key: localeCode === 'zh_CN' ? '最近活动' : 'Latest Activity', value: summarizeLatestActivity(localeCode, latestBuild, latestWorkflow, latestRelease) },
        ]} />
      </Card>
      <Card title={localeCode === 'zh_CN' ? '交付动作' : 'Delivery Actions'}>
        <div className="kc-delivery-action-grid">
          <div className="kc-delivery-action-block">
            <Text strong>{localeCode === 'zh_CN' ? '发布目标' : 'Release Target'}</Text>
            <Select
              value={selectedTarget?.id}
              optionList={targetOptions}
              onChange={(value) => setSelectedTargetId(String(value))}
              placeholder={localeCode === 'zh_CN' ? '选择目标 deployment' : 'Select target deployment'}
            />
            <Text type="tertiary" size="small">
              {binding.workflowTemplate?.name
                ? `${localeCode === 'zh_CN' ? '工作流模板' : 'Workflow Template'}: ${binding.workflowTemplate.name}${binding.workflowTemplate.category ? ` / ${binding.workflowTemplate.category}` : ''}`
                : localeCode === 'zh_CN' ? '当前未绑定工作流模板，将使用默认流程名' : 'No workflow template is bound. The default workflow name will be used.'}
            </Text>
          </div>
          <div className="kc-delivery-action-block">
            <Text strong>{localeCode === 'zh_CN' ? '触发工作流' : 'Trigger Workflow'}</Text>
            <Text type="tertiary" size="small">{localeCode === 'zh_CN' ? '生成一条 workflow run，只做流程编排，不直接改 deployment 镜像。' : 'Create a workflow run for orchestration only without directly changing the deployment image.'}</Text>
            <Button
              icon={<IconPlay />}
              theme="solid"
              onClick={() => workflowMutation.mutate()}
              loading={workflowMutation.isPending}
              disabled={!canTriggerWorkflow || !selectedTarget}
            >
              {localeCode === 'zh_CN' ? '触发工作流' : 'Trigger Workflow'}
            </Button>
          </div>
          <div className="kc-delivery-action-block">
            <Text strong>{localeCode === 'zh_CN' ? '触发发布' : 'Trigger Release'}</Text>
            <Input value={imageTag} onChange={setImageTag} placeholder={localeCode === 'zh_CN' ? 'Image Tag，默认取应用默认 Tag' : 'Image tag, defaulting to the application default tag'} />
            <Input value={releaseName} onChange={setReleaseName} placeholder={localeCode === 'zh_CN' ? 'Release Name，可留空自动生成' : 'Release name, leave empty to auto-generate'} />
            <Input value={containerName} onChange={setContainerName} placeholder={localeCode === 'zh_CN' ? 'Container Name，可留空使用绑定值' : 'Container name, leave empty to use the binding value'} />
            {releaseImagePreview ? <Text type="tertiary" size="small">{`${localeCode === 'zh_CN' ? '目标镜像' : 'Target Image'}: ${releaseImagePreview}`}</Text> : null}
            {!selectedTargetIsDeployment ? <Text type="warning">{localeCode === 'zh_CN' ? '当前目标不是 Deployment，暂不支持直接触发发布。' : 'The current target is not a Deployment, so direct release triggering is not supported yet.'}</Text> : null}
            <Button
              icon={<IconSend />}
              theme="solid"
              type="primary"
              onClick={() => releaseMutation.mutate()}
              loading={releaseMutation.isPending}
              disabled={!canTriggerRelease || !selectedTarget || !selectedTargetIsDeployment}
            >
              {localeCode === 'zh_CN' ? '触发发布' : 'Trigger Release'}
            </Button>
          </div>
          <div className="kc-delivery-action-block">
            <Text strong>{localeCode === 'zh_CN' ? '回滚' : 'Rollback'}</Text>
            <Select
              value={rollbackRevision || undefined}
              optionList={rolloutOptions}
              onChange={(value) => setRollbackRevision(String(value))}
              placeholder={t('common.selectRevision', 'Select a revision to roll back to')}
              loading={rolloutHistoryQuery.isLoading}
              disabled={!selectedTarget || rolloutOptions.length === 0}
            />
            <Text type="tertiary" size="small">
              {!selectedTargetIsDeployment
                ? (localeCode === 'zh_CN' ? '当前目标不是 Deployment，暂不支持回滚。' : 'The current target is not a Deployment, so rollback is not supported yet.')
                : rolloutOptions.length > 0
                  ? (localeCode === 'zh_CN' ? '回滚会直接对目标 deployment 发起 Kubernetes rollback。' : 'Rollback will issue a Kubernetes rollback directly against the target deployment.')
                  : (localeCode === 'zh_CN' ? '当前没有可用的 rollout history。' : 'No rollout history is currently available.')}
            </Text>
            <Button
              icon={<IconRefresh />}
              theme="outline"
              type="danger"
              onClick={() => rollbackMutation.mutate()}
              loading={rollbackMutation.isPending}
              disabled={!canRollbackDeployment || !selectedTarget || !selectedTargetIsDeployment || !rollbackRevision}
            >
              {localeCode === 'zh_CN' ? '回滚到所选版本' : 'Rollback to Selected Revision'}
            </Button>
          </div>
        </div>
      </Card>
      <Card title={localeCode === 'zh_CN' ? '发布目标' : 'Release Targets'}>
        <AdminTable columns={targetColumns} dataSource={targetRows} rowKey="id" pagination={false} />
      </Card>
      <Card title={localeCode === 'zh_CN' ? 'Workflow Template 定义' : 'Workflow Template Definition'}>
        {binding.workflowTemplate?.definition ? (
          <pre className="kc-json-block">{JSON.stringify(binding.workflowTemplate.definition, null, 2)}</pre>
        ) : (
          <Empty description={localeCode === 'zh_CN' ? '当前未配置工作流模板定义' : 'No workflow template definition is configured'} />
        )}
      </Card>
      <Card title={localeCode === 'zh_CN' ? '构建与发布策略' : 'Build and Release Policy'}>
        <Descriptions data={[
          { key: 'Build Policy', value: <pre className="kc-json-block">{JSON.stringify(binding.buildPolicy ?? {}, null, 2)}</pre> },
          { key: 'Release Policy', value: <pre className="kc-json-block">{JSON.stringify(binding.releasePolicy ?? {}, null, 2)}</pre> },
        ]} />
      </Card>
    </div>
  )
}

export function WorkflowTemplatesPage() {
  const { t, localeCode } = useI18n()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<WorkflowTemplate | null>(null)
  const [editorDefinition, setEditorDefinition] = useState<ReleaseDagDefinition>(createDefaultReleaseDagDefinition())
  const canManageWorkflowTemplates = hasPermission(permissionSnapshotQuery.data?.data, 'delivery.workflow-templates.manage')

  const { data, isLoading } = useQuery({
    queryKey: ['workflow-templates'],
    queryFn: () => api.get<ApiResponse<WorkflowTemplate[]>>('/workflow-templates'),
  })

  useEffect(() => {
    if (!modalVisible) return
    setEditorDefinition(normalizeReleaseDagDefinition(editing?.definition))
  }, [editing, modalVisible])

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/workflow-templates', values),
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? 'DAG 发布流程模板创建成功' : 'DAG release flow template created')
      queryClient.invalidateQueries({ queryKey: ['workflow-templates'] })
      setModalVisible(false)
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/workflow-templates/${id}`, values),
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? 'DAG 发布流程模板更新成功' : 'DAG release flow template updated')
      queryClient.invalidateQueries({ queryKey: ['workflow-templates'] })
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => Toast.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/workflow-templates/${id}`),
    onSuccess: () => {
      Toast.success(localeCode === 'zh_CN' ? 'DAG 发布流程模板已删除' : 'DAG release flow template deleted')
      queryClient.invalidateQueries({ queryKey: ['workflow-templates'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const previewDefinition = useMemo(() => JSON.stringify(editorDefinition, null, 2), [editorDefinition])

  const columns: ColumnProps<WorkflowTemplate>[] = [
    { title: t('common.name', 'Name'), dataIndex: 'name' },
    { title: 'Key', dataIndex: 'key' },
    { title: localeCode === 'zh_CN' ? '分类' : 'Category', dataIndex: 'category', render: (value: string) => value || 'release' },
    {
      title: localeCode === 'zh_CN' ? '模式' : 'Mode',
      dataIndex: 'definition',
      render: (value: WorkflowTemplate['definition']) => normalizeReleaseDagDefinition(value).mode,
    },
    {
      title: localeCode === 'zh_CN' ? '节点数' : 'Nodes',
      dataIndex: 'definition',
      render: (value: WorkflowTemplate['definition']) => countReleaseDagNodes(value),
    },
    { title: localeCode === 'zh_CN' ? '启用' : 'Enabled', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} /> },
    { ...tableColumnPresets.datetime, title: t('common.updatedAt', 'Updated At'), dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: t('common.actions', 'Actions'),
      dataIndex: 'id',
      render: (_: unknown, record: WorkflowTemplate) => (
        <Space>
          {canManageWorkflowTemplates ? <Button icon={<IconEdit />} theme="borderless" size="small" onClick={() => { setEditing(record); setModalVisible(true) }} /> : null}
          {canManageWorkflowTemplates ? (
            <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <Button icon={<IconDelete />} theme="borderless" type="danger" size="small" />
            </Popconfirm>
          ) : null}
          {!canManageWorkflowTemplates ? '-' : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title={t('page.workflowTemplates.title', 'Release Flow Templates')}
        description={t('page.workflowTemplates.desc', 'Maintain reusable DAG-based release flow templates with the React Flow canvas, including serial, parallel, and auto-layout patterns.')}
        actions={canManageWorkflowTemplates ? <Button icon={<IconPlus />} theme="solid" onClick={() => { setEditing(null); setModalVisible(true) }}>{localeCode === 'zh_CN' ? '新建模板' : 'New Template'}</Button> : null}
      />
      <Card className="kc-scope-hint-card">
        <Text type="tertiary">
          {t('page.workflowTemplates.hint', 'The canvas uses React Flow and dagre for auto-layout. Only fixed node types are allowed so backend execution and audit remain controllable.')}
        </Text>
      </Card>
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} />
      <Modal
        title={editing ? (localeCode === 'zh_CN' ? '编辑 DAG 发布流程模板' : 'Edit DAG Release Flow Template') : (localeCode === 'zh_CN' ? '新建 DAG 发布流程模板' : 'New DAG Release Flow Template')}
        visible={modalVisible}
        onCancel={() => { setModalVisible(false); setEditing(null) }}
        footer={null}
        width={1440}
      >
        <Form
          onSubmit={(values) => {
            const payload = {
              ...values,
              category: values.category || 'release',
              definition: editorDefinition,
            }
            if (editing) {
              updateMutation.mutate({ id: editing.id, values: payload })
            } else {
              createMutation.mutate(payload)
            }
          }}
          initValues={editing ? { ...editing, category: editing.category || 'release' } : { enabled: true, category: 'release' }}
        >
          <div className="kc-delivery-action-grid">
            <div className="kc-delivery-action-block">
              <Form.Input field="key" label={localeCode === 'zh_CN' ? '模板 Key' : 'Template Key'} rules={[{ required: true, message: localeCode === 'zh_CN' ? '请输入模板 Key' : 'Enter the template key' }]} />
            </div>
            <div className="kc-delivery-action-block">
              <Form.Input field="name" label={localeCode === 'zh_CN' ? '模板名称' : 'Template Name'} rules={[{ required: true, message: localeCode === 'zh_CN' ? '请输入模板名称' : 'Enter the template name' }]} />
            </div>
            <div className="kc-delivery-action-block">
              <Form.Input field="description" label={localeCode === 'zh_CN' ? '描述' : 'Description'} />
            </div>
            <div className="kc-delivery-action-block">
              <Form.Select field="category" label={localeCode === 'zh_CN' ? '分类' : 'Category'} optionList={RELEASE_TEMPLATE_CATEGORY_OPTIONS} />
              <Form.Switch field="enabled" label={localeCode === 'zh_CN' ? '启用' : 'Enabled'} />
            </div>
          </div>

          <Card className="kc-template-editor-card" title={localeCode === 'zh_CN' ? 'DAG 编排画布' : 'DAG Composer'}>
            <Suspense fallback={<Card><Text type="tertiary">{t('common.loading', 'Loading...')}</Text></Card>}>
              <ReleaseFlowDagEditor
                key={editing?.id ?? 'new-release-dag'}
                initialDefinition={modalVisible ? normalizeReleaseDagDefinition(editing?.definition) : createDefaultReleaseDagDefinition()}
                onChange={setEditorDefinition}
              />
            </Suspense>
          </Card>

          <Card className="kc-flow-stage-card" title={localeCode === 'zh_CN' ? 'JSON 预览' : 'JSON Preview'}>
            <pre className="kc-json-block">{previewDefinition}</pre>
          </Card>

          <div className="kc-form-actions">
            <Button onClick={() => setModalVisible(false)}>{t('common.cancel', 'Cancel')}</Button>
            <Button htmlType="submit" theme="solid" loading={createMutation.isPending || updateMutation.isPending}>
              {editing ? t('common.update', 'Update') : t('common.create', 'Create')}
            </Button>
          </div>
        </Form>
      </Modal>
    </div>
  )
}
