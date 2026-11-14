import { AdminTable } from '@/components/admin-table'
import { ManagementIconButton } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { StepForm } from '@/components/step-form'
import { StepFormModal } from '@/components/step-form-modal'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableProps } from 'antd'
import {
  App,
  Button,
  Card,
  Col,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Modal,
  Radio,
  Row,
  Select,
  Space,
  Switch,
  Tabs,
  Tag,
} from 'antd'
import { useEffect, useMemo, useState } from 'react'
import '../observability-pages.css'
import {
  buildOnCallAssignmentPayload,
  buildOnCallEscalationPolicyPayload,
  buildOnCallRotationPayload,
  buildOnCallSchedulePayload,
  defaultEscalationStep,
  defaultOnCallRotationFormValues,
  onCallUserOptions,
  toOnCallEscalationStepFormValues,
  toOnCallRotationFormValues,
} from './model'
import { observabilityOncallMutations } from './mutations'
import { observabilityOncallQueries } from './queries'
import type {
  OnCallAssignmentFormValues,
  OnCallAssignmentMatchers,
  OnCallAssignmentRule,
  OnCallEscalationPolicy,
  OnCallEscalationPolicyFormValues,
  OnCallEscalationStepPayload,
  OnCallRotation,
  OnCallRotationFormValues,
  OnCallSchedule,
  OnCallScheduleFormValues,
} from './types'

const integrationTypeOptions = [
  { value: 'prometheus', label: 'Prometheus' },
  { value: 'grafana_alerting', label: 'Grafana Alerting' },
  { value: 'alertmanager', label: 'Alertmanager' },
  { value: 'webhook', label: 'Webhook' },
  { value: 'logs', label: 'Logs' },
  { value: 'traces', label: 'Traces' },
]
const groupByOptions = [
  'alertName',
  'clusterId',
  'namespace',
  'service',
  'severity',
  'businessLineId',
  'integrationId',
].map((value) => ({ value, label: value }))
const roleOptions = [
  { value: 'dev', label: '开发 Dev' },
  { value: 'qa', label: '测试 QA' },
  { value: 'ops', label: '运维 Ops' },
  { value: 'sre', label: 'SRE' },
  { value: 'security', label: '安全 Security' },
  { value: 'owner', label: '业务负责人 Owner' },
]
const severityOptions = ['critical', 'warning', 'info'].map((value) => ({
  value,
  label: value[0].toUpperCase() + value.slice(1),
}))

