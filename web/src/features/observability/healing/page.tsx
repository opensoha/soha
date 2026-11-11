import { AdminTable } from '@/components/admin-table'
import { ManagementIconButton } from '@/components/management-list'
import type { ReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import { BooleanTag, MetadataTag, StatusTag } from '@/components/status-tag'
import { StepForm } from '@/components/step-form'
import { StepFormModal } from '@/components/step-form-modal'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  CheckOutlined,
  CloseOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableProps } from 'antd'
import { App, Button, Card, Form, Input, InputNumber, Select, Space, Switch, Tabs } from 'antd'
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import '../observability-pages.css'
import { buildHealingPolicyPayload } from './model'
import { observabilityHealingMutations } from './mutations'
import { observabilityHealingQueries } from './queries'
import type { HealingPolicy, HealingPolicyFormValues, HealingRun } from './types'

const HealingDagEditor = lazy(() => import('./editor'))

export function HealingPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCreateHealing = hasPermission(permissionSnapshot, 'observe.healing.create')
  const canUpdateHealing = hasPermission(permissionSnapshot, 'observe.healing.update')
  const canApproveHealing = hasPermission(permissionSnapshot, 'observe.healing.approve')
  const canRejectHealing = hasPermission(permissionSnapshot, 'observe.healing.reject')
  const canRetryHealing = hasPermission(permissionSnapshot, 'observe.healing.retry')
  const [form] = Form.useForm<HealingPolicyFormValues>()
  const [step, setStep] = useState(0)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<HealingPolicy | null>(null)
  const [definition, setDefinition] = useState<ReleaseDagDefinition>()
  const policiesQuery = useQuery(observabilityHealingQueries.policies())
  const runsQuery = useQuery(observabilityHealingQueries.runs())
  const createMutation = useOperationMutation(
    observabilityHealingMutations.createPolicy(queryClient),
    {
      label: ['创建自愈策略', 'Create healing policy'],
      target: (input) => input.name,
      receipt: () => ({ message: '自愈策略记录已创建' }),
    },
  )
  const updateMutation = useOperationMutation(
    observabilityHealingMutations.updatePolicy(queryClient),
    {
      label: ['更新自愈策略', 'Update healing policy'],
      target: ({ id }) => id,
      receipt: () => ({ message: '自愈策略记录已更新' }),
    },
  )
  const approveMutation = useOperationMutation(
    observabilityHealingMutations.approveRun(queryClient),
    {
      label: ['审批自愈运行', 'Approve healing run'],
      target: ({ id }) => id,
      receipt: () => ({ message: '审批请求已返回，请核对运行记录。' }),
    },
  )
  const rejectMutation = useOperationMutation(
    observabilityHealingMutations.rejectRun(queryClient),
    {
      label: ['拒绝自愈运行', 'Reject healing run'],
      target: ({ id }) => id,
      receipt: () => ({ message: '拒绝请求已返回，请核对运行记录。' }),
    },
  )
  const retryMutation = useOperationMutation(observabilityHealingMutations.retryRun(queryClient), {
    label: ['重试自愈运行', 'Retry healing run'],
    target: (id) => id,
    receipt: () => ({ message: '重试请求已返回，请核对运行记录。' }),
  })
  const denied = queryAccessDenied(policiesQuery)
  const writeBlocked = createMutation.blocked || updateMutation.blocked
  const runBlocked =
    approveMutation.blocked ||
    rejectMutation.blocked ||
    retryMutation.blocked ||
    queryAccessDenied(runsQuery)
  function closeEditor() {
    if (writeBlocked) return
    setOpen(false)
    setEditing(null)
    setDefinition(undefined)
    form.resetFields()
  }
  useEffect(() => {
    if (denied || queryAccessDenied(createMutation) || queryAccessDenied(updateMutation)) {
      setOpen(false)
      setEditing(null)
      setDefinition(undefined)
      form.resetFields()
    }
  }, [denied, createMutation.error, updateMutation.error, form])
  function openEditor(record: HealingPolicy | null) {
    if (writeBlocked || denied) return
    createMutation.reset()
    updateMutation.reset()
    form.resetFields()
    setStep(0)
    setEditing(record)
    setOpen(true)
    const defaults = record ?? {
      id: '',
      name: '',
      triggerMode: 'approval_then_auto',
      workflowTemplateId: '',
      approvalPolicyRef: '',
      cooldownSeconds: 300,
      concurrencyKey: '',
      safetyWindowSeconds: 600,
      enabled: true,
      definition: undefined,
    }
    setDefinition(defaults.definition)
    form.setFieldsValue({
      name: defaults.name,
      triggerMode: defaults.triggerMode,
      workflowTemplateId: defaults.workflowTemplateId,
      approvalPolicyRef: defaults.approvalPolicyRef,
      cooldownSeconds: defaults.cooldownSeconds,
      concurrencyKey: defaults.concurrencyKey,
      safetyWindowSeconds: defaults.safetyWindowSeconds,
      enabled: defaults.enabled,
    })
  }

  function submit(values: HealingPolicyFormValues) {
    if (writeBlocked || denied || !(editing ? canUpdateHealing : canCreateHealing)) return
    if (!definition) {
      message.warning('自愈 DAG 尚未加载完成')
      return
    }
    const payload = buildHealingPolicyPayload(values, definition)
    const onSuccess = () => {
      setOpen(false)
      setEditing(null)
      setDefinition(undefined)
      form.resetFields()
    }
    if (editing) updateMutation.mutate({ id: editing.id, payload }, { onSuccess })
    else createMutation.mutate(payload, { onSuccess })
  }

  const updateDefinition = useCallback((next: ReleaseDagDefinition) => {
    setDefinition(next)
  }, [])

  const runColumns: TableProps<HealingRun>['columns'] = useMemo(
    () => [
      { title: '运行ID', dataIndex: 'id' },
      { title: '策略', dataIndex: 'policyId' },
      { title: '事件', dataIndex: 'eventId' },
      {
        title: '状态',
        dataIndex: 'status',
        render: (value: string) => <StatusTag value={value} />,
      },
      {
        title: '审批',
        dataIndex: 'approvalStatus',
        render: (value: string) => (value ? <StatusTag value={value} /> : '-'),
      },
      {
        title: 'Workflow',
        dataIndex: 'workflowStatus',
        render: (value: string, record) =>
          value ? <StatusTag value={value} /> : record.workflowRunId || '-',
      },
      {
        title: '审批人',
        dataIndex: 'approvedBy',
        render: (value: string) => value || '-',
      },
      {
        title: '执行摘要',
        dataIndex: 'workflowSummary',
        render: (value: string) => value || '-',
      },
      { title: '创建时间', dataIndex: 'createdAt', render: formatDateTime },
      {
        title: '操作',
        key: 'actions',
        dataIndex: 'id',
        render: (value: string, record) => (
          <Space>
            {canApproveHealing ? (
              <Button
                size="small"
                icon={<CheckOutlined />}
                disabled={runBlocked || ['completed', 'rejected'].includes(record.status)}
                onClick={() =>
                  approveMutation.mutate({ id: value, comment: 'approved from console' })
                }
              >
                通过
              </Button>
            ) : null}
            {canRejectHealing ? (
              <Button
                size="small"
                icon={<CloseOutlined />}
                disabled={runBlocked || ['completed', 'rejected'].includes(record.status)}
                onClick={() =>
                  rejectMutation.mutate({ id: value, comment: 'rejected from console' })
                }
              >
                拒绝
              </Button>
            ) : null}
            {canRetryHealing ? (
              <Button
                size="small"
                disabled={runBlocked}
                icon={<ReloadOutlined />}
                onClick={() => retryMutation.mutate(value)}
              >
                重试
              </Button>
            ) : null}
          </Space>
        ),
      },
    ],
    [
      approveMutation,
      canApproveHealing,
      canRejectHealing,
      canRetryHealing,
      runBlocked,
      rejectMutation,
      retryMutation,
    ],
  )

  const policyColumns: TableProps<HealingPolicy>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '触发模式',
      dataIndex: 'triggerMode',
      render: (value: string) => <MetadataTag label={value} />,
    },
    { title: '工作流模板', dataIndex: 'workflowTemplateId' },
    {
      title: '审批策略',
      dataIndex: 'approvalPolicyRef',
      render: (value: string) => value || '-',
    },
    { title: '冷却(s)', dataIndex: 'cooldownSeconds' },
    { title: '安全窗(s)', dataIndex: 'safetyWindowSeconds' },
    {
      title: '启用',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    {
      title: '操作',
      key: 'actions',
      dataIndex: 'id',
      render: (_: string, record) =>
        canUpdateHealing ? (
          <ManagementIconButton
            aria-label="编辑自愈策略"
            disabled={writeBlocked || denied}
            size="small"
            tooltip="编辑"
            icon={<EditOutlined />}
            onClick={() => openEditor(record)}
          />
        ) : null,
    },
  ]

  return (
    <div className="soha-page">
      {!open ? (
        <>
          {createMutation.feedback}
          {updateMutation.feedback}
        </>
      ) : null}
      {approveMutation.feedback}
      {rejectMutation.feedback}
      {retryMutation.feedback}
      <h1 className="soha-observability-page-heading">自愈中心</h1>
      <Tabs
        className="soha-resource-tabs"
        defaultActiveKey="policies"
        items={[
          {
            key: 'policies',
            label: '自愈策略',
            children: (
              <AdminTable
                enableDensity
                error={policiesQuery.error}
                hasReadData={policiesQuery.data !== undefined}
                refreshing={policiesQuery.isFetching}
                onRefresh={() => void policiesQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                headerExtra={
                  canCreateHealing ? (
                    <Button
                      disabled={writeBlocked || denied}
                      icon={<PlusOutlined />}
                      type="primary"
                      onClick={() => openEditor(null)}
                    >
                      新建自愈策略
                    </Button>
                  ) : null
                }
                shellClassName="soha-management-table-shell"
                columns={policyColumns}
                dataSource={policiesQuery.data ?? []}
                rowKey="id"
                loading={policiesQuery.isLoading}
              />
            ),
          },
          {
            key: 'runs',
            label: '执行记录',
            children: (
              <AdminTable
                enableDensity
                error={runsQuery.error}
                hasReadData={runsQuery.data !== undefined}
                refreshing={runsQuery.isFetching}
                onRefresh={() => void runsQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                columns={runColumns}
                dataSource={runsQuery.data ?? []}
                rowKey="id"
                loading={runsQuery.isLoading}
                pagination={{ pageSize: 10 }}
              />
            ),
          },
        ]}
      />

      <StepFormModal
        title={editing ? '编辑自愈策略' : '新建自愈策略'}
        open={open && !denied}
        closeDisabled={writeBlocked}
        feedback={
          <>
            {createMutation.feedback}
            {updateMutation.feedback}
          </>
        }
        onClose={closeEditor}
        width={step === 2 ? 1180 : 760}
      >
        <StepForm
          onCancel={closeEditor}
          contentMaxWidth="100%"
          disabled={writeBlocked || denied}
          form={form}
          current={step}
          onCurrentChange={setStep}
          onFinish={submit}
          loading={createMutation.isPending || updateMutation.isPending}
          steps={[
            {
              title: '基础与触发',
              fieldNames: ['name', 'workflowTemplateId', 'triggerMode'],
              children: (
                <div className="soha-observability-form-grid">
                  <Form.Item name="name" label="名称" rules={[{ required: true }]}>
                    <Input />
                  </Form.Item>
                  <Form.Item name="triggerMode" label="触发模式">
                    <Select
                      options={[
                        { value: 'approval_then_auto', label: '审批后自动' },
                        { value: 'manual', label: '仅手动' },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item
                    name="workflowTemplateId"
                    label="工作流模板 ID"
                    rules={[{ required: true }]}
                  >
                    <Input />
                  </Form.Item>
                </div>
              ),
            },
            {
              title: '安全约束',
              fieldNames: ['cooldownSeconds', 'safetyWindowSeconds'],
              children: (
                <>
                  <div className="soha-observability-form-grid">
                    <Form.Item name="approvalPolicyRef" label="审批策略引用">
                      <Input />
                    </Form.Item>
                    <Form.Item name="concurrencyKey" label="并发键">
                      <Input />
                    </Form.Item>
                    <Form.Item name="cooldownSeconds" label="冷却(s)">
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                    <Form.Item name="safetyWindowSeconds" label="安全窗(s)">
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                  </div>
                  <Form.Item name="enabled" label="启用" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                </>
              ),
            },
            {
              title: '自愈 DAG',
              children:
                step === 2 ? (
                  <>
                    <Suspense fallback={<Card loading variant="borderless" />}>
                      <HealingDagEditor
                        disabled={writeBlocked}
                        initialDefinition={definition}
                        onChange={updateDefinition}
                      />
                    </Suspense>
                  </>
                ) : null,
            },
          ]}
        />
      </StepFormModal>
    </div>
  )
}
