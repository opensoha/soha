import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Form, Input, Select, Space, Typography } from 'antd'
import type {
  NetworkProxyInstance,
  NetworkProxyRuntimeCommandInput,
  NetworkProxyRuntimeCommandKind,
  NetworkProxyGroup,
  NetworkProxyRule,
  NetworkProxyRuntimeCommandResult,
  NetworkProxyRuntimeCommandReceipt,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { ApiError } from '@/services/api-error'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { workspaceApi } from './workspace-api'
import { workspaceKeys, workspaceQueries } from './workspace-queries'
type RuntimeRequest = { instanceId: string; input: NetworkProxyRuntimeCommandInput }
const terminalStatuses = [
  'succeeded',
  'failed',
  'expired',
  'cancelled',
  'unsupported',
  'unavailable',
]
function matchesCommand(target: RuntimeRequest, receipt?: NetworkProxyRuntimeCommandReceipt) {
  return (
    receipt?.commandId === target.input.requestId &&
    receipt.requestId === target.input.requestId &&
    receipt.processEpoch === target.input.processEpoch &&
    receipt.kind === target.input.kind
  )
}
export function RuntimePanel({
  instance,
  canUpdate,
  diagnostics = false,
}: {
  instance: NetworkProxyInstance
  canUpdate: boolean
  diagnostics?: boolean
}) {
  const id = instance.id
  const client = useQueryClient()
  const [submitted, setSubmitted] = useState<RuntimeRequest>()
  const [form] =
    Form.useForm<
      Pick<NetworkProxyRuntimeCommandInput, 'groupId' | 'nodeId' | 'probeProfileId' | 'sourceId'>
    >()
  const [groups, setGroups] = useState<NetworkProxyGroup[]>([])
  const supports = (kind: NetworkProxyRuntimeCommandKind) => instance.capabilities.includes(kind)
  const queue = useOperationMutation(
    {
      mutationFn: async (target: RuntimeRequest) => {
        const result = await workspaceApi.queue(target.instanceId, target.input)
        if (!matchesCommand(target, result))
          throw new ApiError(0, '返回的命令回执与原请求不符，请读取原回执。', { method: 'POST' })
        return result
      },
      onMutate: setSubmitted,
      onError: (error, target) => {
        if (!writeOutcomeUnknown(error))
          setSubmitted((current) => (current === target ? undefined : current))
      },
      onSuccess: (result, target) =>
        client.setQueryData(
          workspaceKeys.command(target.instanceId, target.input.requestId),
          result,
        ),
    },
    {
      label: ['执行代理操作', 'Run proxy operation'],
      target: (v) => v.input.kind,
      receipt: (d) => ({ message: '请求已受理，以运行时回读为准。', evidence: d.commandId }),
    },
  )
  const requestId = submitted?.input.requestId ?? ''
  const receipt = useQuery(
    workspaceQueries.command(
      submitted?.instanceId ?? id,
      requestId,
      Boolean(submitted && !queue.isPending),
    ),
  )
  const candidate = readableQueryData(receipt)
  const data = submitted && matchesCommand(submitted, candidate) ? candidate : undefined
  const cancel = useOperationMutation(
    {
      mutationFn: (target: RuntimeRequest) =>
        workspaceApi.cancel(target.instanceId, target.input.requestId),
      onSuccess: (_, target) =>
        void client.invalidateQueries({
          queryKey: workspaceKeys.command(target.instanceId, target.input.requestId),
        }),
    },
    {
      label: ['取消代理操作', 'Cancel proxy operation'],
      target: (target) => target.input.requestId,
    },
  )
  const terminal = Boolean(data && terminalStatuses.includes(data.status))
  useEffect(() => {
    if (submitted?.instanceId === id && data?.result?.groups) setGroups(data.result.groups)
    if (!submitted || !terminal || receipt.isError) return
    if (queue.outcomeUnknown) queue.resolveUnknown(submitted)
    if (cancel.outcomeUnknown) cancel.resolveUnknown(submitted)
  }, [id, submitted, data, terminal, receipt.isError, queue, cancel])
  const blocked =
    queue.blocked || Boolean(submitted && !terminal) || cancel.blocked || !instance.processEpoch
  const canCancel = Boolean(
    canUpdate &&
    submitted?.instanceId === id &&
    submitted?.input.processEpoch === instance.processEpoch &&
    !receipt.isError &&
    data &&
    ['pending', 'running'].includes(data.status) &&
    !cancel.blocked,
  )
  function run(
    kind: NetworkProxyRuntimeCommandKind,
    extra: Partial<NetworkProxyRuntimeCommandInput> = {},
  ) {
    if (
      blocked ||
      !instance.processEpoch ||
      !supports(kind) ||
      (!canUpdate && ['select', 'refresh'].includes(kind))
    )
      return
    const now = Date.now()
    queue.mutate({
      instanceId: id,
      input: {
        ...extra,
        requestId: crypto.randomUUID(),
        processEpoch: instance.processEpoch,
        kind,
        issuedAt: new Date(now).toISOString(),
        expiresAt: new Date(now + 30000).toISOString(),
        limit: 100,
      },
    })
  }
  return (
    <section className="soha-proxy-workspace-section">
      <h2>{diagnostics ? 'DNS 与诊断' : '节点与策略组'}</h2>
      <Typography.Paragraph type="secondary">
        {instance.engine === 'external' ? instance.engineDescriptor?.engineId : instance.engine} ·{' '}
        {instance.engineVersion ?? '版本未知'}
        。能力以运行时回执为准；未知不自动启用。不支持与暂不可用分别保留原因。
      </Typography.Paragraph>
      {!instance.processEpoch && <Alert type="warning" title="尚未取得运行时状态，不能提交操作" />}
      <Typography.Paragraph type="secondary">
        {instance.capabilities.length
          ? `已声明能力：${instance.capabilities.join('、')}`
          : '能力未知，请等待运行时报告。'}{' '}
        未声明的动作不可用。
      </Typography.Paragraph>
      <Space wrap>
        {(diagnostics ? ['rules', 'dns', 'logs', 'diagnostics'] : ['groups']).map((kind) => (
          <Button
            key={kind}
            disabled={blocked || !supports(kind as NetworkProxyRuntimeCommandKind)}
            onClick={() => run(kind as NetworkProxyRuntimeCommandKind)}
          >
            {
              (
                {
                  rules: '读取实际规则',
                  dns: '读取 DNS',
                  logs: '读取最近日志',
                  diagnostics: '检查诊断',
                  groups: '读取策略组',
                } as Record<string, string>
              )[kind]
            }
          </Button>
        ))}
      </Space>
      {!diagnostics && (
        <Form form={form} layout="vertical">
          <div className="soha-proxy-workspace-fields">
            <Form.Item name="groupId" label="策略组">
              <Select
                showSearch
                options={groups.map((g) => ({ value: g.groupId, label: g.label }))}
              />
            </Form.Item>
            <Form.Item name="nodeId" label="逻辑节点">
              <Select
                showSearch
                options={[...new Set(groups.flatMap((g) => g.nodeIds) ?? [])].map((value) => ({
                  value,
                  label: value,
                }))}
              />
            </Form.Item>
            <Form.Item name="probeProfileId" label="安装者登记的探测方案">
              <Input placeholder="方案 ID，不接受任意 URL" />
            </Form.Item>
            <Form.Item name="sourceId" label="订阅来源 ID">
              <Input />
            </Form.Item>
          </div>
          <Space wrap>
            <Button
              disabled={blocked || !canUpdate || !supports('select')}
              onClick={() => {
                const v = form.getFieldsValue()
                if (v.groupId && v.nodeId) run('select', { groupId: v.groupId, nodeId: v.nodeId })
              }}
            >
              选择节点
            </Button>
            <Button
              disabled={blocked || !supports('probe')}
              onClick={() => {
                const v = form.getFieldsValue()
                if (v.probeProfileId)
                  run('probe', { probeProfileId: v.probeProfileId, nodeId: v.nodeId })
              }}
            >
              测试延迟
            </Button>
            <Button
              disabled={blocked || !canUpdate || !supports('refresh')}
              onClick={() => {
                const v = form.getFieldsValue()
                if (v.sourceId) run('refresh', { sourceId: v.sourceId })
              }}
            >
              主动更新订阅
            </Button>
          </Space>
        </Form>
      )}
      {queue.feedback}
      {cancel.feedback}
      {requestId && (
        <>
          <ReadFeedback canRead query={receipt} label="运行回执" />
          <Alert
            type={
              data?.status === 'succeeded'
                ? 'success'
                : data?.status === 'failed'
                  ? 'error'
                  : 'info'
            }
            title={`运行回执 · ${data?.status ?? '未知'}`}
            description={
              <>
                {data?.reasonCode ?? '等待运行时确认；未知结果请读取原回执。'}
                <br />
                请求：{requestId}
                {data?.result?.latencyMilliseconds !== undefined && (
                  <p>延迟 {data.result.latencyMilliseconds} ms</p>
                )}
                {data?.result?.selectedNodeId && <p>实际选中：{data.result.selectedNodeId}</p>}
              </>
            }
            action={
              <Space>
                <Button onClick={() => void receipt.refetch()}>读取原回执</Button>
                {canUpdate && (
                  <Button
                    disabled={!canCancel}
                    onClick={() => {
                      if (canCancel && submitted) cancel.mutate(submitted)
                    }}
                  >
                    取消请求
                  </Button>
                )}
              </Space>
            }
          />
        </>
      )}
      {data?.result?.groups && (
        <AdminTable
          rowKey="groupId"
          pagination={false}
          dataSource={data.result.groups}
          columns={[
            { title: '策略组', dataIndex: 'label' },
            { title: '类型', dataIndex: 'type' },
            {
              title: '逻辑节点',
              render: (_: unknown, g: NetworkProxyGroup) => g.nodeIds.join('、'),
            },
            { title: '实际选中', dataIndex: 'selectedNodeId' },
          ]}
        />
      )}
      {data?.result?.rules && (
        <AdminTable
          rowKey="ruleId"
          pagination={false}
          dataSource={data.result.rules}
          columns={[
            { title: '规则', dataIndex: 'ruleId' },
            { title: '来源', dataIndex: 'source' },
            {
              title: '匹配',
              render: (_: unknown, r: NetworkProxyRule) =>
                `${r.matcher.type} ${r.matcher.value ?? ''}`,
            },
            {
              title: '动作',
              render: (_: unknown, r: NetworkProxyRule) =>
                `${r.action.type} ${r.action.targetId ?? ''}`,
            },
          ]}
        />
      )}
      {data?.result?.dns && (
        <AdminTable
          rowKey="name"
          pagination={false}
          dataSource={data.result.dns}
          columns={[
            { title: '名称', dataIndex: 'name' },
            {
              title: '地址',
              render: (
                _: unknown,
                r: NonNullable<NetworkProxyRuntimeCommandResult['dns']>[number],
              ) => r.addresses.join('、'),
            },
            { title: '到期时间', dataIndex: 'expiresAt' },
          ]}
        />
      )}
      {data?.result?.logs && (
        <AdminTable
          rowKey="sequence"
          pagination={false}
          dataSource={data.result.logs}
          columns={[
            { title: '时间', dataIndex: 'occurredAt' },
            { title: '级别', dataIndex: 'level' },
            { title: '脱敏日志', dataIndex: 'message' },
          ]}
        />
      )}
      {data?.result?.diagnostics && (
        <AdminTable
          rowKey="code"
          pagination={false}
          dataSource={data.result.diagnostics}
          columns={[
            { title: '检查', dataIndex: 'code' },
            { title: '状态', dataIndex: 'state' },
            { title: '说明', dataIndex: 'message' },
          ]}
        />
      )}
      {diagnostics && (
        <Typography.Paragraph type="secondary">
          DNS 和实际规则只读。日志最多读取 100 项，仅显示服务端脱敏白名单结果。
        </Typography.Paragraph>
      )}
    </section>
  )
}
