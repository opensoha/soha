import { readableQueryData } from '@/utils/query-feedback'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import type { DeploymentDetail, RolloutHistory, ScopeKey } from '@/types'
import { useQueryClient } from '@tanstack/react-query'
import { Alert, Button, InputNumber, Modal, Tooltip } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { deploymentMutations } from './mutations'

type Action = 'scale' | 'restart' | 'rollback' | 'delete'
export function useDeploymentDetailActions(
  detail: DeploymentDetail | undefined,
  scope: ScopeKey,
  currentRevision?: string,
) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const capability = useClusterCapability('workload.mutations', localeCode)
  const client = useQueryClient()
  const navigate = useNavigate()
  const identity = JSON.stringify([scope.clusterId, scope.namespace, detail?.name])
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
  } | null>(null)
  const [replicas, setReplicas] = useState<number | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const inFlight = useRef(false)
  const restart = usePlatformMutation(deploymentMutations.restart(client), {
    label: ['重启 Deployment', 'Restart Deployment'],
    target: platformResourceTarget,
  })
  const scale = usePlatformMutation(deploymentMutations.scale(client), {
    label: ['扩缩容 Deployment', 'Scale Deployment'],
    target: platformResourceTarget,
  })
  const rollback = usePlatformMutation(deploymentMutations.rollback(client), {
    label: ['回滚 Deployment', 'Roll back Deployment'],
    target: platformResourceTarget,
  })
  const remove = usePlatformMutation(deploymentMutations.remove(client), {
    label: ['删除 Deployment', 'Delete Deployment'],
    target: platformResourceTarget,
  })
  const blocked =
    submitting || restart.blocked || scale.blocked || rollback.blocked || remove.blocked
  const allowed = (action: Action) =>
    Boolean(
      detail &&
      hasPermission(permissions, `platform.deployment.${action}`) &&
      hasAllowedAction(detail.allowedActions, action),
    )
  const labels = {
    scale: zh ? '扩缩容' : 'Scale',
    restart: zh ? '重启' : 'Restart',
    rollback: zh ? '回滚' : 'Roll back',
    delete: zh ? '删除' : 'Delete',
  }
  const selected =
    dialog?.action === 'scale'
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
      !allowed(action) ||
      blocked ||
      capability.disabled ||
      (action === 'rollback' &&
        (!revision || !currentRevision || revision.revision === currentRevision))
    )
      return
    restart.reset()
    scale.reset()
    rollback.reset()
    remove.reset()
    setReplicas(detail?.desiredReplicas ?? null)
    setDialog({ identity, action, revision })
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
    buttons: (['scale', 'restart'] as const).filter(allowed).map((action) => (
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
          const target = { name: detail.name, scope: { ...scope } }
          const submitted = dialog
          inFlight.current = true
          setSubmitting(true)
          try {
            if (submitted.action === 'scale')
              await scale.mutateAsync({ ...target, replicas: replicas! })
            else if (submitted.action === 'restart') await restart.mutateAsync(target)
            else if (submitted.action === 'rollback')
              await rollback.mutateAsync({ ...target, revision: submitted.revision!.revision })
            else await remove.mutateAsync(target)
            if (mounted.current && liveIdentity.current === submitted.identity) {
              setDialog(null)
              if (submitted.action === 'delete')
                navigate(
                  `/workloads/deployments?clusterId=${encodeURIComponent(scope.clusterId || '')}&namespace=${encodeURIComponent(scope.namespace || '')}`,
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
        {dialog?.action === 'restart' && (
          <p>
            {zh
              ? '更新 Pod 模板并触发滚动重启，实际就绪以集群观察为准。'
              : 'Update the Pod template to request a rolling restart. Check observed readiness afterward.'}
          </p>
        )}
        {dialog?.action === 'delete' && (
          <Alert
            showIcon
            type="warning"
            title={
              zh
                ? '删除 Deployment 及其受控副本，此操作不可恢复。'
                : 'Delete the Deployment and its controlled replicas. This cannot be undone.'
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