export function OnCallSettingsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCreateOnCall = hasPermission(permissionSnapshot, 'observe.oncall.create')
  const canUpdateOnCall = hasPermission(permissionSnapshot, 'observe.oncall.update')
  const [scheduleForm] = Form.useForm<OnCallScheduleFormValues>()
  const [rotationForm] = Form.useForm<OnCallRotationFormValues>()
  const [policyForm] = Form.useForm<OnCallEscalationPolicyFormValues>()
  const [assignmentForm] = Form.useForm<OnCallAssignmentFormValues>()
  const [scheduleOpen, setScheduleOpen] = useState(false)
  const [rotationOpen, setRotationOpen] = useState(false)
  const [policyOpen, setPolicyOpen] = useState(false)
  const [assignmentStep, setAssignmentStep] = useState(0)
  const [assignmentOpen, setAssignmentOpen] = useState(false)
  const [editingSchedule, setEditingSchedule] = useState<OnCallSchedule | null>(null)
  const [editingRotation, setEditingRotation] = useState<OnCallRotation | null>(null)
  const [editingPolicy, setEditingPolicy] = useState<OnCallEscalationPolicy | null>(null)
  const [editingAssignment, setEditingAssignment] = useState<OnCallAssignmentRule | null>(null)

  const usersQuery = useQuery(observabilityOncallQueries.users())
  const schedulesQuery = useQuery(observabilityOncallQueries.schedules())
  const rotationsQuery = useQuery(observabilityOncallQueries.rotations())
  const policiesQuery = useQuery(observabilityOncallQueries.escalationPolicies())
  const assignmentsQuery = useQuery(observabilityOncallQueries.routes())
  const createSchedule = useOperationMutation(
    observabilityOncallMutations.createSchedule(queryClient),
    {
      label: ['创建排班', 'Create schedule'],
      target: (input) => input.name,
      receipt: () => ({ message: '排班记录已保存' }),
    },
  )
  const updateSchedule = useOperationMutation(
    observabilityOncallMutations.updateSchedule(queryClient),
    {
      label: ['更新排班', 'Update schedule'],
      target: ({ id }) => id,
      receipt: () => ({ message: '排班记录已保存' }),
    },
  )
  const scheduleDenied = queryAccessDenied(schedulesQuery)
  const scheduleWriteBlocked = createSchedule.blocked || updateSchedule.blocked
  const scheduleBlocked = scheduleWriteBlocked || scheduleDenied
  function closeScheduleEditor() {
    if (scheduleWriteBlocked) return
    setScheduleOpen(false)
    setEditingSchedule(null)
    scheduleForm.resetFields()
  }
  useEffect(() => {
    if (scheduleDenied || queryAccessDenied(createSchedule) || queryAccessDenied(updateSchedule)) {
      setScheduleOpen(false)
      setEditingSchedule(null)
      scheduleForm.resetFields()
    }
  }, [scheduleDenied, createSchedule.error, updateSchedule.error, scheduleForm])
  const createRotation = useOperationMutation(
    observabilityOncallMutations.createRotation(queryClient),
    {
      label: ['创建轮值', 'Create rotation'],
      target: (input) => input.name,
      receipt: () => ({ message: '轮值记录已保存' }),
    },
  )
  const updateRotation = useOperationMutation(
    observabilityOncallMutations.updateRotation(queryClient),
    {
      label: ['更新轮值', 'Update rotation'],
      target: ({ id }) => id,
      receipt: () => ({ message: '轮值记录已保存' }),
    },
  )
  const rotationDenied = queryAccessDenied(rotationsQuery)
  const rotationWriteBlocked = createRotation.blocked || updateRotation.blocked
  const rotationBlocked =
    rotationWriteBlocked ||
    rotationDenied ||
    !readableQueryData(usersQuery) ||
    !readableQueryData(schedulesQuery)
  function closeRotationEditor() {
    if (rotationWriteBlocked) return
    setRotationOpen(false)
    setEditingRotation(null)
    rotationForm.resetFields()
  }
  useEffect(() => {
    if (rotationDenied || queryAccessDenied(createRotation) || queryAccessDenied(updateRotation)) {
      setRotationOpen(false)
      setEditingRotation(null)
      rotationForm.resetFields()
    }
  }, [rotationDenied, createRotation.error, updateRotation.error, rotationForm])
  const createPolicy = useOperationMutation(
    observabilityOncallMutations.createEscalationPolicy(queryClient),
    {
      label: ['创建升级链', 'Create policy'],
      target: (input) => input.name,
      receipt: () => ({ message: '升级链记录已保存' }),
    },
  )
  const updatePolicy = useOperationMutation(
    observabilityOncallMutations.updateEscalationPolicy(queryClient),
    {
      label: ['更新升级链', 'Update policy'],
      target: ({ id }) => id,
      receipt: () => ({ message: '升级链记录已保存' }),
    },
  )
  const policyDenied = queryAccessDenied(policiesQuery)
  const policyWriteBlocked = createPolicy.blocked || updatePolicy.blocked
  const policyBlocked = policyWriteBlocked || policyDenied || !readableQueryData(schedulesQuery)
  function closePolicyEditor() {
    if (policyWriteBlocked) return
    setPolicyOpen(false)
    setEditingPolicy(null)
    policyForm.resetFields()
  }
  useEffect(() => {
    if (policyDenied || queryAccessDenied(createPolicy) || queryAccessDenied(updatePolicy)) {
      setPolicyOpen(false)
      setEditingPolicy(null)
      policyForm.resetFields()
    }
  }, [policyDenied, createPolicy.error, updatePolicy.error, policyForm])
  const createAssignment = useOperationMutation(
    observabilityOncallMutations.createRoute(queryClient),
    {
      label: ['创建分派规则', 'Create assignment'],
      target: (input) => input.name,
      receipt: () => ({ message: '分派规则记录已保存' }),
    },
  )
  const updateAssignment = useOperationMutation(
    observabilityOncallMutations.updateRoute(queryClient),
    {
      label: ['更新分派规则', 'Update assignment'],
      target: ({ id }) => id,
      receipt: () => ({ message: '分派规则记录已保存' }),
    },
  )
  const assignmentDenied = queryAccessDenied(assignmentsQuery)
  const assignmentWriteBlocked = createAssignment.blocked || updateAssignment.blocked
  const assignmentBlocked =
    assignmentWriteBlocked ||
    assignmentDenied ||
    !readableQueryData(policiesQuery) ||
    !readableQueryData(schedulesQuery)
  function closeAssignmentEditor() {
    if (assignmentWriteBlocked) return
    setAssignmentOpen(false)
    setEditingAssignment(null)
    assignmentForm.resetFields()
  }
  useEffect(() => {
    if (
      assignmentDenied ||
      queryAccessDenied(createAssignment) ||
      queryAccessDenied(updateAssignment)
    ) {
      setAssignmentOpen(false)
      setEditingAssignment(null)
      assignmentForm.resetFields()
    }
  }, [assignmentDenied, createAssignment.error, updateAssignment.error, assignmentForm])
  const schedules = readableQueryData(schedulesQuery) ?? []
  const scheduleMap = useMemo(
    () => Object.fromEntries(schedules.map((item) => [item.id, item.name])),
    [schedules],
  )
  const escalationMap = useMemo(
    () =>
      Object.fromEntries(
        (readableQueryData(policiesQuery) ?? []).map((item) => [item.id, item.name]),
      ),
    [policiesQuery.data, policiesQuery.error],
  )
  const targetOptions = useMemo(
    () => [
      ...(readableQueryData(policiesQuery) ?? []).map((item) => ({
        value: item.id,
        label: `升级链 · ${item.name}`,
      })),
      ...schedules.map((item) => ({ value: item.id, label: `排班 · ${item.name}` })),
    ],
    [policiesQuery.data, policiesQuery.error, schedules],
  )
  const userOptions = useMemo(
    () => onCallUserOptions(readableQueryData(usersQuery) ?? []),
    [usersQuery.data, usersQuery.error],
  )

  function targetLabel(type?: string, ref?: string) {
    if (!ref) return '-'
    if (type === 'escalation') {
      return escalationMap[ref] ? `升级链 · ${escalationMap[ref]}` : ref
    }
    return scheduleMap[ref] ? `排班 · ${scheduleMap[ref]}` : ref
  }

  function success(close: () => void) {
    close()
  }

  function submitSchedule(values: OnCallScheduleFormValues) {
    if (scheduleBlocked || !(editingSchedule ? canUpdateOnCall : canCreateOnCall)) return
    const payload = buildOnCallSchedulePayload(values)
    const onSuccess = () =>
      success(() => {
        setScheduleOpen(false)
        setEditingSchedule(null)
        scheduleForm.resetFields()
      })
    if (editingSchedule) {
      updateSchedule.mutate({ id: editingSchedule.id, payload }, { onSuccess })
    } else createSchedule.mutate(payload, { onSuccess })
  }

  function submitRotation(values: OnCallRotationFormValues) {
    if (rotationBlocked || !(editingRotation ? canUpdateOnCall : canCreateOnCall)) return
    const payload = buildOnCallRotationPayload(values, editingRotation?.rotationConfig)
    const onSuccess = () =>
      success(() => {
        setRotationOpen(false)
        setEditingRotation(null)
        rotationForm.resetFields()
      })
    if (editingRotation) {
      updateRotation.mutate({ id: editingRotation.id, payload }, { onSuccess })
    } else createRotation.mutate(payload, { onSuccess })
  }

  function submitPolicy(values: OnCallEscalationPolicyFormValues) {
    if (policyBlocked || !(editingPolicy ? canUpdateOnCall : canCreateOnCall)) return
    const payload = buildOnCallEscalationPolicyPayload(values, editingPolicy?.steps)
    const onSuccess = () =>
      success(() => {
        setPolicyOpen(false)
        setEditingPolicy(null)
        policyForm.resetFields()
      })
    if (editingPolicy) {
      updatePolicy.mutate({ id: editingPolicy.id, payload }, { onSuccess })
    } else createPolicy.mutate(payload, { onSuccess })
  }

  function openAssignmentEditor(record: OnCallAssignmentRule | null) {
    if (assignmentWriteBlocked || assignmentDenied) return
    createAssignment.reset()
    updateAssignment.reset()
    assignmentForm.resetFields()
    setEditingAssignment(record)
    setAssignmentStep(0)
    setAssignmentOpen(true)
    assignmentForm.setFieldsValue(
      record
        ? {
            ...record,
            matchers: JSON.stringify(record.matchers ?? {}, null, 2),
            groupBy: record.groupBy ?? [],
          }
        : {
            name: '',
            integrationId: '',
            integrationType: 'prometheus',
            businessLineId: '',
            alertCategory: '',
            alertName: '',
            severity: '',
            service: '',
            role: '',
            matchers: '{}',
            targetType: 'escalation',
            targetRef: '',
            routeOrder: 100,
            groupBy: ['alertName', 'clusterId', 'namespace', 'service'],
            priority: 100,
            enabled: true,
          },
    )
  }

  function submitAssignment(values: OnCallAssignmentFormValues) {
    if (assignmentBlocked || !(editingAssignment ? canUpdateOnCall : canCreateOnCall)) return
    try {
      const payload = buildOnCallAssignmentPayload(values)
      const onSuccess = () =>
        success(() => {
          setAssignmentOpen(false)
          setEditingAssignment(null)
        })
      if (editingAssignment) {
        updateAssignment.mutate({ id: editingAssignment.id, payload }, { onSuccess })
      } else createAssignment.mutate(payload, { onSuccess })
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  const scheduleColumns: TableProps<OnCallSchedule>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    { title: '时区', dataIndex: 'timeZone', render: (value: string) => value || '-' },
    { title: '描述', dataIndex: 'description', render: (value: string) => value || '-' },
    {
      title: '启用',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    { title: '更新时间', dataIndex: 'updatedAt', render: formatDateTime },
    {
      title: '操作',
      key: 'actions',
      dataIndex: 'id',
      render: (_: string, record) =>
        canUpdateOnCall ? (
          <ManagementIconButton
            aria-label="编辑排班"
            disabled={scheduleWriteBlocked || scheduleDenied}
            size="small"
            tooltip="编辑"
            icon={<EditOutlined />}
            onClick={() => {
              if (scheduleWriteBlocked || scheduleDenied) return
              createSchedule.reset()
              updateSchedule.reset()
              scheduleForm.resetFields()
              setEditingSchedule(record)
              scheduleForm.setFieldsValue({
                name: record.name,
                timeZone: record.timeZone || '',
                description: record.description || '',
                enabled: record.enabled,
              })
              setScheduleOpen(true)
            }}
          />
        ) : null,
    },
  ]

  const rotationColumns: TableProps<OnCallRotation>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '排班',
      dataIndex: 'scheduleId',
      render: (value: string) => scheduleMap[value] || value,
    },
    {
      title: '参与人',
      dataIndex: 'participants',
      render: (value: string[]) => (
        <Space wrap>
          {(value ?? []).map((item) => (
            <Tag key={item}>{item}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    { title: '更新时间', dataIndex: 'updatedAt', render: formatDateTime },
    {
      title: '操作',
      key: 'actions',
      dataIndex: 'id',
      render: (_: string, record) =>
        canUpdateOnCall ? (
          <ManagementIconButton
            aria-label="编辑轮值"
            disabled={rotationWriteBlocked || rotationDenied}
            size="small"
            tooltip="编辑"
            icon={<EditOutlined />}
            onClick={() => {
              if (rotationWriteBlocked || rotationDenied) return
              createRotation.reset()
              updateRotation.reset()
              rotationForm.resetFields()
              setEditingRotation(record)
              rotationForm.setFieldsValue(toOnCallRotationFormValues(record))
              setRotationOpen(true)
            }}
          />
        ) : null,
    },
  ]

  const escalationColumns: TableProps<OnCallEscalationPolicy>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '步骤数',
      dataIndex: 'steps',
      render: (value: OnCallEscalationStepPayload[]) => value?.length ?? 0,
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    { title: '更新时间', dataIndex: 'updatedAt', render: formatDateTime },
    {
      title: '操作',
      key: 'actions',
      dataIndex: 'id',
      render: (_: string, record) =>
        canUpdateOnCall ? (
          <ManagementIconButton
            aria-label="编辑升级策略"
            disabled={policyWriteBlocked || policyDenied}
            size="small"
            tooltip="编辑"
            icon={<EditOutlined />}
            onClick={() => {
              if (policyWriteBlocked || policyDenied) return
              createPolicy.reset()
              updatePolicy.reset()
              policyForm.resetFields()
              setEditingPolicy(record)
              policyForm.setFieldsValue({
                name: record.name,
                steps: toOnCallEscalationStepFormValues(record.steps),
                enabled: record.enabled,
              })
              setPolicyOpen(true)
            }}
          />
        ) : null,
    },
  ]

  const assignmentColumns: TableProps<OnCallAssignmentRule>['columns'] = [
    {
      title: '顺序',
      dataIndex: 'routeOrder',
      width: 78,
      render: (value: number, record) => value || record.priority || '-',
    },
    { title: '规则名称', dataIndex: 'name', width: 220 },
    {
      title: '集成源',
      dataIndex: 'integrationType',
      render: (value: string, record) => (
        <Space wrap>
          <Tag>
            {value
              ? integrationTypeOptions.find((item) => item.value === value)?.label || value
              : '全部入口'}
          </Tag>
          {record.integrationId ? <Tag>{record.integrationId}</Tag> : null}
        </Space>
      ),
    },
    {
      title: '匹配器',
      dataIndex: 'matchers',
      render: (_: OnCallAssignmentMatchers | undefined, record) => (
        <Space wrap>
          {record.businessLineId ? <Tag>范围:{record.businessLineId}</Tag> : null}
          {record.service ? <Tag>服务:{record.service}</Tag> : null}
          {record.severity ? <StatusTag value={record.severity} /> : null}
          {record.role ? <Tag>角色:{record.role}</Tag> : null}
          {record.alertCategory ? <Tag>类型:{record.alertCategory}</Tag> : null}
          {record.matchers && Object.keys(record.matchers).length ? (
            <Tag>扩展 {Object.keys(record.matchers).length}</Tag>
          ) : null}
        </Space>
      ),
    },
    {
      title: '分组',
      dataIndex: 'groupBy',
      render: (value: string[]) =>
        value?.length ? (
          <Space wrap>
            {value.map((item) => (
              <Tag key={item}>{item}</Tag>
            ))}
          </Space>
        ) : (
          '默认分组'
        ),
    },
    {
      title: '升级目标',
      dataIndex: 'targetRef',
      render: (value: string, record) => targetLabel(record.targetType, value),
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    { title: '更新时间', dataIndex: 'updatedAt', render: formatDateTime },
    {
      title: '操作',
      key: 'actions',
      dataIndex: 'id',
      render: (_: string, record) =>
        canUpdateOnCall ? (
          <ManagementIconButton
            aria-label="编辑分派规则"
            disabled={assignmentWriteBlocked || assignmentDenied}
            size="small"
            tooltip="编辑"
            icon={<EditOutlined />}
            onClick={() => openAssignmentEditor(record)}
          />
        ) : null,
    },
  ]

  return (
    <div className="soha-page">
      {!scheduleOpen ? (
        <>
          {createSchedule.feedback}
          {updateSchedule.feedback}
        </>
      ) : null}
      {!rotationOpen ? (
        <>
          {createRotation.feedback}
          {updateRotation.feedback}
        </>
      ) : null}
      {!policyOpen ? (
        <>
          {createPolicy.feedback}
          {updatePolicy.feedback}
        </>
      ) : null}
      {!assignmentOpen ? (
        <>
          {createAssignment.feedback}
          {updateAssignment.feedback}
        </>
      ) : null}
      <Tabs
        items={[
          {
            key: 'assignments',
            label: '告警分派',
            children: (
              <AdminTable
                enableDensity
                error={assignmentsQuery.error}
                hasReadData={assignmentsQuery.data !== undefined}
                refreshing={assignmentsQuery.isFetching}
                onRefresh={() => void assignmentsQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                headerExtra={
                  canCreateOnCall ? (
                    <Button
                      icon={<PlusOutlined />}
                      type="primary"
                      onClick={() => {
                        openAssignmentEditor(null)
                      }}
                    >
                      新增分派规则
                    </Button>
                  ) : null
                }
                columns={assignmentColumns}
                dataSource={assignmentsQuery.data ?? []}
                rowKey="id"
                loading={assignmentsQuery.isLoading}
              />
            ),
          },
          {
            key: 'schedules',
            label: '排班',
            children: (
              <AdminTable
                enableDensity
                error={schedulesQuery.error}
                hasReadData={schedulesQuery.data !== undefined}
                refreshing={schedulesQuery.isFetching}
                onRefresh={() => void schedulesQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                headerExtra={
                  canCreateOnCall ? (
                    <Button
                      icon={<PlusOutlined />}
                      type="primary"
                      onClick={() => {
                        if (scheduleWriteBlocked || scheduleDenied) return
                        createSchedule.reset()
                        updateSchedule.reset()
                        scheduleForm.resetFields()
                        setEditingSchedule(null)
                        scheduleForm.resetFields()
                        setScheduleOpen(true)
                      }}
                    >
                      新增排班
                    </Button>
                  ) : null
                }
                columns={scheduleColumns}
                dataSource={schedules}
                rowKey="id"
                loading={schedulesQuery.isLoading}
              />
            ),
          },
          {
            key: 'rotations',
            label: '轮值',
            children: (
              <AdminTable
                enableDensity
                error={rotationsQuery.error}
                hasReadData={rotationsQuery.data !== undefined}
                refreshing={rotationsQuery.isFetching}
                onRefresh={() => void rotationsQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                headerExtra={
                  canCreateOnCall ? (
                    <Button
                      icon={<PlusOutlined />}
                      type="primary"
                      onClick={() => {
                        if (rotationWriteBlocked || rotationDenied) return
                        createRotation.reset()
                        updateRotation.reset()
                        rotationForm.resetFields()
                        setEditingRotation(null)
                        rotationForm.setFieldsValue(defaultOnCallRotationFormValues())
                        setRotationOpen(true)
                      }}
                    >
                      新增轮值
                    </Button>
                  ) : null
                }
                columns={rotationColumns}
                dataSource={rotationsQuery.data ?? []}
                rowKey="id"
                loading={rotationsQuery.isLoading}
              />
            ),
          },
          {
            key: 'policies',
            label: '升级链',
            children: (
              <AdminTable
                enableDensity
                error={policiesQuery.error}
                hasReadData={policiesQuery.data !== undefined}
                refreshing={policiesQuery.isFetching}
                onRefresh={() => void policiesQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                headerExtra={
                  canCreateOnCall ? (
                    <Button
                      icon={<PlusOutlined />}
                      type="primary"
                      onClick={() => {
                        if (policyWriteBlocked || policyDenied) return
                        createPolicy.reset()
                        updatePolicy.reset()
                        policyForm.resetFields()
                        setEditingPolicy(null)
                        policyForm.setFieldsValue({
                          name: '',
                          steps: [defaultEscalationStep()],
                          enabled: true,
                        })
                        setPolicyOpen(true)
                      }}
                    >
                      新增升级链
                    </Button>
                  ) : null
                }
                columns={escalationColumns}
                dataSource={readableQueryData(policiesQuery) ?? []}
                rowKey="id"
                loading={policiesQuery.isLoading}
              />
            ),
          },
        ]}
      />

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title={editingSchedule ? '编辑排班' : '新建排班'}
        open={scheduleOpen && !scheduleDenied}
        closable={!scheduleWriteBlocked}
        keyboard={!scheduleWriteBlocked}
        mask={{ closable: false }}
        onCancel={closeScheduleEditor}
        footer={
          <Space>
            <Button disabled={scheduleWriteBlocked} onClick={closeScheduleEditor}>
              取消
            </Button>
            <Button
              type="primary"
              loading={createSchedule.isPending || updateSchedule.isPending}
              disabled={scheduleBlocked}
              onClick={() => scheduleForm.submit()}
            >
              保存
            </Button>
          </Space>
        }
        destroyOnHidden
      >
        {createSchedule.feedback}
        {updateSchedule.feedback}
        <Form
          layout="vertical"
          disabled={scheduleBlocked}
          form={scheduleForm}
          onFinish={submitSchedule}
          initialValues={{ enabled: true }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="timeZone" label="时区">
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      <StepFormModal
        title={editingAssignment ? '编辑告警分派规则' : '新建告警分派规则'}
        open={assignmentOpen && !assignmentDenied}
        closeDisabled={assignmentWriteBlocked}
        feedback={
          <>
            {createAssignment.feedback}
            {updateAssignment.feedback}
            <ReadFeedback label="排班选项" query={schedulesQuery} />
            <ReadFeedback label="升级链选项" query={policiesQuery} />
          </>
        }
        onClose={closeAssignmentEditor}
        width={860}
      >
        <StepForm
          onCancel={closeAssignmentEditor}
          contentMaxWidth="100%"
          disabled={assignmentBlocked}
          form={assignmentForm}
          current={assignmentStep}
          onCurrentChange={setAssignmentStep}
          onFinish={submitAssignment}
          loading={createAssignment.isPending || updateAssignment.isPending}
          steps={[
            {
              title: '匹配条件',
              fieldNames: ['name'],
              children: (
                <>
                  {' '}
                  <div className="soha-observability-form-grid">
                    <Form.Item name="name" label="规则名称" rules={[{ required: true }]}>
                      <Input />
                    </Form.Item>
                    <Form.Item name="routeOrder" label="匹配顺序">
                      <InputNumber min={1} style={{ width: '100%' }} />
                    </Form.Item>
                  </div>
                  <div className="soha-observability-form-grid">
                    <Form.Item name="integrationType" label="集成类型">
                      <Select allowClear options={integrationTypeOptions} />
                    </Form.Item>
                    <Form.Item name="integrationId" label="集成ID">
                      <Input placeholder="grafana-prod / am-main" />
                    </Form.Item>
                    <Form.Item name="severity" label="严重度">
                      <Select allowClear options={severityOptions} />
                    </Form.Item>
                  </div>
                  <div className="soha-observability-form-grid">
                    <Form.Item name="service" label="服务/应用">
                      <Input />
                    </Form.Item>
                    <Form.Item name="alertName" label="告警名称包含">
                      <Input />
                    </Form.Item>
                    <Form.Item name="alertCategory" label="告警类型标签">
                      <Input />
                    </Form.Item>
                  </div>
                  <div className="soha-observability-form-grid">
                    <Form.Item name="businessLineId" label="范围标签">
                      <Input allowClear />
                    </Form.Item>
                    <Form.Item name="role" label="响应角色标签">
                      <Select allowClear options={roleOptions} />
                    </Form.Item>
                    <Form.Item name="groupBy" label="分组键">
                      <Select mode="tags" options={groupByOptions} />
                    </Form.Item>
                  </div>
                  <Form.Item name="matchers" label="扩展匹配器(JSON)">
                    <Input.TextArea rows={4} />
                  </Form.Item>
                </>
              ),
            },
            {
              title: '分派目标',
              children: (
                <>
                  {' '}
                  <div className="soha-observability-form-grid">
                    <Form.Item name="targetType" label="目标类型" rules={[{ required: true }]}>
                      <Select
                        options={[
                          { value: 'escalation', label: '升级链' },
                          { value: 'schedule', label: '排班' },
                        ]}
                      />
                    </Form.Item>
                    <Form.Item name="targetRef" label="升级目标" rules={[{ required: true }]}>
                      <Select showSearch options={targetOptions} />
                    </Form.Item>
                    <Form.Item name="priority" label="兼容优先级">
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                  </div>
                  <Form.Item name="enabled" label="启用" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                </>
              ),
            },
          ]}
        />
      </StepFormModal>

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title={editingRotation ? '编辑轮值' : '新建轮值'}
        open={rotationOpen && !rotationDenied}
        closable={!rotationWriteBlocked}
        keyboard={!rotationWriteBlocked}
        mask={{ closable: false }}
        onCancel={closeRotationEditor}
        footer={
          <Space>
            <Button disabled={rotationWriteBlocked} onClick={closeRotationEditor}>
              取消
            </Button>
            <Button
              type="primary"
              disabled={rotationBlocked}
              onClick={() => rotationForm.submit()}
              loading={createRotation.isPending || updateRotation.isPending}
            >
              保存
            </Button>
          </Space>
        }
        destroyOnHidden
        width={720}
      >
        {createRotation.feedback}
        {updateRotation.feedback}
        <ReadFeedback label="排班选项" query={schedulesQuery} />
        <ReadFeedback label="值班人员选项" query={usersQuery} />
        <Form
          layout="vertical"
          disabled={rotationBlocked}
          form={rotationForm}
          onFinish={submitRotation}
          initialValues={defaultOnCallRotationFormValues()}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="scheduleId" label="排班" rules={[{ required: true }]}>
            <Select
              showSearch
              options={schedules.map((item) => ({ value: item.id, label: item.name }))}
            />
          </Form.Item>
          <Form.Item
            name="participants"
            label="参与人"
            rules={[{ required: true, message: '至少选择一个参与人' }]}
          >
            <Select
              mode="multiple"
              showSearch={{ optionFilterProp: 'label' }}
              options={userOptions}
            />
          </Form.Item>
          <Form.Item name="rotationMode" label="轮换节奏" rules={[{ required: true }]}>
            <Radio.Group
              options={[
                { value: 'daily', label: '每日轮换' },
                { value: 'weekly', label: '每周轮换' },
                { value: 'custom', label: '自定义' },
              ]}
              optionType="button"
              buttonStyle="solid"
            />
          </Form.Item>
          <Form.Item
            noStyle
            shouldUpdate={(previous, next) => previous.rotationMode !== next.rotationMode}
          >
            {({ getFieldValue }) =>
              getFieldValue('rotationMode') === 'custom' ? (
                <Form.Item name="shiftHours" label="单班时长(小时)" rules={[{ required: true }]}>
                  <InputNumber min={1} max={168 * 4} style={{ width: '100%' }} />
                </Form.Item>
              ) : null
            }
          </Form.Item>
          <Form.Item name="startAt" label="轮值起始时间">
            <DatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title={editingPolicy ? '编辑升级链' : '新建升级链'}
        open={policyOpen && !policyDenied}
        closable={!policyWriteBlocked}
        keyboard={!policyWriteBlocked}
        mask={{ closable: false }}
        onCancel={closePolicyEditor}
        footer={
          <Space>
            <Button disabled={policyWriteBlocked} onClick={closePolicyEditor}>
              取消
            </Button>
            <Button
              type="primary"
              disabled={policyBlocked}
              onClick={() => policyForm.submit()}
              loading={createPolicy.isPending || updatePolicy.isPending}
            >
              保存
            </Button>
          </Space>
        }
        destroyOnHidden
        width={840}
      >
        {createPolicy.feedback}
        {updatePolicy.feedback}
        <ReadFeedback label="排班选项" query={schedulesQuery} />
        <Form
          layout="vertical"
          disabled={policyBlocked}
          form={policyForm}
          onFinish={submitPolicy}
          initialValues={{ enabled: true, steps: [defaultEscalationStep()] }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.List name="steps">
            {(fields, { add, remove }) => (
              <>
                {fields.map(({ key, name, ...rest }, index) => (
                  <Card
                    key={key}
                    size="small"
                    className="soha-oncall-step-card"
                    title={`步骤 ${index + 1}`}
                    extra={
                      fields.length > 1 ? (
                        <Button
                          disabled={policyBlocked}
                          size="small"
                          danger
                          onClick={() => remove(name)}
                        >
                          删除
                        </Button>
                      ) : null
                    }
                  >
                    <Row gutter={12}>
                      <Col xs={24} sm={12}>
                        <Form.Item
                          {...rest}
                          name={[name, 'scheduleId']}
                          label="排班对象"
                          rules={[{ required: true }]}
                        >
                          <Select
                            showSearch
                            options={schedules.map((item) => ({
                              value: item.id,
                              label: item.name,
                            }))}
                          />
                        </Form.Item>
                      </Col>
                      <Col xs={12} sm={6}>
                        <Form.Item {...rest} name={[name, 'delayMinutes']} label="延迟(分钟)">
                          <InputNumber min={0} style={{ width: '100%' }} />
                        </Form.Item>
                      </Col>
                      <Col xs={12} sm={6}>
                        <Form.Item {...rest} name={[name, 'role']} label="响应角色">
                          <Select allowClear options={roleOptions} />
                        </Form.Item>
                      </Col>
                      <Col span={24}>
                        <Form.Item {...rest} name={[name, 'description']} label="说明">
                          <Input />
                        </Form.Item>
                      </Col>
                    </Row>
                  </Card>
                ))}
                <Button
                  type="dashed"
                  disabled={policyBlocked}
                  onClick={() => add(defaultEscalationStep())}
                  icon={<PlusOutlined />}
                  block
                >
                  新增步骤
                </Button>
              </>
            )}
          </Form.List>
          <Form.Item name="enabled" label="启用" valuePropName="checked" style={{ marginTop: 16 }}>
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
