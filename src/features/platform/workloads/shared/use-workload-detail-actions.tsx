import { readableQueryData } from '@/utils/query-feedback'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapabilityForCluster } from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import type { RolloutHistory, ScopeKey } from '@/types'
import {
  useQueryClient,
  type MutationObserverOptions,
  type QueryClient,
} from '@tanstack/react-query'
import { Alert, Button, InputNumber, Modal, Tooltip } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
type Target = { readonly scope: ScopeKey; readonly name: string; readonly uid?: string }
type Mutation<T> = (client: QueryClient) => MutationObserverOptions<void, Error, T>
export interface WorkloadDetailActionOptions {
  resource:
    | 'deployments'
    | 'statefulsets'
    | 'daemonsets'
    | 'jobs'
    | 'cronjobs'
    | 'replicasets'
    | 'replicationcontrollers'
  kind:
    | 'Deployment'
    | 'StatefulSet'
    | 'DaemonSet'
    | 'Job'
    | 'CronJob'
    | 'ReplicaSet'
    | 'ReplicationController'
  permission: string
  detail:
    | {
        name: string
        uid?: string
        desiredReplicas?: number
        suspend?: boolean
        allowedActions?: string[]
      }
    | undefined
  scope: ScopeKey
  currentRevision?: string
  mutations: {
    suspend?: Mutation<Target & { suspend: boolean }>
    restart?: Mutation<Target>
    scale?: Mutation<Target & { replicas: number }>
    remove: Mutation<Target>
    rollback?: Mutation<Target & { revision: string }>
  }
}

