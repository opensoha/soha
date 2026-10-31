import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapabilityForCluster } from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Modal, Tooltip } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { podMutations } from './mutations'
import type { PodDetail } from './types'

export function usePodDetailDelete(detail: PodDetail | undefined, scope: ScopeKey) {
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
  const identity = JSON.stringify([scope.clusterId, scope.namespace, detail?.name])
  const liveIdentity = useRef<string | null>(identity)
  liveIdentity.current = identity
  const [confirmation, setConfirmation] = useState<string | null>(null)
  useEffect(() => {
    liveIdentity.current = identity
    setConfirmation(null)
    return () => {
      liveIdentity.current = null
    }
  }, [identity])
  const remove = usePlatformMutation(podMutations.remove(client), {
    label: ['删除 Pod', 'Delete Pod'],
    target: platformResourceTarget,
  })
  const allowed = Boolean(
    detail &&
    scope.clusterId &&
    scope.namespace &&
    hasPermission(permissions, 'platform.pods.delete') &&
    hasAllowedAction(detail.allowedActions, 'delete'),
  )
  const capabilityBlocked =
    capability.isLoading ||
    capability.disabled ||
    (capability.mode === 'agent' && capability.status === 'partial')
  const blocked = capabilityBlocked || remove.blocked
  const open = confirmation === identity
  const ready = open && allowed && !blocked
  const label = zh ? '删除' : 'Delete'
  return {
    button: allowed ? (
      <Tooltip title={capabilityBlocked ? capability.reason : undefined}>
        <Button
          danger
          aria-label={label}
          disabled={blocked}
          loading={remove.isPending}
          onClick={() => {
            if (!allowed || blocked) return
            remove.reset()
            setConfirmation(identity)
          }}
        >
          {label}
        </Button>
      </Tooltip>
    ) : null,
    feedback: !open ? remove.feedback : null,
    confirmation: (
      <Modal
        destroyOnHidden
        open={open}
        title={zh ? `确认删除 Pod ${detail?.name || ''}？` : `Delete pod ${detail?.name || ''}?`}
        okText={label}
        cancelText={zh ? '取消' : 'Cancel'}
        confirmLoading={remove.isPending}
        okButtonProps={{
          danger: true,
          disabled: !ready,
          autoInsertSpace: false,
          'aria-label': label,
        }}
        cancelButtonProps={{ disabled: remove.isPending, autoInsertSpace: false }}
        onCancel={() => {
          if (!remove.isPending) setConfirmation(null)
        }}
        onOk={async () => {
          if (!ready || !detail) return
          const target = { name: detail.name, scope: { ...scope } }
          const submittedIdentity = identity
          try {
            await remove.mutateAsync(target)
            if (liveIdentity.current === submittedIdentity) {
              setConfirmation(null)
              navigate(
                `/workloads/pods?clusterId=${encodeURIComponent(target.scope.clusterId || '')}&namespace=${encodeURIComponent(target.scope.namespace || '')}`,
              )
            }
          } catch {
            // The shared mutation owns the persistent error and uncertain-write fence.
          }
        }}
      >
        <p>{[scope.clusterId, scope.namespace, detail?.name].filter(Boolean).join(' / ')}</p>
        <p>
          {zh
            ? '独立 Pod 会被删除；受控制器管理的 Pod 可能会自动创建替代实例。'
            : 'Standalone pods are removed. A controller-managed pod may be replaced automatically.'}
        </p>
        {(!allowed || capabilityBlocked) && (
          <Alert
            showIcon
            type="warning"
            title={
              capability.reason ||
              (zh
                ? '资源或操作权限已变化，请关闭后重新选择。'
                : 'Resource or permission changed. Close and select again.')
            }
          />
        )}
        {remove.feedback}
      </Modal>
    ),
  }
}
