import { AdminTable } from '@/components/admin-table'
import { ManagementIconButton } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { AISettingsPage } from '@/features/settings'
import { ApiError } from '@/services/api-error'
import { useAuthStore } from '@/stores/auth-store'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { createUUID } from '@/utils/uuid'
import {
  DeleteOutlined,
  EditOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  RobotOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  App,
  Button,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tabs,
  Typography,
} from 'antd'
import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { capabilityTaskMutations } from '../../capability-tasks/mutations'
import '../../copilot-pages.css'
import { getAIWorkbenchPathForMode, getAIWorkbenchPathForSession } from '../../workbench/navigation'
import { observeApi } from '../api'
import { observeKeys } from '../keys'
import { inspectionTaskPayload, observeMutations, policyFormValuesFromRecord } from '../mutations'
import { observeQueries } from '../queries'
import type {
  AnalysisProfile,
  AutomationPolicy,
  AutomationPolicyFormValues,
  InspectionRun,
  InspectionTask,
  InspectionTaskFormValues,
} from '../types'

const { Text } = Typography

const INSPECTION_CHECK_OPTIONS = [
  { value: 'cluster_health', label: 'Cluster Health' },
  { value: 'alert_pressure', label: 'Alert Pressure' },
  { value: 'audit_denials', label: 'Audit Denials' },
  { value: 'resource_pressure', label: 'Resource Pressure' },
  { value: 'delivery_risk', label: 'Delivery Risk' },
]

const AUTOMATION_ANALYSIS_KIND_OPTIONS = [
  { value: 'root_cause', label: 'Root Cause' },
  { value: 'performance', label: 'Performance' },
  { value: 'trace', label: 'Trace' },
  { value: 'inspection_review', label: 'Inspection Review' },
]

const AUTOMATION_TRIGGER_TYPE_OPTIONS = [{ value: 'alert_webhook', label: 'Alert Webhook' }]

const AUTOMATION_REMEDIATION_POLICY_OPTIONS = [
  { value: 'suggest_only', label: 'Suggest Only' },
  { value: 'require_approval', label: 'Require Approval' },
  { value: 'disabled', label: 'Disabled' },
]

const AUTOMATION_SEVERITY_OPTIONS = [
  { value: 'critical', label: 'critical' },
  { value: 'warning', label: 'warning' },
  { value: 'info', label: 'info' },
]

const AUTOMATION_STATUS_OPTIONS = [
  { value: 'firing', label: 'firing' },
  { value: 'resolved', label: 'resolved' },
]

function defaultInspectionTaskValues(): InspectionTaskFormValues {
  return {
    id: createUUID(),
    mode: 'legacy',
    triggerKind: 'schedule',
    planJSON: JSON.stringify({ goal: '', steps: [], verificationSteps: [] }, null, 2),
    title: '',
    scopeType: 'platform',
    clusterId: '',
    namespace: '',
    checks: ['cluster_health', 'alert_pressure', 'audit_denials'],
    enabled: true,
    intervalMinutes: 30,
    analysisProfileId: '',
  }
}

function defaultAutomationPolicyValues(): AutomationPolicyFormValues {
  return {
    name: '',
    triggerType: 'alert_webhook',
    analysisKinds: ['root_cause'],
    agentProviderId: 'internal',
    analysisProfileId: 'default',
    remediationPolicy: 'suggest_only',
    dedupWindowSeconds: 900,
    cooldownSeconds: 900,
    enabled: true,
    triggerSeverity: [],
    triggerStatus: ['firing'],
    triggerMinDurationSeconds: 120,
    triggerLabelKey: '',
    triggerLabelValue: '',
    triggerTimeRangeMinutes: 60,
    approvalRequired: false,
    approvalRoles: [],
  }
}

export function AIOperationsPage() {
  const userId = useAuthStore((state) => state.user?.userId ?? '')
  return <AIOperationsContent key={userId} userId={userId} />
}