type Action = 'suspend' | 'scale' | 'restart' | 'rollback' | 'delete'
export function useWorkloadDetailActions({
  detail,
  scope,
  currentRevision,
  mutations,
  resource,
  kind,
  permission,
}: WorkloadDetailActionOptions) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const capability = useClusterCapabilityForCluster(
    'workload.mutations',
    localeCode,
    scope.clusterId,
  )
  const client = useQueryClient()
  const navigate = useNavigate()
  const identity = JSON.stringify([
    scope.clusterId,
    scope.namespace,
    resource,
    detail?.name,
    detail?.uid,
  ])
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const liveIdentity = useRef(identity)
  liveIdentity.current = identity
  const [dialog, setDialog] = useState<{
    identity: string
    action: Action
    revision?: RolloutHistory
    target: Target
  } | null>(null)
  const [replicas, setReplicas] = useState<number | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const inFlight = useRef(false)
  const suspend = usePlatformMutation(
    mutations.suspend?.(client) ?? {
      mutationFn: async (_target: Target & { suspend: boolean }) => {
        throw new Error('Suspend unavailable')
      },
    },
    { label: ['调整 CronJob 调度', 'Change CronJob scheduling'], target: platformResourceTarget },
  )
  const restart = usePlatformMutation(
    mutations.restart?.(client) ?? {
      mutationFn: async () => {
        throw new Error('Restart is unavailable for this workload')
      },
    },
    {
      label: [`重启 ${kind}`, `Restart ${kind}`],
      target: platformResourceTarget,
    },
  )
  const scale = usePlatformMutation(
    mutations.scale?.(client) ?? {
      mutationFn: async (_target: Target & { replicas: number }) => {
        throw new Error('Scaling is unavailable for this workload')
      },
    },
    {
      label: [`扩缩容 ${kind}`, `Scale ${kind}`],
      target: platformResourceTarget,
    },
  )
  const rollback = usePlatformMutation(
    mutations.rollback?.(client) ?? {
      mutationFn: async () => {
        throw new Error('Rollback is unavailable for this workload')
      },
    },
    {
      label: [`回滚 ${kind}`, `Roll back ${kind}`],
      target: platformResourceTarget,
    },
  )
  const remove = usePlatformMutation(mutations.remove(client), {
    label: [`删除 ${kind}`, `Delete ${kind}`],
    target: platformResourceTarget,
  })
  const blocked =
    submitting ||
    suspend.blocked ||
    restart.blocked ||
    scale.blocked ||
    rollback.blocked ||
    remove.blocked
  const allowed = (action: Action) =>
    Boolean(
      detail &&
      (action !== 'suspend' ||
        (Boolean(mutations.suspend) && typeof detail.suspend === 'boolean')) &&
      (action !== 'restart' || Boolean(mutations.restart)) &&
      (action !== 'rollback' || Boolean(mutations.rollback)) &&
      (action !== 'scale' || Boolean(mutations.scale)) &&
      (action !== 'delete' || resource !== 'deployments' || detail.uid) &&
      hasPermission(permissions, `${permission}.${action}`) &&
      hasAllowedAction(detail.allowedActions, action),
    )
  const labels = {
    suspend: detail?.suspend ? (zh ? '恢复' : 'Resume') : zh ? '暂停' : 'Suspend',
    scale: zh ? '扩缩容' : 'Scale',
    restart: zh ? '重启' : 'Restart',
    rollback: zh ? '回滚' : 'Roll back',
    delete: zh ? '删除' : 'Delete',
  }
  const selected =
    dialog?.action === 'suspend'
      ? suspend
      : dialog?.action === 'scale'
        ? scale
        : dialog?.action === 'rollback'
          ? rollback
          : dialog?.action === 'delete'
            ? remove
            : restart
  useEffect(() => {
    setDialog(null)
  }, [identity])
  const open = (action: Action, revision?: RolloutHistory) => {
    if (
      !detail ||
      !allowed(action) ||
      blocked ||
      capability.disabled ||
      (action === 'rollback' &&
        (!revision || !currentRevision || revision.revision === currentRevision))
    )
      return
    suspend.reset()
    restart.reset()
    scale.reset()
    rollback.reset()
    remove.reset()
    setReplicas(detail?.desiredReplicas ?? null)
    setDialog({
      identity,
      action,
      revision,
      target: { name: detail.name, scope: { ...scope }, uid: detail.uid },
    })
  }
  const ready = Boolean(
    dialog &&
    dialog.identity === identity &&
    allowed(dialog.action) &&
    !blocked &&
    !capability.disabled &&
    (dialog.action !== 'scale' ||
      (replicas !== null && Number.isInteger(replicas) && replicas >= 0)) &&
    (dialog.action !== 'rollback' ||
      (dialog.revision && currentRevision && dialog.revision.revision !== currentRevision)),
  )
  return {
    canRollback: allowed('rollback'),
    blocked: blocked || capability.disabled || !currentRevision,
    reason: capability.reason,
    openRollback: (record: RolloutHistory) => open('rollback', record),
    buttons: (['suspend', 'scale', 'restart'] as const).filter(allowed).map((action) => (
      <Tooltip key={action} title={capability.disabled ? capability.reason : undefined}>
        <Button
          type={action === 'scale' ? 'primary' : 'default'}
          aria-label={labels[action]}
          disabled={blocked || capability.disabled}
          onClick={() => open(action)}
        >
          {labels[action]}
        </Button>
      </Tooltip>
    )),
    deleteButton: allowed('delete') ? (
      <Tooltip title={capability.disabled ? capability.reason : undefined}>
        <Button
          danger
          aria-label={labels.delete}
          disabled={blocked || capability.disabled}
          onClick={() => open('delete')}
        >
          {labels.delete}
        </Button>
      </Tooltip>
    ) : null,
    feedback: !dialog ? (
      <>
        {suspend.feedback}
        {restart.feedback}
        {scale.feedback}
        {rollback.feedback}
        {remove.feedback}
      </>
    ) : null,
    confirmation: (
      <Modal
        destroyOnHidden
        open={Boolean(dialog && dialog.identity === identity)}
        title={dialog ? `${labels[dialog.action]} ${detail?.name || ''}` : ''}
        okText={dialog ? labels[dialog.action] : undefined}
        cancelText={zh ? '取消' : 'Cancel'}
        confirmLoading={submitting}
        okButtonProps={{
          disabled: !ready,
          danger: dialog?.action === 'delete',
          'aria-label': dialog ? labels[dialog.action] : undefined,
          autoInsertSpace: false,
        }}
        cancelButtonProps={{
          disabled: submitting,
          autoInsertSpace: false,
          'aria-label': zh ? '取消' : 'Cancel',
        }}
        onCancel={() => {
          if (!inFlight.current) setDialog(null)
        }}
        onOk={async () => {
          if (!ready || !dialog || !detail || inFlight.current) return
          const submitted = dialog
          const { target } = submitted
          inFlight.current = true
          setSubmitting(true)
          try {
            if (submitted.action === 'suspend')
              await suspend.mutateAsync({ ...target, suspend: !detail.suspend })
            else if (submitted.action === 'scale')
              await scale.mutateAsync({ ...target, replicas: replicas! })
            else if (submitted.action === 'restart') await restart.mutateAsync(target)
            else if (submitted.action === 'rollback')
              await rollback.mutateAsync({ ...target, revision: submitted.revision!.revision })
            else await remove.mutateAsync(target)
            if (mounted.current && liveIdentity.current === submitted.identity) {
              setDialog(null)
              if (submitted.action === 'delete')
                navigate(
                  `/workloads/${resource}?clusterId=${encodeURIComponent(scope.clusterId || '')}&namespace=${encodeURIComponent(scope.namespace || '')}`,
                )
            }
          } catch {
            /* The mutation keeps the persistent error and uncertain-write fence. */
          } finally {
            inFlight.current = false
            setSubmitting(false)
          }
        }}
      >
        <p>{[scope.clusterId, scope.namespace, detail?.name].filter(Boolean).join(' / ')}</p>
        {dialog?.action === 'scale' && (
          <label>
            {zh ? '期望副本数' : 'Desired replicas'}{' '}
            <InputNumber
              aria-label={zh ? '期望副本数' : 'Desired replicas'}
              min={0}
              precision={0}
              value={replicas}
              disabled={submitting}
              onChange={(value) => setReplicas(value === null ? null : Number(value))}
            />
          </label>
        )}
        {dialog?.action === 'suspend' && (
          <p>
            {zh
              ? '仅改变后续调度，已经启动的 Jobs 会继续执行。恢复后可能触发错过的调度，受调度截止和并发策略约束。'
              : 'Only future scheduling changes. Existing Jobs continue. Resuming may trigger missed schedules subject to deadline and concurrency policy.'}
          </p>
        )}
        {dialog?.action === 'restart' && (
          <p>
            {zh
              ? kind !== 'Deployment'
                ? '更新 Pod 模板中的重启时间，控制器按更新策略处理现有 Pod；OnDelete 策略不会自动替换 Pod。'
                : '更新 Pod 模板并触发滚动重启，实际就绪以集群观察为准。'
              : kind !== 'Deployment'
                ? 'Update the restart timestamp in the Pod template. The configured strategy controls replacement; OnDelete does not automatically replace Pods.'
                : 'Update the Pod template to request a rolling restart. Check observed readiness afterward.'}
          </p>
        )}
        {dialog?.action === 'delete' && kind === 'ReplicaSet' && (
          <p>
            {zh
              ? '若由 Deployment 管理，控制器可能按期望状态重新创建资源。'
              : 'If managed by a Deployment, its controller may recreate resources to satisfy desired state.'}
          </p>
        )}
        {dialog?.action === 'delete' && (
          <Alert
            showIcon
            type="warning"
            title={
              zh
                ? `删除 ${kind} 及其受控副本，此操作不可恢复。${kind === 'StatefulSet' ? 'PVC 是否保留由当前保留策略决定。' : ''}`
                : `Delete the ${kind} and its controlled replicas. This cannot be undone.${kind === 'StatefulSet' ? ' PVC retention follows the configured policy.' : ''}`
            }
          />
        )}
        {dialog?.action === 'rollback' && (
          <>
            <p>
              {zh ? '目标版本' : 'Target revision'} {dialog.revision?.revision}
            </p>
            {dialog.revision?.images?.map((image) => (
              <p key={image}>{image}</p>
            ))}
            <p>
              {zh
                ? '应用旧模板以发起新的滚动更新。'
                : 'Apply the older template to start a new rolling update.'}
            </p>
          </>
        )}
        {!ready && !submitting && !selected.isError && (
          <Alert
            type="warning"
            title={
              capability.reason ||
              (zh
                ? '资源、权限或输入已变化，请核对后再提交。'
                : 'The resource, permission or input changed. Review before submitting.')
            }
          />
        )}
        {selected.feedback}
      </Modal>
    ),
  }
}
