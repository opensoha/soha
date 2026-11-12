import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Checkbox,
  Descriptions,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
} from 'antd'
import type { TableColumnsType } from 'antd'
import type {
  NetworkRADIUSMember,
  NetworkRADIUSControlInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData, queryAccessDenied } from '@/utils/query-feedback'
import { radiusApi } from './api'
import { radiusKeys, canScale, memberFresh, stateCount, stopped, type RadiusCommand } from './model'

export function RadiusServicesPanel({
  canRead,
  canCreate,
  canUpdate,
}: {
  canRead: boolean
  canCreate: boolean
  canUpdate: boolean
}) {
  const client = useQueryClient()
  const pools = useQuery({ queryKey: radiusKeys.all, queryFn: radiusApi.list, enabled: canRead })
  const options = useQuery({
    queryKey: [...radiusKeys.all, 'options'],
    queryFn: radiusApi.options,
    enabled: canRead && (canCreate || canUpdate),
  })
  const [creating, setCreating] = useState(false)
  const [form] = Form.useForm<{
    name: string
    slotId: string
    configurationRef: string
    createApprovedBinding?: boolean
  }>()
  const slotId = Form.useWatch('slotId', form)
  const createApprovedBinding = Form.useWatch('createApprovedBinding', form)
  const [reference, setReference] = useState<string>()
  const [selected, setSelected] = useState<string>()
  const [pending, setPending] = useState<RadiusCommand>()
  const [replicas, setReplicas] = useState<number | null>(1)
  const [, tick] = useState(0)
  useEffect(() => {
    const timer = window.setInterval(() => tick((value) => value + 1), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const denied = !canRead || queryAccessDenied(pools)
  const items = denied ? [] : (readableQueryData(pools)?.items ?? [])
  const pool = items.find((p) => p.poolId === selected)
  const slots = readableQueryData(options)?.slots ?? []
  const chosenSlot = slots.find((s) => s.slotId === slotId)
  const poolSlot = slots.find((s) => s.slotId === pool?.slotId)
  const mutation = useOperationMutation(
    {
      mutationFn: (command: RadiusCommand) => {
        if (denied || (command.kind === 'create' ? !canCreate : !canUpdate))
          throw new Error('权限已撤回')
        if (command.kind === 'create') return radiusApi.create(command.input)
        if (command.kind === 'configure') return radiusApi.configure(command.id, command.input)
        return radiusApi.control(command.id, command.input)
      },
      onSuccess: () => {
        setPending(undefined)
        setCreating(false)
        form.resetFields()
        void client.invalidateQueries({ queryKey: radiusKeys.all })
      },
    },
    {
      label: ['RADIUS 管理请求', 'RADIUS management request'],
      target: (command) => (command.kind === 'create' ? command.input.name : command.id),
      receipt: (result) => ({
        message: `请求已返回，当前状态 ${result.state}；请刷新核对实际观察。`,
        evidence: result.operationId,
      }),
    },
  )
  useEffect(() => {
    setSelected(undefined)
    setPending(undefined)
    setCreating(false)
    setReference(undefined)
    form.resetFields()
  }, [denied, canCreate, canUpdate, form])
  const blocked = denied || mutation.blocked || pools.isError || pools.isFetching
  function control(action: NetworkRADIUSControlInput['action']) {
    if (!pool || blocked || !canUpdate) return
    if (action === 'scale' && !canScale(pool)) return
    if (action === 'start' && !stopped(pool)) return
    setPending({
      kind: 'control',
      id: pool.poolId,
      input: {
        action,
        requestId: crypto.randomUUID(),
        expectedRevision: pool.revision,
        ...(action === 'scale' ? { replicas: replicas! } : {}),
        ...(action === 'stop' ? { drainSeconds: 90 } : {}),
      },
    })
  }
  async function prepareCreate() {
    if (blocked || !canCreate || options.isError) return
    const values = await form.validateFields().catch(() => undefined)
    if (!values) return
    const slot = slots.find((s) => s.slotId === values.slotId && s.available && !s.reservedPoolId)
    const bootstrap = values.createApprovedBinding === true
    if (
      !slot ||
      !slot.configurationRefs.includes(values.configurationRef) ||
      (bootstrap
        ? slot.bindingExists || !slot.createApprovedBindingRequired
        : !slot.bindingExists || !slot.bindingUpdatedAt || slot.createApprovedBindingRequired)
    )
      return
    setPending({
      kind: 'create',
      input: {
        ...values,
        siteId: slot.siteId,
        nasBindingId: slot.nasBindingId,
        ...(bootstrap
          ? { createApprovedBinding: true }
          : { expectedBindingUpdatedAt: slot.bindingUpdatedAt }),
        requestId: crypto.randomUUID(),
      },
    })
  }
  return (
    <Flex component="section" vertical gap="middle" aria-label="FreeRADIUS 托管服务">
      <h3>FreeRADIUS 托管服务</h3>
      <Alert
        type="info"
        showIcon
        title="默认关闭，创建配置不会隐式部署"
        description="仅使用安装者批准的 slot 与配置引用。NAS 必须显式配置各成员端点为主备；不提供普通 UDP 轮询。停止影响新认证，NAS 已授权端口仍由会话租约和撤权命令管理。PEAP/MSCHAPv2 尚无兼容身份后端，不支持。"
      />
      <ReadFeedback canRead={canRead} query={pools} label="RADIUS 池" />
      {!denied && pools.isSuccess && items.length === 0 && (
        <p>尚无托管池；外部 NAS 不提供托管启停控制。</p>
      )}
      {mutation.feedback}
      <ReadFeedback
        canRead={canRead && (canCreate || canUpdate)}
        query={options}
        label="批准的 RADIUS 目标"
      />
      <Space wrap>
        <Button onClick={() => void pools.refetch()} disabled={!canRead}>
          刷新观察
        </Button>
        <Select
          aria-label="选择 RADIUS 池"
          placeholder="选择 RADIUS 池"
          value={denied ? undefined : selected}
          options={items.map((p) => ({ value: p.poolId, label: `${p.name} · ${p.siteId}` }))}
          onChange={(id) => {
            setSelected(id)
            setPending(undefined)
            setReference(undefined)
          }}
        />
        {canCreate && (
          <Button
            disabled={blocked || options.isError || options.isFetching}
            onClick={() => setCreating(true)}
          >
            创建托管池
          </Button>
        )}
      </Space>
      {creating && canCreate && !denied && (
        <Form form={form} layout="vertical" disabled={blocked}>
          <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true }]}>
            <Input maxLength={200} />
          </Form.Item>
          <Form.Item name="slotId" label="批准目标 / NAS / 站点" rules={[{ required: true }]}>
            <Select
              onChange={() => {
                form.setFieldValue('configurationRef', undefined)
                form.setFieldValue('createApprovedBinding', false)
              }}
              options={slots.map((s) => ({
                value: s.slotId,
                label: `${s.slotId} / ${s.nasBindingId} / ${s.siteId}${s.reasonCode ? ` · ${s.reasonCode}` : ''}`,
                disabled: !s.available || Boolean(s.reservedPoolId),
              }))}
            />
          </Form.Item>
          <Form.Item
            name="configurationRef"
            label="批准的配置引用"
            rules={[{ required: true }]}
            extra="不传证书私钥或 NAS shared secret 正文；由服务器再次校验批准目标和同站点绑定。"
          >
            <Select
              options={(chosenSlot?.configurationRefs ?? []).map((ref) => ({
                value: ref,
                label: ref,
              }))}
            />
          </Form.Item>
          {chosenSlot?.createApprovedBindingRequired && !chosenSlot.bindingExists && (
            <Form.Item name="createApprovedBinding" valuePropName="checked">
              <Checkbox>
                显式创建批准的 NAS 绑定：{chosenSlot.nasBindingId} / {chosenSlot.siteId}
              </Checkbox>
            </Form.Item>
          )}
          <Space>
            <Button
              disabled={
                blocked ||
                (chosenSlot?.createApprovedBindingRequired
                  ? createApprovedBinding !== true
                  : !chosenSlot?.bindingExists || !chosenSlot.bindingUpdatedAt)
              }
              onClick={() => void prepareCreate()}
            >
              审核创建
            </Button>
            <Button
              onClick={() => {
                setCreating(false)
                form.resetFields()
              }}
            >
              取消创建
            </Button>
          </Space>
        </Form>
      )}
      {pool && !denied && (
        <>
          <Descriptions
            column={1}
            items={[
              { key: 'config', label: '当前配置引用', children: pool.configurationRef },
              {
                key: 'binding',
                label: 'NAS / 站点',
                children: `${pool.nasBindingId} / ${pool.siteId}`,
              },
              { key: 'state', label: '池状态', children: <StatusTag value={pool.state} /> },
              {
                key: 'replicas',
                label: '目标 / 观察 / 就绪副本',
                children: `${pool.desiredReplicas} / ${pool.observedReplicas ?? '未知'} / ${pool.readyReplicas ?? '未知'}`,
              },
              {
                key: 'revision',
                label: '修订 / 配置版本',
                children: `${pool.revision} / ${pool.configurationRevision}`,
              },
              {
                key: 'reason',
                label: '原因 / 操作',
                children: `${pool.reasonCode ?? '—'} / ${pool.operationId ?? '—'}`,
              },
            ]}
          />
          <Alert
            type="warning"
            title={
              stopped(pool)
                ? '已观察到停止，可提交配置引用更新'
                : 'stop_required：修改配置前须停止并确认观察副本为 0'
            }
            description="不提供热更新或无损保证；就绪不代表 NAS 已使用端点，流量就绪必须有实际观察。未知及停止中的回执需继续刷新核对。"
          />
          {canUpdate && (
            <Space wrap>
              <Button disabled={blocked || !stopped(pool)} onClick={() => control('start')}>
                启动
              </Button>
              <Button
                disabled={blocked || pool.state === 'stopped'}
                onClick={() => control('stop')}
              >
                停止新认证并排空
              </Button>
              <InputNumber
                aria-label="目标 RADIUS 副本"
                min={0}
                max={poolSlot?.maxReplicas}
                precision={0}
                value={replicas}
                onChange={setReplicas}
                disabled={blocked}
              />
              <Button
                disabled={blocked || !canScale(pool) || replicas === null}
                onClick={() => control('scale')}
              >
                调整副本
              </Button>
              <Button disabled={blocked} onClick={() => control('reconcile')}>
                核对控制状态
              </Button>
              <Select
                aria-label="新配置引用"
                placeholder="批准的新配置引用"
                value={reference}
                onChange={setReference}
                disabled={blocked || !stopped(pool) || options.isError}
                options={(poolSlot?.configurationRefs ?? []).map((ref) => ({
                  value: ref,
                  label: ref,
                }))}
              />
              <Button
                disabled={
                  blocked ||
                  !stopped(pool) ||
                  options.isError ||
                  !reference ||
                  !poolSlot?.configurationRefs.includes(reference)
                }
                onClick={() => {
                  if (reference)
                    setPending({
                      kind: 'configure',
                      id: pool.poolId,
                      input: {
                        configurationRef: reference,
                        expectedRevision: pool.revision,
                        requestId: crypto.randomUUID(),
                      },
                    })
                }}
              >
                更新配置引用
              </Button>
            </Space>
          )}
          <AdminTable
            scroll={{ x: 'max-content' }}
            rowKey="memberId"
            dataSource={pool.members}
            pagination={false}
            enableColumnSelection={false}
            columns={
              [
                {
                  title: '成员 / 运行时',
                  key: 'id',
                  render: (_, m) => `${m.memberId} / ${m.runtimeId}`,
                },
                { title: 'NAS 主备端点', dataIndex: 'endpoint' },
                { title: '版本', dataIndex: 'runtimeVersion', render: (v) => v ?? '未知' },
                {
                  title: '配置 / 控制观察',
                  key: 'revision',
                  render: (_, m) =>
                    `${m.configurationRevision ?? '未知'} / ${m.controlRevision ?? '未知'}`,
                },
                { title: '活动 EAP State', key: 'count', render: (_, m) => stateCount(m) },
                {
                  title: '就绪 / 流量就绪',
                  key: 'ready',
                  render: (_, m) =>
                    `${memberFresh(m) && m.ready ? '就绪' : '未就绪'} / ${memberFresh(m) && m.ready && m.trafficReady ? '流量就绪' : '待 NAS 使用观察'}`,
                },
                {
                  title: '最后观察 / 租约',
                  key: 'observation',
                  render: (_, m) => `${m.lastObservedAt ?? '未知'} / ${m.leaseUntil ?? '未知'}`,
                },
                { title: '原因', dataIndex: 'reasonCode' },
              ] satisfies TableColumnsType<NetworkRADIUSMember>
            }
          />
        </>
      )}
      {!denied && (canCreate || canUpdate) && (
        <Modal
          okText="确定"
          cancelText="取消"
          open={Boolean(pending) && !denied}
          title="确认 RADIUS 管理变更"
          onCancel={() => {
            if (!mutation.isPending) setPending(undefined)
          }}
          confirmLoading={mutation.isPending}
          okButtonProps={{
            disabled: blocked || !pending || (pending.kind === 'create' ? !canCreate : !canUpdate),
          }}
          onOk={() => {
            if (pending && !blocked) mutation.mutate(pending)
          }}
        >
          <p>
            操作：{pending?.kind === 'control' ? pending.input.action : pending?.kind}
            。启停或缩容可能中断正在进行的认证；不会自动撤销 NAS 已授权端口。
          </p>
          <p>请求 ID：{pending?.input.requestId}</p>
          {pending?.kind === 'create' && (
            <p>
              批准目标：{pending.input.slotId} / NAS {pending.input.nasBindingId} / 站点{' '}
              {pending.input.siteId} / 配置 {pending.input.configurationRef}。
              {pending.input.createApprovedBinding
                ? '同时创建上述批准的 NAS 绑定。'
                : '关联已有 NAS 绑定。'}
              池保持默认关闭。
            </p>
          )}
          <p>
            版本：
            {pending && 'expectedRevision' in pending.input
              ? pending.input.expectedRevision
              : 'NAS 绑定更新时间校验'}
          </p>
          {mutation.feedback}
        </Modal>
      )}
    </Flex>
  )
}
