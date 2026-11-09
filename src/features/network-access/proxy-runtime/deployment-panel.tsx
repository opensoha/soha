import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Checkbox,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Typography,
} from 'antd'
import type {
  NetworkProxyDeployment,
  NetworkProxyDeploymentControlInput,
  NetworkProxyDeploymentMember,
  NetworkProxyDeploymentInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { workspaceApi } from './workspace-api'
import { workspaceQueries, workspaceKeys } from './workspace-queries'
type DeploymentAction = Exclude<NetworkProxyDeploymentControlInput['action'], 'reconcile'>
type DeploymentConfirmation = {
  id: string
  input: NetworkProxyDeploymentControlInput
  observation: NetworkProxyDeployment
}
export function DeploymentPanel({ id, canUpdate }: { id: string; canUpdate: boolean }) {
  const client = useQueryClient()
  const query = useQuery(workspaceQueries.deployment(id, true))
  const data = readableQueryData(query)
  const [bindingOpen, setBindingOpen] = useState(false)
  const [confirmation, setConfirmation] = useState<DeploymentConfirmation>()
  const [confirmationStale, setConfirmationStale] = useState(false)
  const [bindingForm] = Form.useForm<NetworkProxyDeploymentInput>()
  const [replicas, setReplicas] = useState<number | null>(1)
  const refresh = () => client.invalidateQueries({ queryKey: workspaceKeys.deployment(id) })
  const control = useOperationMutation(
    {
      mutationFn: (target: DeploymentConfirmation) => workspaceApi.control(target.id, target.input),
      onSuccess: () => void refresh(),
    },
    {
      label: ['部署操作', 'Deployment operation'],
      target: (target) => target.input.action,
      receipt: (d) => ({ message: `操作 ${d.status}，等待部署观察。`, evidence: d.operationId }),
    },
  )
  const reconcile = useOperationMutation(
    {
      mutationFn: () =>
        workspaceApi.control(id, {
          requestId: crypto.randomUUID(),
          expectedRevision: data!.revision,
          action: 'reconcile',
        }),
      onSuccess: () => void refresh(),
    },
    { label: ['核对部署', 'Reconcile deployment'], target: () => id, readOnly: true },
  )
  const bind = useOperationMutation(
    {
      mutationFn: (input: NetworkProxyDeploymentInput) =>
        workspaceApi.bind(id, { ...input, requestId: crypto.randomUUID() }),
      onSuccess: () => void refresh(),
    },
    { label: ['移交部署控制权', 'Adopt deployment'], target: () => data?.slotId ?? '' },
  )
  const blocked =
    !canUpdate ||
    !data ||
    query.isError ||
    control.blocked ||
    bind.blocked ||
    ['unknown', 'conflict', 'pending', 'starting', 'draining', 'unsupported', 'unbound'].includes(
      data.state,
    )
  function controlConfirmation(
    action: DeploymentAction,
    title: string,
    label: string,
    description?: string,
  ) {
    const pending = confirmation?.input.action === action ? confirmation : undefined
    const disabled = blocked || data?.owner !== 'soha-managed' || (action === 'scale' && !replicas)
    return (
      <Popconfirm
        title={title}
        description={
          <>
            {description}
            {pending && (
              <div>
                {pending.id} · 版本 {pending.input.expectedRevision}
                {pending.input.replicas !== undefined && ` · 目标副本 ${pending.input.replicas}`}
              </div>
            )}
          </>
        }
        open={Boolean(pending)}
        onOpenChange={(open) => {
          if (!open) {
            setConfirmation((current) => (current?.input.action === action ? undefined : current))
            return
          }
          if (disabled || !data) return
          setConfirmationStale(false)
          setConfirmation({
            id,
            observation: data,
            input: {
              action,
              expectedRevision: data.revision,
              requestId: crypto.randomUUID(),
              drainSeconds: 120,
              ...(action === 'scale' && replicas !== null ? { replicas } : {}),
            },
          })
        }}
        onConfirm={() => {
          setConfirmation(undefined)
          if (!pending) return
          const latest = client.getQueryState(workspaceKeys.deployment(id))
          if (
            !canUpdate ||
            blocked ||
            pending.id !== id ||
            latest?.status !== 'success' ||
            latest.data !== pending.observation
          ) {
            setConfirmationStale(true)
            return
          }
          control.mutate(pending)
        }}
      >
        <Button danger={action === 'stop'} disabled={disabled}>
          {label}
        </Button>
      </Popconfirm>
    )
  }
  return (
    <section className="soha-proxy-workspace-section">
      <div className="soha-server-proxy-actions">
        <h2>部署与运行</h2>
        <Button onClick={() => void query.refetch()}>刷新观察</Button>
      </div>
      <ReadFeedback canRead query={query} label="部署" />
      <Alert
        type={data?.state === 'ready' ? 'success' : 'info'}
        title={`部署 · ${data?.state ?? '未知'}`}
        description={
          data?.reasonCode === 'rebind_required'
            ? '批准目标已发生变更，需要重新绑定；启动和调整副本已暂停。'
            : (data?.reasonCode ?? '仅操作已由安装者批准的部署目标。配置启用与部署运行分别管理。')
        }
      />
      <div className="soha-proxy-workspace-facts">
        <span>控制方：{data?.owner ?? '未绑定'}</span>
        <span>目标副本：{data?.desiredReplicas ?? '未知'}</span>
        <span>观察副本：{data?.observedReplicas ?? '未知'}</span>
        <span>配置版本：{data?.revision ?? '—'}</span>
      </div>
      <div className="soha-proxy-workspace-facts">
        <span>当前接受的批准目标：{data?.slotId ?? '未绑定'}</span>
        <Typography.Text style={{ overflowWrap: 'anywhere' }}>
          当前接受的对象 UID：{data?.objectUID ?? '未知'}
        </Typography.Text>
        <Typography.Text style={{ overflowWrap: 'anywhere' }}>
          当前接受的模板摘要：{data?.templateHash ?? '未知'}
        </Typography.Text>
      </div>
      <Typography.Paragraph type="secondary">
        以上是当前绑定接受的身份，不证明安装者已更新镜像或候选内核已运行。
      </Typography.Paragraph>
      <div>
        <h3>受控升级与回退</h3>
        <Typography.Paragraph>
          当前停止证据：
          {query.isError
            ? '观察读取失败，无法确认停止。'
            : data?.state === 'stopped' && data.observedReplicas === 0
              ? '观察为已停止、0 副本；仍须核对原停止操作的成功回执。'
              : '尚未观察到已停止且副本为 0，不能视为已完成停止。'}
        </Typography.Paragraph>
        <ol>
          <li>
            先停止接入，等待原停止操作的可信成功回执，并确认观察为已停止、0
            副本。未知结果先核对原操作，不当作停止成功。
          </li>
          <li>
            安装者在本地对固定、可信的升级或回退候选执行{' '}
            <Typography.Text code>--kernel-preflight &lt;私有配置文件&gt;</Typography.Text>
            。此检查仅验证解析层，不证明真实运行或网络保护；不上传私有文件。
          </li>
          <li>
            安装者更新批准镜像，并导出更新后的批准目标、对象 UID
            与模板摘要。页面不下载内核，也不接受任意镜像。
          </li>
          <li>
            明确填写新批准信息并提交重新绑定。填表和接管勾选不是批准证明，服务端会核对目标及停止证据。
          </li>
          <li>
            绑定成功后启动，再核对实际运行时版本与诊断。升级和回退都遵循同一过程，受理或预检通过不等于运行成功。
          </li>
        </ol>
      </div>
      {(!data?.slotId || data.state === 'unbound') && (
        <Alert
          type="warning"
          title="待安装者配置批准的部署目标"
          description="由安装者提供批准目标信息后可在下方绑定。服务端会精确校验安装策略，不能提交任意部署模板。"
        />
      )}
      {data?.owner && data.owner !== 'soha-managed' && (
        <Alert
          type="warning"
          title="当前由外部工具管理"
          description="停止原控制方并满足安装者接管条件后，才可显式移交。"
          action={
            <Popconfirm
              title="已停止原控制方，确认将批准目标交由 Soha 管理？"
              onConfirm={() => {
                bindingForm.setFieldsValue({
                  expectedRevision: data.revision,
                  slotId: data.slotId,
                  objectUID: data.objectUID,
                  templateHash: data.templateHash,
                  owner: 'soha-managed',
                  ownerEpoch: data.ownerEpoch ?? 0,
                  adopt: true,
                })
                setBindingOpen(true)
              }}
            >
              <Button
                disabled={!canUpdate || bind.blocked || !data.objectUID || !data.templateHash}
              >
                移交控制权
              </Button>
            </Popconfirm>
          }
        />
      )}
      <Button
        disabled={!canUpdate || bind.blocked || !data}
        onClick={() => {
          bindingForm.setFieldsValue({
            expectedRevision: data?.revision ?? 0,
            ownerEpoch: data?.ownerEpoch ?? 0,
            owner: 'soha-managed',
            adopt: false,
          })
          setBindingOpen(!bindingOpen)
        }}
      >
        绑定安装者批准的目标
      </Button>
      {bindingOpen && (
        <Form form={bindingForm} layout="vertical" onFinish={(input) => bind.mutate(input)}>
          <Alert
            type="info"
            title="填写安装者导出的批准目标信息"
            description="Soha 仅绑定已有批准工作负载，不接受 YAML、镜像或任意模板。移交控制权前必须停止原控制方。"
          />
          <div className="soha-proxy-workspace-fields">
            <Form.Item name="slotId" label="批准目标（slot）" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="objectUID" label="批准对象 UID" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="templateHash" label="批准模板摘要" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="expectedRevision" label="当前配置版本" rules={[{ required: true }]}>
              <InputNumber min={0} />
            </Form.Item>
            <Form.Item name="ownerEpoch" label="安装者控制权版本" rules={[{ required: true }]}>
              <InputNumber min={0} />
            </Form.Item>
            <Form.Item name="owner" label="目标控制方" rules={[{ required: true }]}>
              <Select
                options={['soha-managed', 'gitops', 'helm', 'manual', 'external'].map((value) => ({
                  value,
                  label: value,
                }))}
              />
            </Form.Item>
          </div>
          <Form.Item name="adopt" valuePropName="checked">
            <Checkbox>显式接管：已停止原控制方，安装者允许移交此目标</Checkbox>
          </Form.Item>
          <Popconfirm
            title="确认批准目标、对象和控制方并提交绑定？"
            onConfirm={() => bindingForm.submit()}
          >
            <Button type="primary" disabled={!canUpdate || bind.blocked}>
              提交绑定
            </Button>
          </Popconfirm>
        </Form>
      )}
      <Space wrap>
        {controlConfirmation('start', '启动批准的部署目标？', '启动')}
        <InputNumber
          aria-label="目标副本"
          min={1}
          max={100}
          value={replicas}
          onChange={setReplicas}
        />
        {controlConfirmation(
          'scale',
          '调整副本可能排空现有连接，确认继续？',
          '调整副本',
          '缩容须取得排空或受控停止的肯定确认；排空期限 120 秒，未知连接数不当作零。',
        )}
        {controlConfirmation(
          'stop',
          '停止接入并排空连接后停止部署？',
          '停止',
          '默认最多排空 120 秒；到期后执行受控停止，取得停止的肯定确认后才允许缩容。连接数或停止结果未知均不当作零或成功。',
        )}
        <Button
          disabled={!canUpdate || !data || reconcile.blocked || control.isPending || bind.isPending}
          onClick={() => reconcile.mutate()}
        >
          核对原操作
        </Button>
      </Space>
      {confirmationStale && (
        <Alert type="warning" title="部署观察或权限已变化，请刷新后重新确认。" />
      )}
      {control.feedback}
      {reconcile.feedback}
      {bind.feedback}
      <Typography.Paragraph type="secondary">
        操作受理不等于部署完成。结果未知时先刷新观察并核对原操作，不重放启动或扩缩容。
      </Typography.Paragraph>
      <AdminTable
        rowKey="runtimeId"
        pagination={false}
        dataSource={data?.members ?? []}
        columns={[
          { title: '运行成员', dataIndex: 'runtimeId' },
          {
            title: '就绪',
            render: (_: unknown, m: NetworkProxyDeploymentMember) => (m.ready ? '是' : '否'),
          },
          {
            title: '流量就绪',
            render: (_: unknown, m: NetworkProxyDeploymentMember) => (m.trafficReady ? '是' : '否'),
          },
          {
            title: '活动连接',
            render: (_: unknown, m: NetworkProxyDeploymentMember) =>
              m.activeCountKnown ? (m.activeConnections ?? '未知') : '未知',
          },
          { title: '已应用版本', dataIndex: 'ackRevision' },
          { title: '最近观察', dataIndex: 'observedAt' },
        ]}
      />
    </section>
  )
}
