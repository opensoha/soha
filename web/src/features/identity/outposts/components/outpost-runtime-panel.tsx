import { useRef, useState } from 'react'
import {
  useIsMutating,
  useMutationState,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Alert, Button, Checkbox, Descriptions, Input, InputNumber, Space, Typography } from 'antd'
import { OperationFeedback, ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { isApiError } from '@/services/api-error'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { identityOutpostMutations } from '../mutations'
import { identityOutpostMutationKeys } from '../keys'
import { identityOutpostQueries } from '../queries'
import type {
  IdentityOutpostRuntime,
  IdentityOutpostRuntimeControlInput,
  OutpostRuntimeIntent,
} from '../types'

const phases = {
  pending: '等待处理',
  applying: '正在应用',
  ready: '部署就绪',
  draining: '正在排空',
  stopped: '已停止',
  failed: '失败',
  unknown: '未知',
}
export function OutpostRuntimePanel({ id }: { id: string }) {
  const snapshot = usePermissionSnapshot().data?.data
  const canRead = hasPermission(snapshot, 'identity.outposts.view')
  const canManage = hasPermission(snapshot, 'identity.outposts.update')
  const query = useQuery({ ...identityOutpostQueries.runtime(id), enabled: Boolean(id) && canRead })
  const runtime = query.isEnabled === false ? undefined : readableQueryData(query)
  const client = useQueryClient()
  const cached = useMutationState({
    filters: { mutationKey: identityOutpostMutationKeys.runtime, exact: true },
    select: (item) => item.state,
  })
    .filter((item) => (item.variables as OutpostRuntimeIntent | undefined)?.outpostId === id)
    .reverse()[0]
  const mutation = useMutation(identityOutpostMutations.runtime(client))
  const reconcileKey = [...identityOutpostMutationKeys.runtime, 'reconcile']
  const reconcileCached = useMutationState({
    filters: { mutationKey: reconcileKey, exact: true },
    select: (item) => item.state,
  })
    .filter((item) => (item.variables as OutpostRuntimeIntent | undefined)?.outpostId === id)
    .reverse()[0]
  const reconciliation = useMutation({
    ...identityOutpostMutations.runtime(client),
    mutationKey: reconcileKey,
  })
  const reconcileIntent =
    reconciliation.variables ?? (reconcileCached?.variables as OutpostRuntimeIntent | undefined)
  const reconcileError =
    reconciliation.error ?? (reconcileCached?.status === 'error' ? reconcileCached.error : null)
  const reconcileConflict = isApiError(reconcileError) && reconcileError.status === 409
  const reconcileTaskId = reconciliation.data?.task?.id ?? reconciliation.data?.runtime.latestTaskId
  const isReconciled = (value?: IdentityOutpostRuntime) =>
    Boolean(
      reconcileTaskId &&
      value?.latestTaskId === reconcileTaskId &&
      ['ready', 'stopped', 'failed'].includes(value.phase) &&
      value.observedGeneration !== undefined &&
      value.observedGeneration === value.generation &&
      value.observedAt &&
      value.actualReplicas !== undefined &&
      value.readyReplicas !== undefined,
    )
  const reconcileVerified = isReconciled(runtime)
  const [reconcileConfirmed, setReconcileConfirmed] = useState(false)
  const [slotId, setSlotId] = useState('')
  const [adoption, setAdoption] = useState(false)
  const [replicas, setReplicas] = useState<number | null>(1)
  const [submittedIntent, setIntent] = useState<OutpostRuntimeIntent>()
  const intent =
    submittedIntent ??
    (cached?.status === 'error' ? (cached.variables as OutpostRuntimeIntent) : undefined)
  const pendingRequests = useIsMutating({
    mutationKey: identityOutpostMutationKeys.runtime,
    predicate: (item) =>
      (item.state.variables as OutpostRuntimeIntent | undefined)?.outpostId === id,
  })
  const fence = useRef(false)
  const error = mutation.error ?? (cached?.status === 'error' ? cached.error : null)
  const conflict = isApiError(error) && error.status === 409
  const uncertain = writeOutcomeUnknown(error)
  const locked =
    pendingRequests > 0 ||
    mutation.isPending ||
    Boolean(intent && error) ||
    Boolean(reconcileError) ||
    (reconciliation.isSuccess && !reconcileConfirmed)
  const runtimeUnsettled =
    runtime?.owner === 'soha' &&
    (['applying', 'draining', 'unknown'].includes(runtime.phase) ||
      (runtime.phase === 'pending' && Boolean(runtime.latestTaskId)))
  const manageable =
    canManage &&
    runtime?.owner === 'soha' &&
    runtime.capabilities.supportsScale &&
    !runtimeUnsettled &&
    !query.isError
  const submit = async (next: OutpostRuntimeIntent) => {
    if (fence.current || pendingRequests > 0) return
    fence.current = true
    setIntent(next)
    try {
      await mutation.mutateAsync(next)
    } catch {
      /* Shared persistent feedback below. */
    } finally {
      fence.current = false
    }
  }
  const control = (action: IdentityOutpostRuntimeControlInput['action']) => {
    if (
      !runtime ||
      !manageable ||
      locked ||
      (action === 'scale' && (replicas === null || !Number.isInteger(replicas) || replicas < 0))
    )
      return
    void submit({
      outpostId: id,
      kind: 'control',
      input: {
        action,
        expectedGeneration: runtime.generation,
        requestId: crypto.randomUUID(),
        ...(action === 'scale' ? { replicas: replicas ?? undefined } : {}),
        ...(action === 'stop' ||
        (action === 'scale' && replicas !== null && replicas < runtime.desiredReplicas)
          ? { drainTimeoutSeconds: 30 }
          : {}),
      },
    })
  }
  const reconcile = async (retry?: OutpostRuntimeIntent) => {
    if (
      !runtime ||
      !canManage ||
      runtime.owner !== 'soha' ||
      !runtime.slotId ||
      !runtime.capabilities.supportsScale ||
      query.isError ||
      fence.current ||
      pendingRequests > 0
    )
      return
    fence.current = true
    setReconcileConfirmed(false)
    try {
      await reconciliation.mutateAsync(
        retry ?? {
          outpostId: id,
          kind: 'control',
          input: {
            action: 'reconcile',
            expectedGeneration: runtime.generation,
            requestId: crypto.randomUUID(),
          },
        },
      )
    } catch {
      /* Keep the original write fence and the reconciliation receipt. */
    } finally {
      fence.current = false
    }
  }
  const resetReconcileConflict = async () => {
    const result = await query.refetch()
    if (result.isError) return
    client
      .getMutationCache()
      .findAll({ mutationKey: reconcileKey, exact: true })
      .forEach((item) => {
        if (
          (item.state.variables as OutpostRuntimeIntent | undefined)?.outpostId === id &&
          item.state.status === 'error'
        )
          client.getMutationCache().remove(item)
      })
    reconciliation.reset()
  }
  const release = async () => {
    if (uncertain && !reconcileVerified) return
    const result = await query.refetch()
    if (
      !result.isError &&
      (!(uncertain || reconciliation.isSuccess) || isReconciled(result.data))
    ) {
      setReconcileConfirmed(true)
      client
        .getMutationCache()
        .findAll({ mutationKey: identityOutpostMutationKeys.runtime })
        .forEach((item) => {
          if (
            item.state.status === 'error' &&
            (item.state.variables as OutpostRuntimeIntent | undefined)?.outpostId === id
          )
            client.getMutationCache().remove(item)
        })
      setIntent(undefined)
      mutation.reset()
    }
  }
  if (!canRead) return <Alert type="warning" showIcon title="没有查看运行部署的权限" />
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <ReadFeedback canRead={canRead} query={query} label="运行部署" />
      {query.isPending && <Typography.Text>正在读取运行部署</Typography.Text>}
      <Button loading={query.isFetching} onClick={() => void query.refetch()}>
        重新读取部署
      </Button>
      <OperationFeedback
        pending={mutation.isPending}
        error={error}
        label="运行部署操作"
        target={intent ? `请求 ID：${intent.input.requestId}` : undefined}
        receipt={
          mutation.isSuccess
            ? {
                message: '请求已返回，请核对部署观察结果。',
                evidence: '任务受理或期望副本变化不代表实际部署已完成。',
              }
            : undefined
        }
      />
      <OperationFeedback
        pending={reconciliation.isPending}
        error={reconcileError}
        label="核对实际部署"
        target={reconcileIntent ? `请求 ID：${reconcileIntent.input.requestId}` : undefined}
        receipt={
          reconciliation.isSuccess
            ? {
                message: '核对请求已返回，等待控制面部署观察。',
                evidence: '只读观察批准的槽位，不重放原未知写操作。',
              }
            : undefined
        }
      />
      {reconcileError && (
        <Space wrap>
          {!reconcileConflict && reconcileIntent && (
            <Button
              disabled={pendingRequests > 0 || !canManage}
              onClick={() => void reconcile(reconcileIntent)}
            >
              重试原核对
            </Button>
          )}
          {reconcileConflict && (
            <Button disabled={pendingRequests > 0} onClick={() => void resetReconcileConflict()}>
              重新读取并确认重新核对
            </Button>
          )}
        </Space>
      )}
      {reconcileVerified && !reconcileConfirmed && (
        <Button disabled={pendingRequests > 0} onClick={() => void release()}>
          核对已完成，确认继续管理
        </Button>
      )}
      {error && intent && (
        <Alert
          showIcon
          type="warning"
          title={conflict ? '配置代次冲突' : uncertain ? '结果未知，保留原操作意图' : '操作未完成'}
          description={
            <Space orientation="vertical">
              <Typography.Text>
                {conflict
                  ? '先重新读取状态并核对当前部署；确认后才能发起新操作。'
                  : uncertain
                    ? '重试将使用相同请求 ID、配置代次和参数；普通刷新不会确认原请求结果。'
                    : '核对错误后可重试原操作，或重新读取并确认调整。'}
              </Typography.Text>
              {!conflict && (
                <Button
                  disabled={!canManage || mutation.isPending}
                  onClick={() => void submit(intent)}
                >
                  重试原操作
                </Button>
              )}
              {!uncertain && (
                <Button disabled={mutation.isPending} onClick={() => void release()}>
                  重新读取并确认调整
                </Button>
              )}
            </Space>
          }
        />
      )}
      {runtime && (
        <>
          <Descriptions
            bordered
            column={1}
            size="small"
            styles={{ content: { overflowWrap: 'anywhere' } }}
            items={[
              {
                key: 'owner',
                label: '部署所有者',
                children: runtime.owner === 'soha' ? 'Soha 管理' : '外部管理',
              },
              { key: 'slot', label: '批准的安装槽位', children: runtime.slotId || '未绑定' },
              {
                key: 'workload',
                label: '工作负载',
                children:
                  runtime.namespace && runtime.workloadName
                    ? `${runtime.namespace}/${runtime.workloadName}`
                    : '未提供',
              },
              { key: 'uid', label: '工作负载 UID', children: runtime.workloadUid || '尚未观察' },
              {
                key: 'generation',
                label: '期望 / 已观察代次',
                children: `${runtime.generation} / ${runtime.observedGeneration ?? '未知'}`,
              },
              {
                key: 'replicas',
                label: '期望 / 实际 / 就绪副本',
                children: `${runtime.desiredReplicas} / ${runtime.actualReplicas ?? '未知'} / ${runtime.readyReplicas ?? '未知'}`,
              },
              { key: 'phase', label: '部署阶段', children: phases[runtime.phase] },
              { key: 'reason', label: '原因', children: runtime.reason || '未提供' },
              { key: 'observed', label: '观察时间', children: runtime.observedAt || '尚未观察' },
              {
                key: 'task',
                label: '最近任务 ID',
                children: runtime.latestTaskId || mutation.data?.task?.id || '未提供',
              },
              {
                key: 'drain',
                label: '排空能力',
                children: runtime.capabilities.supportsDrain
                  ? '支持当前受管实例排空'
                  : '未确认或不支持排空，不能保证平滑停止',
              },
            ]}
          />
          {runtime.phase === 'pending' && !runtime.latestTaskId && runtime.owner === 'soha' && (
            <Alert
              type="info"
              showIcon
              title="槽位已绑定，尚未观察到部署"
              description="可启动安装者批准的槽位；启动后按任务 ID 和部署观察结果确认进展。"
            />
          )}
          {runtimeUnsettled && (
            <Alert
              type="warning"
              showIcon
              title="部署仍在进行中或结果未知"
              description="请核对最近任务 ID、配置代次和观察状态；重新读取不会确认部署成功，当前暂停新操作。"
            />
          )}
          <Alert
            type="info"
            showIcon
            title="部署就绪与鉴权健康分别观察"
            description="副本就绪不代表真实应用接入通过。停止或缩容会短暂排空鉴权请求，最多等待 30 秒；业务应用长连接不经过 Outpost。未确认排空能力时不允许停止或缩容。"
          />
          {!canManage && <Typography.Text type="warning">没有管理运行部署的权限。</Typography.Text>}
          {runtime.owner === 'external' && (
            <Alert
              type="info"
              showIcon
              title="外部部署不会自动接管"
              description="绑定只接受安装者批准的槽位。首次安装或接管前，需安装者核实没有其他活动控制者，或旧控制者已停止；再明确申请 Soha 管理副本，由后端安装策略批准。"
            />
          )}
          {!runtime.capabilities.supportsScale && !runtimeUnsettled && (
            <Typography.Text type="warning">
              当前部署不支持副本控制，启停和扩缩容不可用。
            </Typography.Text>
          )}
          <Typography.Text>
            核对只读观察批准槽位的
            UID、模板和资源版本，不调整副本或重试未知写；实际不符或仍需排空时保留原因，普通操作继续等待。
          </Typography.Text>
          <Space wrap>
            <Button
              disabled={
                !canManage ||
                runtime.owner !== 'soha' ||
                !runtime.slotId ||
                !runtime.capabilities.supportsScale ||
                query.isError ||
                pendingRequests > 0 ||
                Boolean(reconcileError) ||
                ['applying', 'draining'].includes(runtime.phase) ||
                (runtime.phase === 'pending' && Boolean(runtime.latestTaskId))
              }
              onClick={() => void reconcile()}
            >
              核对实际部署
            </Button>
            <Button disabled={!manageable || locked} onClick={() => control('start')}>
              启动
            </Button>
            <InputNumber
              aria-label="目标副本数"
              min={0}
              precision={0}
              value={replicas}
              onChange={setReplicas}
              disabled={!manageable || locked}
            />
            <Button
              disabled={
                !manageable ||
                locked ||
                replicas === null ||
                !Number.isInteger(replicas) ||
                replicas < 0 ||
                (replicas < runtime.desiredReplicas && !runtime.capabilities.supportsDrain)
              }
              onClick={() => control('scale')}
            >
              调整副本
            </Button>
            <Button
              danger
              disabled={!manageable || locked || !runtime.capabilities.supportsDrain}
              onClick={() => control('stop')}
            >
              停止
            </Button>
          </Space>
          <Typography.Text>
            绑定安装者提供的槽位 ID，不接受自定义命名空间、镜像或 YAML。
          </Typography.Text>
          <Input
            aria-label="安装槽位 ID"
            value={slotId}
            onChange={(event) => setSlotId(event.target.value)}
            disabled={!canManage || locked}
          />
          <Checkbox
            checked={adoption}
            onChange={(event) => setAdoption(event.target.checked)}
            disabled={!canManage || locked}
          >
            明确申请 Soha 管理副本（含首次安装，仍需安装策略批准）
          </Checkbox>
          <Button
            disabled={!canManage || locked || runtimeUnsettled || query.isError || !slotId.trim()}
            onClick={() =>
              void submit({
                outpostId: id,
                kind: 'bind',
                input: {
                  slotId: slotId.trim(),
                  expectedGeneration: runtime.generation,
                  requestId: crypto.randomUUID(),
                  explicitAdoption: adoption,
                },
              })
            }
          >
            绑定批准的槽位
          </Button>
        </>
      )}
    </Space>
  )
}
