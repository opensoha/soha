import { AdminTable } from '@/components/admin-table'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementIconButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Drawer, Input, Select, Space } from 'antd'
import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import type { CapabilityTask, CapabilityTaskInput, CapabilityTaskRevisionInput } from './api'
import { capabilityTaskMutations } from './mutations'
import { capabilityTaskQueries, taskCanResume } from './queries'

export function CapabilityTasksPage() {
  const permissions = usePermissionSnapshot()
  const canInvoke = hasPermission(readableQueryData(permissions)?.data, 'ai.gateway.invoke')
  const { modal } = App.useApp()
  const [params, setParams] = useSearchParams()
  const id = params.get('taskId') ?? ''
  const version = Number(params.get('planVersion') ?? 0)
  const [editor, setEditor] = useState<{ value: string; task?: CapabilityTask }>()
  const list = useQuery(capabilityTaskQueries.list(canInvoke))
  const detail = useQuery(
    capabilityTaskQueries.detail(
      id,
      Number.isSafeInteger(version) && version > 0 ? version : 0,
      canInvoke,
    ),
  )
  const task = readableQueryData(detail)?.data
  const client = useQueryClient()
  const cancel = useOperationMutation(capabilityTaskMutations.cancel(client), {
    label: ['请求取消目标任务', 'Request task cancellation'],
    target: (id) => id,
  })
  const denied = queryAccessDenied(list) || queryAccessDenied(detail) || queryAccessDenied(cancel)
  useEffect(() => {
    if (!canInvoke || denied) setEditor(undefined)
  }, [canInvoke, denied])
  const selectTask = (taskId: string, planVersion = 0) => {
    const next = new URLSearchParams(params)
    if (taskId) next.set('taskId', taskId)
    else next.delete('taskId')
    if (planVersion) next.set('planVersion', String(planVersion))
    else next.delete('planVersion')
    setParams(next)
  }
  const openEditor = (current?: CapabilityTask) => {
    if (cancel.blocked || denied) return
    setEditor({
      task: current,
      value: JSON.stringify(
        current
          ? { expectedVersion: current.version, plan: current.plan }
          : {
              idempotencyKey: crypto.randomUUID(),
              plan: { goal: '', steps: [], verificationSteps: [] },
            },
        null,
        2,
      ),
    })
  }
  if (permissions.isLoading) return <ManagementState title="正在读取权限" />
  if (!canInvoke) return <ManagementState kind="error" title="缺少目标任务访问权限" />
  return (
    <>
      {!id && cancel.feedback}
      <ManagementDataPage
        table={{
          title: '目标任务',
          rowKey: 'id',
          dataSource: readableQueryData(list)?.items ?? [],
          loading: list.isLoading,
          error: list.error,
          hasReadData: readableQueryData(list) !== undefined,
          onRetry: () => void list.refetch(),
          headerExtra: (
            <ManagementTableToolbar>
              <ManagementIconButton
                aria-label="刷新目标任务"
                tooltip="刷新"
                icon={<ReloadOutlined />}
                loading={list.isFetching}
                onClick={() => void list.refetch()}
              />
              <Button
                type="primary"
                icon={<PlusOutlined />}
                disabled={cancel.blocked || denied}
                onClick={() => openEditor()}
              >
                提交计划
              </Button>
            </ManagementTableToolbar>
          ),
          columns: [
            {
              title: '目标',
              key: 'goal',
              render: (_: unknown, row: CapabilityTask) => (
                <Link to={`?taskId=${encodeURIComponent(row.id)}`}>{row.plan.goal}</Link>
              ),
            },
            {
              ...tableColumnPresets.status,
              title: '状态',
              dataIndex: 'status',
              render: (value: string) => <StatusTag value={value} />,
            },
            { title: '计划版本', dataIndex: 'planVersion', width: 110 },
            { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt' },
          ],
          empty: list.isError ? (
            <ManagementState kind="error" title="目标任务加载失败" />
          ) : (
            <ManagementState
              title="暂无目标任务"
              description="通过 CLI、MCP 或此处提交计划，使用同一个任务 ID 查看进度与证据。"
            />
          ),
        }}
      />
      <Drawer
        title="目标任务"
        size="min(960px, 100vw)"
        open={Boolean(id) && !editor}
        onClose={() => selectTask('')}
        extra={
          task && !version ? (
            <Space>
              {taskCanResume(task.status) ? (
                <Button disabled={cancel.blocked || denied} onClick={() => openEditor(task)}>
                  修订并续接
                </Button>
              ) : null}
              {!['completed', 'failed', 'canceled', 'inconclusive'].includes(task.status) ? (
                <Button
                  danger
                  disabled={cancel.blocked || denied}
                  loading={cancel.isPending}
                  onClick={() =>
                    modal.confirm({
                      title: '请求取消目标任务？',
                      content: '已完成的操作会保留，正在运行的操作由所属服务处理取消。',
                      okText: '请求取消',
                      onOk: () => cancel.mutateAsync(task.id).catch(() => undefined),
                    })
                  }
                >
                  取消任务
                </Button>
              ) : null}
            </Space>
          ) : undefined
        }
      >
        {cancel.feedback}
        <ReadFeedback query={detail} label="目标任务" />
        {queryAccessDenied(detail) || (!task && detail.isError) ? (
          <ManagementState
            kind="error"
            title="任务读取失败"
            description="请刷新，或检查当前权限与能力版本。"
          />
        ) : detail.isLoading ? (
          <ManagementState title="正在读取任务" />
        ) : task ? (
          <Space orientation="vertical" size={16} style={{ width: '100%' }}>
            <div>
              <strong>{task.plan.goal}</strong> <StatusTag value={task.status} />
            </div>
            <Select
              aria-label="计划版本"
              value={version || 0}
              style={{ minWidth: 180 }}
              onChange={(value) => selectTask(task.id, value)}
              options={[
                { value: 0, label: '当前计划' },
                ...Array.from(
                  { length: Math.max(0, (task.planVersion ?? 1) - (version ? 0 : 1)) },
                  (_, i) => ({ value: i + 1, label: `历史计划 ${i + 1}` }),
                ),
              ]}
            />
            <Alert
              showIcon
              type={
                task.assessment?.verdict === 'satisfied'
                  ? 'success'
                  : task.assessment?.verdict === 'unsatisfied'
                    ? 'error'
                    : 'info'
              }
              title={task.assessment?.summary || '目标尚无完整验证证据'}
            />
            <AdminTable
              title="执行步骤"
              rowKey="id"
              dataSource={task.nodes}
              pagination={false}
              enableColumnSelection={false}
              columns={[
                { title: '步骤', dataIndex: 'id' },
                {
                  ...tableColumnPresets.status,
                  title: '状态',
                  dataIndex: 'status',
                  render: (value: string) => <StatusTag value={value} />,
                },
                { title: '进度', dataIndex: 'summary' },
                {
                  title: '审批',
                  key: 'approval',
                  render: (_: unknown, node: CapabilityTask['nodes'][number]) =>
                    node.approvalRequestId && !version ? (
                      <Link
                        to={`/ai-gateway/governance?tab=approvals&approvalRequestId=${encodeURIComponent(node.approvalRequestId)}`}
                      >
                        查看审批
                      </Link>
                    ) : (
                      '—'
                    ),
                },
              ]}
              expandable={{
                expandedRowRender: (node: CapabilityTask['nodes'][number]) => (
                  <pre className="soha-system-json-block">
                    {JSON.stringify(node.invocation ?? {}, null, 2)}
                  </pre>
                ),
              }}
            />
            <details>
              <summary>验证证据</summary>
              <pre className="soha-system-json-block">
                {JSON.stringify(task.assessment?.evidence ?? [], null, 2)}
              </pre>
            </details>
            <details>
              <summary>计划与任务 ID</summary>
              <pre className="soha-system-json-block">
                {JSON.stringify({ id: task.id, version: task.version, plan: task.plan }, null, 2)}
              </pre>
            </details>
          </Space>
        ) : null}
      </Drawer>
      {editor ? (
        <CapabilityPlanEditor
          initial={editor}
          onClose={() => setEditor(undefined)}
          onSubmitted={(created) => {
            setEditor(undefined)
            selectTask(created.id)
          }}
        />
      ) : null}
    </>
  )
}

function CapabilityPlanEditor({
  initial,
  onClose,
  onSubmitted,
}: {
  initial: { value: string; task?: CapabilityTask }
  onClose: () => void
  onSubmitted: (task: CapabilityTask) => void
}) {
  const [value, setValue] = useState(initial.value)
  const [validated, setValidated] = useState('')
  const { message, modal } = App.useApp()
  const client = useQueryClient()
  const validation = useOperationMutation(capabilityTaskMutations.validate(), {
    label: ['校验目标计划', 'Validate capability plan'],
    target: () => '当前目标计划',
    readOnly: true,
  })
  const create = useOperationMutation(capabilityTaskMutations.create(client), {
    label: ['提交目标计划', 'Submit capability plan'],
    target: (v) => v.idempotencyKey,
  })
  const resume = useOperationMutation(capabilityTaskMutations.resume(client), {
    label: ['修订并续接目标任务', 'Resume capability task'],
    target: (v) => v.id,
  })
  const blocked = create.blocked || resume.blocked || validation.blocked
  const denied = queryAccessDenied(create) || queryAccessDenied(resume)
  useEffect(() => {
    if (denied) {
      setValue('')
      setValidated('')
      onClose()
    }
  }, [denied, onClose])
  const validate = async () => {
    if (blocked) return
    try {
      const input = JSON.parse(value) as CapabilityTaskInput | CapabilityTaskRevisionInput
      const result = await validation.mutateAsync(
        initial.task
          ? { idempotencyKey: `revision:${initial.task.id}`, plan: input.plan }
          : (input as CapabilityTaskInput),
      )
      setValidated(result.data.valid ? value : '')
    } catch (error) {
      void message.error(error instanceof Error ? error.message : '计划 JSON 无效')
    }
  }
  const submit = () => {
    if (blocked || !validated || validated !== value) return
    modal.confirm({
      title: initial.task ? '提交修订并续接？' : '提交目标计划？',
      content: '提交后逐步执行，所需审批会进入现有审批队列。',
      okText: '提交',
      onOk: async () => {
        try {
          const result = initial.task
            ? await resume.mutateAsync({
                id: initial.task.id,
                input: JSON.parse(value) as CapabilityTaskRevisionInput,
              })
            : await create.mutateAsync(JSON.parse(value) as CapabilityTaskInput)
          onSubmitted(result.data)
        } catch (error) {
          // The editor keeps the request receipt and draft visible after failure.
          void error
        }
      },
    })
  }
  return (
    <Drawer
      title={initial.task ? '修订目标计划' : '提交目标计划'}
      size="min(800px, 100vw)"
      open
      closable={!blocked}
      keyboard={!blocked}
      mask={{ closable: false }}
      onClose={() => {
        if (!blocked) onClose()
      }}
      extra={
        <Space>
          <Button disabled={blocked} loading={validation.isPending} onClick={() => void validate()}>
            校验计划
          </Button>
          <Button
            type="primary"
            disabled={blocked || !validated || validated !== value}
            loading={create.isPending || resume.isPending}
            onClick={submit}
          >
            提交
          </Button>
        </Space>
      }
    >
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        {create.feedback}
        {resume.feedback}
        {validation.feedback}
        {initial.task ? (
          <Alert
            showIcon
            type="info"
            title="保留原目标与未结束步骤；重新验证时使用新的步骤 ID。历史计划及已完成操作会保留。"
          />
        ) : null}
        <label htmlFor="capability-plan-json">计划 JSON</label>
        <Input.TextArea
          disabled={blocked}
          id="capability-plan-json"
          value={value}
          autoSize={{ minRows: 16, maxRows: 32 }}
          spellCheck={false}
          onChange={(event) => {
            setValue(event.target.value)
            setValidated('')
            validation.reset()
          }}
        />
        {validation.data ? (
          <Alert
            showIcon
            type={validated === value ? 'success' : 'error'}
            title={validated === value ? '计划校验通过' : '计划需要调整'}
            description={validation.data.data.issues
              .map((issue) => `${issue.stepId ?? ''} ${issue.message}`)
              .join('\n')}
          />
        ) : null}
      </Space>
    </Drawer>
  )
}
