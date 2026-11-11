import { operationModalStyles } from '@/components/modal-styles'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useQueryClient } from '@tanstack/react-query'
import { Modal } from 'antd'
import { useState } from 'react'
import { pluginMutations } from '../mutations'
import type { InstalledPlugin } from '../plugin-model'

export function useInstalledPluginActions() {
  const queryClient = useQueryClient()
  const [removing, setRemoving] = useState<InstalledPlugin | null>(null)
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
  const blocked =
    enableMutation.blocked ||
    disableMutation.blocked ||
    removeMutation.blocked ||
    upgradeMutation.blocked

  return {
    loading:
      enableMutation.isPending ||
      disableMutation.isPending ||
      removeMutation.isPending ||
      upgradeMutation.isPending,
    blocked,
    feedback: removing ? null : (
      <>
        {enableMutation.feedback}
        {disableMutation.feedback}
        {removeMutation.feedback}
        {upgradeMutation.feedback}
      </>
    ),
    dialog: (
      <Modal
        open={Boolean(removing)}
        title={removing ? `移除 ${removing.name}` : '移除插件'}
        styles={operationModalStyles}
        style={{ top: 32 }}
        okText="移除"
        cancelText="取消"
        confirmLoading={removeMutation.isPending}
        okButtonProps={{ danger: true, disabled: blocked }}
        cancelButtonProps={{ disabled: blocked }}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: false }}
        onCancel={() => {
          if (!blocked) setRemoving(null)
        }}
        onOk={() => {
          if (!removing || blocked) return
          removeMutation.mutate({ pluginId: removing.id }, { onSuccess: () => setRemoving(null) })
        }}
      >
        {removeMutation.feedback}
        移除后会删除安装记录、manifest 快照和配置的 secret refs，不会删除外部 secret 实体。
      </Modal>
    ),
    enable(pluginId: string) {
      if (!blocked) enableMutation.mutate({ pluginId })
    },
    disable(pluginId: string) {
      if (!blocked) disableMutation.mutate({ pluginId })
    },
    upgrade(pluginId: string) {
      if (!blocked) upgradeMutation.mutate({ pluginId })
    },
    confirmRemove(plugin: InstalledPlugin) {
      if (blocked) return
      removeMutation.reset()
      setRemoving(plugin)
    },
  }
}