function AIOperationsContent({ userId }: { userId: string }) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [taskForm] = Form.useForm<InspectionTaskFormValues>()
  const [policyForm] = Form.useForm<AutomationPolicyFormValues>()
  const planValidation = useOperationMutation(capabilityTaskMutations.validate(), {
    label: ['校验巡检计划', 'Validate inspection plan'],
    target: () => '当前巡检计划',
    readOnly: true,
  })
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canViewAI = hasPermission(
    readableQueryData(permissionSnapshotQuery)?.data,
    'observe.ai.view',
  )
  const canUseChat = hasPermission(
    readableQueryData(permissionSnapshotQuery)?.data,
    'observe.ai.chat',
  )
  const canRunInspection = hasPermission(
    readableQueryData(permissionSnapshotQuery)?.data,
    'observe.ai.inspection.run',
  )
  const permissionSnapshot = readableQueryData(permissionSnapshotQuery)?.data
  const canCreateInspection = hasPermission(permissionSnapshot, 'observe.ai.inspection.create')
  const canUpdateInspection = hasPermission(permissionSnapshot, 'observe.ai.inspection.update')
  const canDeleteInspection = hasPermission(permissionSnapshot, 'observe.ai.inspection.delete')
  const canCreateSessionFromRun = canViewAI && canUseChat
  const canUpdateAISettings = hasPermission(permissionSnapshot, 'settings.ai.update')
  const requestedView = searchParams.get('view')
  const requestedInspectionRunId = searchParams.get('inspectionRunId')?.trim() ?? ''
  const [activeView, setActiveView] = useState<'tasks' | 'runs' | 'policies' | 'profiles'>(
    requestedView === 'runs'
      ? 'runs'
      : requestedView === 'policies'
        ? 'policies'
        : requestedView === 'profiles'
          ? 'profiles'
          : 'tasks',
  )
  const [taskModalOpen, setTaskModalOpen] = useState(false)
  const [editingTask, setEditingTask] = useState<InspectionTask | null>(null)
  const [policyModalOpen, setPolicyModalOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<AutomationPolicy | null>(null)
  const tasksQuery = useQuery(observeQueries.operations.tasks())
  const runsQuery = useQuery(observeQueries.operations.runs())
  const policiesQuery = useQuery(observeQueries.operations.policies(canUpdateAISettings))
  const catalogQuery = useQuery(observeQueries.operations.catalog())
  const createSessionMutation = useOperationMutation(
    {
      ...observeMutations.operations.createSession(),
      onSuccess: (response) => {
        void queryClient.invalidateQueries({ queryKey: observeKeys.overview.sessions() })
        navigate(getAIWorkbenchPathForSession(response))
      },
    },
    { label: ['创建复盘会话', 'Create review session'], target: (id) => id },
  )
  const createTaskMutation = useOperationMutation(
    {
      ...observeMutations.operations.createTask(),
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: observeKeys.operations.tasks() })
        setTaskModalOpen(false)
        setEditingTask(null)
        taskForm.resetFields()
      },
    },
    {
      label: ['创建巡检任务', 'Create inspection task'],
      target: (v) => v.title || v.id || '当前巡检任务',
    },
  )
  const updateTaskMutation = useOperationMutation(
    {
      ...observeMutations.operations.updateTask(),
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: observeKeys.operations.tasks() })
        setTaskModalOpen(false)
        setEditingTask(null)
        taskForm.resetFields()
      },
    },
    { label: ['更新巡检任务', 'Update inspection task'], target: (v) => v.taskId },
  )
  const deleteTaskMutation = useOperationMutation(
    {
      ...observeMutations.operations.deleteTask(),
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: observeKeys.operations.tasks() })
        await queryClient.invalidateQueries({ queryKey: observeKeys.operations.runs() })
      },
    },
    { label: ['删除巡检任务', 'Delete inspection task'], target: (id) => id },
  )
  const createPolicyMutation = useOperationMutation(
    {
      ...observeMutations.operations.createPolicy(),
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: observeKeys.operations.policies() })
        setPolicyModalOpen(false)
        setEditingPolicy(null)
        policyForm.resetFields()
      },
    },
    {
      label: ['创建自动化策略', 'Create automation policy'],
      target: (v) => v.name || '当前自动化策略',
    },
  )
  const updatePolicyMutation = useOperationMutation(
    {
      ...observeMutations.operations.updatePolicy(),
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: observeKeys.operations.policies() })
        setPolicyModalOpen(false)
        setEditingPolicy(null)
        policyForm.resetFields()
      },
    },
    { label: ['更新自动化策略', 'Update automation policy'], target: (v) => v.policyId },
  )
  const deletePolicyMutation = useOperationMutation(
    {
      ...observeMutations.operations.deletePolicy(),
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: observeKeys.operations.policies() })
      },
    },
    { label: ['删除自动化策略', 'Delete automation policy'], target: (id) => id },
  )
  const executeMutation = useOperationMutation(
    {
      ...observeMutations.operations.executeTask(),
      onSuccess: () => {
        void queryClient.invalidateQueries({ queryKey: observeKeys.operations.runs() })
        void queryClient.invalidateQueries({ queryKey: observeKeys.operations.tasks() })
      },
    },
    {
      label: ['执行巡检', 'Run inspection'],
      target: (v) => (typeof v === 'string' ? v : v.taskId),
    },
  )

  const blocked = [
    createSessionMutation,
    createTaskMutation,
    updateTaskMutation,
    deleteTaskMutation,
    createPolicyMutation,
    updatePolicyMutation,
    deletePolicyMutation,
    executeMutation,
    planValidation,
  ].some((m) => m.blocked)
  const tasksDenied =
    queryAccessDenied(tasksQuery) ||
    queryAccessDenied(createTaskMutation) ||
    queryAccessDenied(updateTaskMutation)
  const policiesDenied =
    queryAccessDenied(policiesQuery) ||
    queryAccessDenied(createPolicyMutation) ||
    queryAccessDenied(updatePolicyMutation)
  useEffect(() => {
    if (tasksDenied || !(editingTask ? canUpdateInspection : canCreateInspection)) {
      setTaskModalOpen(false)
      setEditingTask(null)
      taskForm.resetFields()
    }
  }, [tasksDenied, editingTask, canUpdateInspection, canCreateInspection, taskForm])
  useEffect(() => {
    if (policiesDenied || !canUpdateAISettings) {
      setPolicyModalOpen(false)
      setEditingPolicy(null)
      policyForm.resetFields()
    }
  }, [policiesDenied, canUpdateAISettings, policyForm])
  const tasks = readableQueryData(tasksQuery) ?? []
  const runs = readableQueryData(runsQuery) ?? []
  const policies = readableQueryData(policiesQuery) ?? []
  const profiles: AnalysisProfile[] = readableQueryData(catalogQuery)?.analysisProfiles ?? []
  const agentProviders = readableQueryData(catalogQuery)?.agentProviders ?? []
  const profileOptions = profiles
    .filter((item) => item.enabled)
    .map((item) => ({ value: item.id, label: `${item.name} (${item.mode})` }))
  const agentProviderOptions = agentProviders
    .filter((item) => item.enabled)
    .map((item) => ({
      value: item.id,
      label: `${item.name}${item.supportsAsync ? ' / async' : ' / inline'}`,
    }))
  const watchedScopeType = Form.useWatch('scopeType', taskForm)
  const watchedInspectionMode = Form.useWatch('mode', taskForm)
  const watchedTriggerKind = Form.useWatch('triggerKind', taskForm)
  async function executeInspection(task: InspectionTask) {
    if (blocked || tasksDenied || !canRunInspection) return
    if (!task.capabilityPlan) {
      executeMutation.mutate(task.id)
      return
    }
    const storageKey = `soha:inspection:${userId}:${task.id}`
    try {
      if (!userId || !task.revision) throw new Error('缺少用户或注册版本，请刷新后重试')
      const saved = localStorage.getItem(storageKey)
      const request = saved
        ? (JSON.parse(saved) as { idempotencyKey: string; expectedRevision: number })
        : { idempotencyKey: createUUID(), expectedRevision: task.revision }
      if (
        typeof request.idempotencyKey !== 'string' ||
        !request.idempotencyKey ||
        !Number.isSafeInteger(request.expectedRevision) ||
        request.expectedRevision < 1
      )
        throw new Error('原请求无法读取，请先核对巡检运行记录')
      localStorage.setItem(storageKey, JSON.stringify(request))
      await executeMutation.mutateAsync({ taskId: task.id, ...request })
      localStorage.removeItem(storageKey)
      setActiveView('runs')
    } catch (error) {
      void message.error(error instanceof Error ? error.message : '无法执行巡检')
    }
  }
  async function validateInspectionPlan(values: InspectionTaskFormValues) {
    const input = inspectionTaskPayload(values)
    if (!input.capabilityPlan) throw new Error('需要能力计划')
    const validation = await planValidation.mutateAsync({
      idempotencyKey: 'inspection-registration',
      plan: input.capabilityPlan,
      aiClientId: input.aiClientId,
      skillId: input.skillId,
    })
    if (!validation.data.valid)
      throw new Error(validation.data.issues.map((issue) => issue.message).join('；'))
  }
  const taskSaving = createTaskMutation.isPending || updateTaskMutation.isPending
  const policySaving = createPolicyMutation.isPending || updatePolicyMutation.isPending

  useEffect(() => {
    if (requestedView === 'runs' || requestedInspectionRunId) {
      setActiveView('runs')
      return
    }
    if (requestedView === 'policies') {
      setActiveView('policies')
      return
    }
    if (requestedView === 'profiles') {
      setActiveView('profiles')
    }
  }, [requestedInspectionRunId, requestedView])

  const openCreateTask = async () => {
    if (blocked || tasksDenied || !canCreateInspection) return
    createTaskMutation.reset()
    updateTaskMutation.reset()
    try {
      if (!userId) throw new Error('缺少当前用户，请重新登录')
      const pendingKey = `soha:inspection-create:${userId}`
      const pendingID = localStorage.getItem(pendingKey)
      if (pendingID) {
        try {
          const original = await observeApi.operations.task(pendingID)
          openEditTask(original)
          localStorage.removeItem(pendingKey)
          void message.info('已找回上次创建的巡检任务，请核对配置')
          return
        } catch (error) {
          if (!(error instanceof ApiError && error.status === 404)) throw error
        }
      }
      setEditingTask(null)
      planValidation.reset()
      taskForm.resetFields()
      taskForm.setFieldsValue({
        ...defaultInspectionTaskValues(),
        ...(pendingID ? { id: pendingID } : {}),
      })
      setTaskModalOpen(true)
    } catch (error) {
      void message.error(error instanceof Error ? error.message : '无法核对原巡检任务')
    }
  }
  const openEditTask = (task: InspectionTask) => {
    if (blocked || tasksDenied || !canUpdateInspection) return
    createTaskMutation.reset()
    updateTaskMutation.reset()
    setEditingTask(task)
    planValidation.reset()
    taskForm.resetFields()
    taskForm.setFieldsValue({
      id: task.id,
      expectedRevision: task.revision,
      mode: task.capabilityPlan ? 'capability' : 'legacy',
      planJSON: JSON.stringify(
        task.capabilityPlan ?? { goal: '', steps: [], verificationSteps: [] },
        null,
        2,
      ),
      triggerKind: task.trigger?.kind ?? 'schedule',
      alertRuleId: task.trigger?.alertRuleId,
      maxEventAgeSeconds: task.trigger?.maxEventAgeSeconds ?? 3600,
      aiClientId: task.aiClientId,
      skillId: task.skillId,
      metadata: task.metadata,
      title: task.title,
      scopeType: task.scopeType || 'platform',
      clusterId: task.clusterId || '',
      namespace: task.namespace || '',
      checks: task.checks ?? [],
      enabled: task.enabled,
      intervalMinutes: task.intervalMinutes || 30,
      analysisProfileId: String(task.metadata?.analysisProfileId ?? ''),
    })
    setTaskModalOpen(true)
  }
  const closeTaskModal = () => {
    if (blocked) return
    setTaskModalOpen(false)
    setEditingTask(null)
    taskForm.resetFields()
  }
  const submitTaskForm = async () => {
    if (blocked || tasksDenied) return
    try {
      const values = await taskForm.validateFields()
      const payload = {
        ...values,
        id: editingTask?.id ?? taskForm.getFieldValue('id'),
        expectedRevision: editingTask?.revision,
        metadata: editingTask?.metadata,
      }
      if (values.mode === 'capability' && !(editingTask?.capabilityPlan && !values.enabled))
        await validateInspectionPlan(values)
      if (editingTask) updateTaskMutation.mutate({ taskId: editingTask.id, values: payload })
      else {
        if (!userId || !payload.id) throw new Error('缺少用户或任务标识，请刷新后重试')
        const pendingKey = `soha:inspection-create:${userId}`
        localStorage.setItem(pendingKey, payload.id)
        await createTaskMutation.mutateAsync(payload)
        localStorage.removeItem(pendingKey)
      }
    } catch (error) {
      if (error instanceof Error) void message.error(error.message)
    }
  }
  const openCreatePolicy = () => {
    if (blocked || policiesDenied || !canUpdateAISettings) return
    createPolicyMutation.reset()
    updatePolicyMutation.reset()
    policyForm.resetFields()
    setEditingPolicy(null)
    policyForm.setFieldsValue(defaultAutomationPolicyValues())
    setPolicyModalOpen(true)
  }
  const openEditPolicy = (policy: AutomationPolicy) => {
    if (blocked || policiesDenied || !canUpdateAISettings) return
    createPolicyMutation.reset()
    updatePolicyMutation.reset()
    policyForm.resetFields()
    setEditingPolicy(policy)
    policyForm.setFieldsValue(policyFormValuesFromRecord(policy))
    setPolicyModalOpen(true)
  }
  const closePolicyModal = () => {
    if (blocked) return
    setPolicyModalOpen(false)
    setEditingPolicy(null)
    policyForm.resetFields()
  }
  const submitPolicyForm = async () => {
    if (blocked || policiesDenied || !canUpdateAISettings) return
    const values = await policyForm.validateFields()
    if (editingPolicy) {
      updatePolicyMutation.mutate({ policyId: editingPolicy.id, values })
    } else {
      createPolicyMutation.mutate(values)
    }
  }

  return (
    <div className="soha-page">
      {!taskModalOpen && (
        <>
          {createTaskMutation.feedback}
          {updateTaskMutation.feedback}
          {planValidation.feedback}
        </>
      )}
      {!policyModalOpen && (
        <>
          {createPolicyMutation.feedback}
          {updatePolicyMutation.feedback}
        </>
      )}
      {deleteTaskMutation.feedback}
      {deletePolicyMutation.feedback}
      {executeMutation.feedback}
      {createSessionMutation.feedback}
      <ReadFeedback query={catalogQuery} label="AI 能力目录" />
      <Tabs
        activeKey={activeView}
        className="soha-resource-tabs is-header-only"
        onChange={(value) => setActiveView(value as typeof activeView)}
        items={[
          { key: 'tasks', label: '巡检任务' },
          { key: 'runs', label: '巡检运行' },
          { key: 'policies', label: '自动化策略' },
          { key: 'profiles', label: '分析模板' },
        ]}
      />

      {activeView === 'tasks' ? (
        <AdminTable
          title="巡检任务"
          headerExtra={
            <Button
              size="small"
              type="primary"
              icon={<PlusOutlined />}
              onClick={openCreateTask}
              disabled={blocked || !canCreateInspection}
              title={canCreateInspection ? undefined : '缺少 observe.ai.inspection.create 权限'}
            >
              新建任务
            </Button>
          }
          shellClassName="soha-management-table-shell"
          columnSettingIconOnly
          columnSettingPlacement="header"
          rowKey="id"
          dataSource={tasks}
          loading={tasksQuery.isLoading}
          error={tasksQuery.error}
          hasReadData={readableQueryData(tasksQuery) !== undefined}
          onRetry={() => void tasksQuery.refetch()}
          tableSize="small"
          pageSize={10}
          columns={[
            { title: '任务名称', dataIndex: 'title' },
            {
              title: '范围',
              dataIndex: 'scopeType',
              render: (_value: string, record: InspectionTask) =>
                [record.scopeType, record.clusterId, record.namespace].filter(Boolean).join(' / '),
            },
            {
              title: '检查项',
              dataIndex: 'checks',
              render: (value: string[], record: InspectionTask) => (
                <Space wrap>
                  {record.capabilityPlan && (
                    <MetadataTag
                      label={`${record.trigger?.kind === 'alert' ? '告警' : '定时'} · r${record.revision}`}
                    />
                  )}
                  {(value ?? []).map((item) => (
                    <MetadataTag key={item} label={item} />
                  ))}
                </Space>
              ),
            },
            {
              title: '间隔',
              dataIndex: 'intervalMinutes',
              render: (value: number) => `${value} min`,
            },
            {
              title: '启用',
              dataIndex: 'enabled',
              render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} />,
            },
            {
              ...tableColumnPresets.action,
              title: '操作',
              dataIndex: 'id',
              render: (_value: string, record: InspectionTask) => (
                <Space className="soha-row-action-icons">
                  <ManagementIconButton
                    aria-label="编辑巡检任务"
                    size="small"
                    tooltip="编辑"
                    icon={<EditOutlined />}
                    onClick={() => openEditTask(record)}
                    disabled={blocked || !canUpdateInspection}
                    title={
                      canUpdateInspection ? undefined : '缺少 observe.ai.inspection.update 权限'
                    }
                  />
                  <ManagementIconButton
                    aria-label="立即执行巡检"
                    size="small"
                    tooltip="立即执行"
                    icon={<PlayCircleOutlined />}
                    loading={executeMutation.isPending}
                    onClick={() => void executeInspection(record)}
                    disabled={blocked || !canRunInspection}
                    title={canRunInspection ? undefined : '缺少 observe.ai.inspection.run 权限'}
                  />
                  <Popconfirm
                    disabled={blocked}
                    title="确认删除巡检任务？"
                    description={
                      record.capabilityPlan
                        ? '有执行历史的能力注册应停用并保留。'
                        : '关联巡检运行记录会一并删除。'
                    }
                    onConfirm={() => deleteTaskMutation.mutate(record.id)}
                    okButtonProps={{ danger: true, loading: deleteTaskMutation.isPending }}
                  >
                    <ManagementIconButton
                      aria-label="删除巡检任务"
                      size="small"
                      tooltip="删除"
                      icon={<DeleteOutlined />}
                      danger
                      disabled={blocked || !canDeleteInspection}
                      title={
                        canDeleteInspection ? undefined : '缺少 observe.ai.inspection.delete 权限'
                      }
                    />
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      ) : null}

      <Modal
        title={editingTask ? '编辑巡检任务' : '新建巡检任务'}
        open={taskModalOpen}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: !blocked }}
        cancelButtonProps={{ disabled: blocked }}
        onCancel={closeTaskModal}
        onOk={submitTaskForm}
        okText={editingTask ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={taskSaving}
        okButtonProps={{
          disabled:
            blocked || tasksDenied || (editingTask ? !canUpdateInspection : !canCreateInspection),
        }}
        width={640}
      >
        {createTaskMutation.feedback}
        {updateTaskMutation.feedback}
        {planValidation.feedback}
        <Form disabled={blocked || tasksDenied} form={taskForm} layout="vertical" preserve={false}>
          <Form.Item name="id" hidden>
            <Input />
          </Form.Item>
          <Form.Item name="mode" label="巡检方式">
            <Select
              options={[
                { value: 'legacy', label: '内置检查项' },
                { value: 'capability', label: '注册能力计划' },
              ]}
              onChange={(mode) => {
                planValidation.reset()
                if (mode === 'capability')
                  taskForm.setFieldsValue({
                    enabled: false,
                    triggerKind: taskForm.getFieldValue('triggerKind') ?? 'schedule',
                    planJSON:
                      taskForm.getFieldValue('planJSON') ||
                      JSON.stringify({ goal: '', steps: [], verificationSteps: [] }, null, 2),
                  })
              }}
            />
          </Form.Item>
          <Form.Item
            name="title"
            label="任务名称"
            rules={[{ required: true, message: '请输入任务名称' }]}
          >
            <Input placeholder="例如：支付命名空间巡检" />
          </Form.Item>
          <Form.Item
            name="scopeType"
            label="巡检范围"
            rules={[{ required: true, message: '请选择巡检范围' }]}
          >
            <Select
              options={[
                { value: 'platform', label: '平台级' },
                { value: 'cluster', label: '集群级' },
                { value: 'namespace', label: '命名空间级' },
              ]}
            />
          </Form.Item>
          {watchedScopeType === 'cluster' || watchedScopeType === 'namespace' ? (
            <Form.Item
              name="clusterId"
              label="集群 ID"
              rules={[{ required: true, message: '请输入集群 ID' }]}
            >
              <Input placeholder="local-k3s" />
            </Form.Item>
          ) : null}
          {watchedScopeType === 'namespace' ? (
            <Form.Item
              name="namespace"
              label="命名空间"
              rules={[{ required: true, message: '请输入命名空间' }]}
            >
              <Input placeholder="default" />
            </Form.Item>
          ) : null}
          {watchedInspectionMode === 'capability' ? (
            <>
              <Alert
                type="info"
                title="启用后按注册触发；每轮使用独立幂等键，写入仍遵守当前权限和审批。停用只停止后续触发。"
              />
              <Form.Item name="triggerKind" label="触发方式" rules={[{ required: true }]}>
                <Select
                  options={[
                    { value: 'schedule', label: '定时' },
                    { value: 'alert', label: '内部告警规则的新告警' },
                  ]}
                />
              </Form.Item>
              {watchedTriggerKind === 'alert' && (
                <>
                  <Form.Item
                    name="alertRuleId"
                    label="告警规则 ID"
                    rules={[{ required: true, whitespace: true }]}
                  >
                    <Input />
                  </Form.Item>
                  <Form.Item name="maxEventAgeSeconds" label="告警有效期（秒）">
                    <InputNumber min={60} max={86400} />
                  </Form.Item>
                </>
              )}
              <Form.Item name="aiClientId" label="AI 客户端 ID（可选）">
                <Input />
              </Form.Item>
              <Form.Item name="skillId" label="Skill ID（可选）">
                <Input />
              </Form.Item>
              <Form.Item
                name="planJSON"
                label="版本固定的能力计划 JSON"
                rules={[{ required: true }]}
              >
                <Input.TextArea
                  rows={12}
                  spellCheck={false}
                  onChange={() => planValidation.reset()}
                />
              </Form.Item>
              <Button
                disabled={blocked}
                loading={planValidation.isPending}
                onClick={() =>
                  void validateInspectionPlan(taskForm.getFieldsValue()).catch((error: unknown) =>
                    message.error(error instanceof Error ? error.message : '计划无效'),
                  )
                }
              >
                校验计划
              </Button>
              {planValidation.data && (
                <Alert
                  type={planValidation.data.data.valid ? 'success' : 'error'}
                  title={
                    planValidation.data.data.valid
                      ? '计划校验通过；提交时会再次校验'
                      : '计划需要调整'
                  }
                  description={planValidation.data.data.issues
                    .map((issue) => issue.message)
                    .join('；')}
                />
              )}
            </>
          ) : (
            <>
              <Form.Item
                name="checks"
                label="检查项"
                rules={[{ required: true, message: '请选择检查项' }]}
              >
                <Select mode="multiple" options={INSPECTION_CHECK_OPTIONS} />
              </Form.Item>
              <Form.Item name="analysisProfileId" label="巡检模板">
                <Select
                  showSearch={{ optionFilterProp: 'label' }}
                  allowClear
                  loading={catalogQuery.isLoading}
                  placeholder="可选：按分析模板覆盖巡检 playbooks"
                  options={profiles
                    .filter((item) => item.mode === 'inspection' && item.enabled)
                    .map((item) => ({ value: item.id, label: `${item.name} (${item.id})` }))}
                />
              </Form.Item>
            </>
          )}
          <Form.Item
            name="intervalMinutes"
            label={
              watchedTriggerKind === 'alert' && watchedInspectionMode === 'capability'
                ? '冷却时间（分钟）'
                : '执行间隔(分钟)'
            }
            rules={[{ required: true, message: '请输入执行间隔' }]}
          >
            <InputNumber
              min={watchedInspectionMode === 'capability' ? 1 : 5}
              max={525600}
              style={{ width: '100%' }}
            />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      {activeView === 'runs' ? (
        <>
          {requestedInspectionRunId ? (
            <Alert
              type={runs.some((item) => item.id === requestedInspectionRunId) ? 'info' : 'warning'}
              showIcon
              title={
                runs.some((item) => item.id === requestedInspectionRunId)
                  ? `已定位巡检运行 ${requestedInspectionRunId}`
                  : '未找到关联巡检运行'
              }
              description={
                runs.some((item) => item.id === requestedInspectionRunId)
                  ? '该运行来自分析工件关联入口。'
                  : `inspectionRunId=${requestedInspectionRunId}`
              }
              style={{ marginBottom: 12 }}
            />
          ) : null}
          <AdminTable
            title="巡检运行记录"
            headerExtra={
              <Button
                size="small"
                onClick={() => navigate(getAIWorkbenchPathForMode('inspection_review'))}
                disabled={blocked || !canUseChat}
              >
                进入巡检复盘工作台
              </Button>
            }
            shellClassName="soha-management-table-shell"
            columnSettingIconOnly
            columnSettingPlacement="header"
            rowKey="id"
            dataSource={runs}
            loading={runsQuery.isLoading}
            error={runsQuery.error}
            hasReadData={readableQueryData(runsQuery) !== undefined}
            onRetry={() => void runsQuery.refetch()}
            tableSize="small"
            pageSize={10}
            columns={[
              {
                title: '运行 ID',
                dataIndex: 'id',
                render: (value: string) => (
                  <Space size={6} wrap>
                    <Text>{value}</Text>
                    {value === requestedInspectionRunId ? (
                      <MetadataTag label="已定位" tone="blue" />
                    ) : null}
                  </Space>
                ),
              },
              { title: '任务', dataIndex: 'taskId' },
              {
                title: '状态',
                dataIndex: 'status',
                render: (value: string) => <StatusTag value={value} />,
              },
              {
                title: '严重度',
                dataIndex: 'severity',
                render: (value: string) => <StatusTag value={value} />,
              },
              {
                title: '发现项',
                dataIndex: 'findings',
                render: (value: Array<{ id: string }>) => value?.length ?? 0,
              },
              { title: '摘要', dataIndex: 'summary' },
              {
                title: '能力任务',
                key: 'capabilityTask',
                render: (_: unknown, record: InspectionRun) =>
                  typeof record.report?.capabilityTaskId === 'string' ? (
                    <Button
                      disabled={blocked}
                      type="link"
                      onClick={() =>
                        navigate(
                          `/ai-workbench/tasks?taskId=${encodeURIComponent(String(record.report?.capabilityTaskId))}`,
                        )
                      }
                    >
                      查看目标与证据
                    </Button>
                  ) : (
                    '—'
                  ),
              },
              {
                ...tableColumnPresets.action,
                title: '联动',
                dataIndex: 'id',
                render: (value: string) => (
                  <ManagementIconButton
                    aria-label="创建 AI 会话"
                    size="small"
                    tooltip="创建 AI 会话"
                    icon={<RobotOutlined />}
                    onClick={() => createSessionMutation.mutate(value)}
                    disabled={blocked || !canCreateSessionFromRun}
                    title={
                      canCreateSessionFromRun
                        ? undefined
                        : !canUseChat
                          ? '缺少 observe.ai.chat 权限'
                          : '缺少 observe.ai.view 权限'
                    }
                  />
                ),
              },
            ]}
          />
        </>
      ) : null}

      {activeView === 'policies' ? (
        <>
          {!canUpdateAISettings ? (
            <Alert
              type="warning"
              showIcon
              title="缺少 settings.ai.update 权限"
              description="自动化策略包含全局 AI 执行配置，当前账号不能查看或编辑。巡检任务和运行记录仍可继续使用。"
              style={{ marginBottom: 16 }}
            />
          ) : null}
          <AdminTable
            title="自动化策略"
            headerExtra={
              <Button
                size="small"
                type="primary"
                icon={<PlusOutlined />}
                onClick={openCreatePolicy}
                disabled={blocked || !canUpdateAISettings}
                title={canUpdateAISettings ? undefined : '缺少 settings.ai.update 权限'}
              >
                新建策略
              </Button>
            }
            shellClassName="soha-management-table-shell"
            columnSettingIconOnly
            columnSettingPlacement="header"
            rowKey="id"
            dataSource={policies}
            loading={policiesQuery.isLoading}
            error={policiesQuery.error}
            hasReadData={readableQueryData(policiesQuery) !== undefined}
            onRetry={() => void policiesQuery.refetch()}
            tableSize="small"
            pageSize={10}
            columns={[
              { title: '名称', dataIndex: 'name' },
              { title: '触发类型', dataIndex: 'triggerType' },
              {
                title: '分析类型',
                dataIndex: 'analysisKinds',
                render: (value: string[]) => (
                  <Space wrap>
                    {(value ?? []).map((item) => (
                      <MetadataTag key={item} label={item} />
                    ))}
                  </Space>
                ),
              },
              {
                title: 'Agent',
                dataIndex: 'agentProviderId',
                render: (value: string) =>
                  agentProviders.find((item) => item.id === (value || 'internal'))?.name ||
                  value ||
                  'internal',
              },
              {
                title: '分析模板',
                dataIndex: 'analysisProfileId',
                render: (value: string) =>
                  profiles.find((item) => item.id === value)?.name || value,
              },
              { title: '修复策略', dataIndex: 'remediationPolicy' },
              {
                title: '启用',
                dataIndex: 'enabled',
                render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} />,
              },
              {
                ...tableColumnPresets.action,
                title: '操作',
                dataIndex: 'id',
                render: (_value: string, record: AutomationPolicy) => (
                  <Space className="soha-row-action-icons">
                    <ManagementIconButton
                      aria-label="编辑自动化策略"
                      size="small"
                      tooltip="编辑"
                      icon={<EditOutlined />}
                      onClick={() => openEditPolicy(record)}
                      disabled={blocked || !canUpdateAISettings}
                      title={canUpdateAISettings ? undefined : '缺少 settings.ai.update 权限'}
                    />
                    <Popconfirm
                      disabled={blocked}
                      title="确认删除自动化策略？"
                      description="删除后不会再由该策略触发新的 AI 分析。"
                      onConfirm={() => deletePolicyMutation.mutate(record.id)}
                      okButtonProps={{ danger: true, loading: deletePolicyMutation.isPending }}
                    >
                      <ManagementIconButton
                        aria-label="删除自动化策略"
                        size="small"
                        tooltip="删除"
                        icon={<DeleteOutlined />}
                        danger
                        disabled={blocked || !canUpdateAISettings}
                        title={canUpdateAISettings ? undefined : '缺少 settings.ai.update 权限'}
                      />
                    </Popconfirm>
                  </Space>
                ),
              },
            ]}
          />
        </>
      ) : null}

      {activeView === 'profiles' ? <AISettingsPage embedded section="profiles" /> : null}

      <Modal
        title={editingPolicy ? '编辑自动化策略' : '新建自动化策略'}
        open={policyModalOpen}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: !blocked }}
        cancelButtonProps={{ disabled: blocked }}
        onCancel={closePolicyModal}
        onOk={() => void submitPolicyForm().catch(() => undefined)}
        okText={editingPolicy ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={policySaving}
        okButtonProps={{ disabled: blocked || policiesDenied || !canUpdateAISettings }}
        width={680}
      >
        {createPolicyMutation.feedback}
        {updatePolicyMutation.feedback}
        <Form
          disabled={blocked || policiesDenied}
          form={policyForm}
          layout="vertical"
          preserve={false}
        >
          <Form.Item
            name="name"
            label="策略名称"
            rules={[{ required: true, message: '请输入策略名称' }]}
          >
            <Input placeholder="例如：P1 告警根因分析" />
          </Form.Item>
          <Form.Item
            name="triggerType"
            label="触发类型"
            rules={[{ required: true, message: '请选择触发类型' }]}
          >
            <Select options={AUTOMATION_TRIGGER_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="analysisKinds"
            label="分析类型"
            rules={[{ required: true, message: '请选择分析类型' }]}
          >
            <Select mode="multiple" options={AUTOMATION_ANALYSIS_KIND_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="agentProviderId"
            label="Agent Provider"
            rules={[{ required: true, message: '请选择 Agent Provider' }]}
          >
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              loading={catalogQuery.isLoading}
              options={
                agentProviderOptions.length > 0
                  ? agentProviderOptions
                  : [{ value: 'internal', label: 'soha 内置分析 / inline' }]
              }
            />
          </Form.Item>
          <Form.Item
            name="analysisProfileId"
            label="分析模板"
            rules={[{ required: true, message: '请选择分析模板' }]}
          >
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              allowClear
              loading={catalogQuery.isLoading}
              placeholder="选择后端分析模板"
              options={profileOptions}
            />
          </Form.Item>
          <Form.Item name="remediationPolicy" label="修复策略">
            <Select options={AUTOMATION_REMEDIATION_POLICY_OPTIONS} />
          </Form.Item>
          <Flex gap={12}>
            <Form.Item name="dedupWindowSeconds" label="去重窗口(秒)" style={{ flex: 1 }}>
              <InputNumber min={60} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="cooldownSeconds" label="冷却时间(秒)" style={{ flex: 1 }}>
              <InputNumber min={60} style={{ width: '100%' }} />
            </Form.Item>
          </Flex>
          <Form.Item name="triggerSeverity" label="告警级别">
            <Select mode="multiple" allowClear options={AUTOMATION_SEVERITY_OPTIONS} />
          </Form.Item>
          <Form.Item name="triggerStatus" label="告警状态">
            <Select mode="multiple" allowClear options={AUTOMATION_STATUS_OPTIONS} />
          </Form.Item>
          <Flex gap={12}>
            <Form.Item name="triggerMinDurationSeconds" label="最小持续(秒)" style={{ flex: 1 }}>
              <InputNumber min={0} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item
              name="triggerTimeRangeMinutes"
              label="分析时间范围(分钟)"
              style={{ flex: 1 }}
            >
              <InputNumber min={5} style={{ width: '100%' }} />
            </Form.Item>
          </Flex>
          <Flex gap={12}>
            <Form.Item name="triggerLabelKey" label="标签 Key" style={{ flex: 1 }}>
              <Input placeholder="service" />
            </Form.Item>
            <Form.Item name="triggerLabelValue" label="标签 Value" style={{ flex: 1 }}>
              <Input placeholder="payment-api" />
            </Form.Item>
          </Flex>
          <Form.Item name="approvalRequired" label="需要审批" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="approvalRoles" label="审批角色">
            <Select mode="tags" tokenSeparators={[',']} placeholder="ops / sre / owner" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
