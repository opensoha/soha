import { ContextFeedback } from '@/components/context-feedback'
import { operationModalStyles } from '@/components/modal-styles'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { ApiError } from '@/services/api-error'
import { useQueryClient } from '@tanstack/react-query'
import { Checkbox, Descriptions, Modal, Space } from 'antd'
import { useState } from 'react'
import { pluginKeys } from '../keys'
import { pluginMutations } from '../mutations'
import type { InstalledPlugin } from '../plugin-model'

export function useInstalledPluginActions(canRollback: boolean, current?: InstalledPlugin) {
  const queryClient = useQueryClient()
  const [removing, setRemoving] = useState<InstalledPlugin | null>(null)
  const [rollingBack, setRollingBack] = useState<InstalledPlugin | null>(null)
  const [rollbackAcknowledged, setRollbackAcknowledged] = useState(false)
  const enableMutation = useOperationMutation(pluginMutations.enable(queryClient), {
    label: ['启用插件', 'Enable plugin'],
    target: (v) => v.pluginId,
  })
  const disableMutation = useOperationMutation(pluginMutations.disable(queryClient), {
    label: ['停用插件', 'Disable plugin'],
    target: (v) => v.pluginId,
  })
  const removeMutation = useOperationMutation(pluginMutations.remove(queryClient), {
    label: ['移除插件', 'Remove plugin'],
    target: (v) => v.pluginId,
    receipt: () => ({ message: '插件安装记录已移除', evidence: '外部 Secret 实体没有被删除。' }),
  })
  const upgradeMutation = useOperationMutation(pluginMutations.upgrade(queryClient), {
    label: ['升级插件', 'Upgrade plugin'],
    target: (v) => v.pluginId,
  })
  const rollbackMutation = useOperationMutation(pluginMutations.repositoryRollback(queryClient), {
    label: ['回退插件', 'Roll back plugin'],
    target: (v) => v.pluginId,
    receipt: (item) => ({ message: `插件已回退至 ${item.version}` }),
  })
  const blocked =
    enableMutation.blocked ||
    disableMutation.blocked ||
    removeMutation.blocked ||
    upgradeMutation.blocked ||
    rollbackMutation.blocked
  const rollbackVersion = rollingBack?.rollbackVersion
  const rollbackNeedsConfirmation = rollingBack?.rollbackRequiresPermissionConfirmation
  const references = current?.referenceCount ?? 0
  const rollbackStale = Boolean(
    rollingBack &&
    (current?.id !== rollingBack.id ||
      current.version !== rollingBack.version ||
      current.updatedAt !== rollingBack.updatedAt ||
      current.rollbackVersion !== rollbackVersion ||
      current.rollbackRequiresPermissionConfirmation !== rollbackNeedsConfirmation),
  )
  const rollbackBlocked =
    blocked || !canRollback || rollbackStale || (rollbackNeedsConfirmation && !rollbackAcknowledged)

  function refreshOnConflict(error: Error, id: string) {
    if (error instanceof ApiError && error.status === 409) {
      void queryClient.invalidateQueries({ queryKey: pluginKeys.installedDetail(id) })
    }
  }

  return {
    loading:
      enableMutation.isPending ||
      disableMutation.isPending ||
      removeMutation.isPending ||
      upgradeMutation.isPending ||
      rollbackMutation.isPending,
    blocked,
    feedback: removing ? null : (
      <>
        {enableMutation.feedback}
        {disableMutation.feedback}
        {removeMutation.feedback}
        {upgradeMutation.feedback}
        {rollingBack ? null : rollbackMutation.feedback}
      </>
    ),
    dialog: (
      <>
        <Modal
          open={Boolean(removing)}
          title={removing ? `移除 ${removing.name}` : '移除插件'}
          styles={operationModalStyles}
          style={{ top: 32 }}
          okText="移除"
          cancelText="取消"
          confirmLoading={removeMutation.isPending}
          okButtonProps={{ danger: true, disabled: blocked || references > 0 }}
          cancelButtonProps={{ disabled: blocked }}
          closable={!blocked}
          keyboard={!blocked}
          mask={{ closable: false }}
          onCancel={() => {
            if (!blocked) setRemoving(null)
          }}
          onOk={() => {
            if (!removing || blocked || references > 0) return
            removeMutation.mutate(
              { pluginId: removing.id },
              {
                onSuccess: () => setRemoving(null),
                onError: (error) => refreshOnConflict(error, removing.id),
              },
            )
          }}
        >
          {removeMutation.feedback}
          {references > 0 ? (
            <ContextFeedback
              type="warning"
              showIcon
              title={`仍有 ${references} 个存储连接引用此驱动，无法移除。`}
            />
          ) : null}
          移除后会删除安装记录、manifest 快照和配置的 secret refs，不会删除外部 secret 实体。
        </Modal>
        <Modal
          open={Boolean(rollingBack)}
          title={rollingBack ? `回退 ${rollingBack.name}` : '回退插件'}
          styles={operationModalStyles}
          style={{ top: 32 }}
          okText="确认回退"
          cancelText="取消"
          confirmLoading={rollbackMutation.isPending}
          okButtonProps={{ disabled: rollbackBlocked }}
          cancelButtonProps={{ disabled: rollbackMutation.isPending, autoFocus: true }}
          closable={!rollbackMutation.isPending}
          keyboard={!rollbackMutation.isPending}
          mask={{ closable: false }}
          onCancel={() => {
            if (!rollbackMutation.isPending) setRollingBack(null)
          }}
          onOk={() => {
            if (!rollingBack || !rollbackVersion || rollbackBlocked) return
            rollbackMutation.mutate(
              {
                pluginId: rollingBack.id,
                input: {
                  expectedVersion: rollingBack.version,
                  expectedUpdatedAt: rollingBack.updatedAt,
                  version: rollbackVersion,
                  ...(rollbackNeedsConfirmation ? { confirmPermissionIncrease: true } : {}),
                },
              },
              {
                onSuccess: () => setRollingBack(null),
                onError: (error) => refreshOnConflict(error, rollingBack.id),
              },
            )
          }}
        >
          {rollingBack ? (
            <Space orientation="vertical" size={12} className="soha-plugin-repository-dialog">
              <Descriptions
                column={1}
                size="small"
                items={[
                  { key: 'current', label: '当前版本', children: rollingBack.version },
                  { key: 'target', label: '回退版本', children: rollbackVersion },
                  ...(current?.referenceCount !== undefined
                    ? [{ key: 'references', label: '受影响存储连接', children: references }]
                    : []),
                ]}
              />
              <ContextFeedback
                showIcon
                type="info"
                title="使用本机保留的已验签版本，无需访问 GitHub 或市场。"
              />
              {rollbackStale ? (
                <ContextFeedback
                  showIcon
                  type="warning"
                  title="安装记录已变化，请关闭此窗口并重新查看可回退版本。"
                />
              ) : null}
              {rollbackNeedsConfirmation ? (
                <Checkbox
                  checked={rollbackAcknowledged}
                  disabled={blocked || rollbackStale || !canRollback}
                  onChange={(event) => setRollbackAcknowledged(event.target.checked)}
                >
                  我已知悉回退将新增权限要求，并同意继续回退
                </Checkbox>
              ) : null}
              {rollbackMutation.feedback}
            </Space>
          ) : null}
        </Modal>
      </>
    ),
    enable(pluginId: string) {
      if (!blocked) enableMutation.mutate({ pluginId })
    },
    disable(pluginId: string) {
      if (!blocked && !references) {
        disableMutation.mutate(
          { pluginId },
          { onError: (error) => refreshOnConflict(error, pluginId) },
        )
      }
    },
    upgrade(pluginId: string) {
      if (!blocked) upgradeMutation.mutate({ pluginId })
    },
    confirmRemove(plugin: InstalledPlugin) {
      if (blocked || references > 0) return
      removeMutation.reset()
      setRemoving(plugin)
    },
    confirmRollback(plugin: InstalledPlugin) {
      if (blocked || !canRollback || !plugin.rollbackVersion) return
      rollbackMutation.reset()
      setRollbackAcknowledged(false)
      setRollingBack(plugin)
    },
  }
}
