import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { useQueryClient } from '@tanstack/react-query'
import { Alert, App, Form, Input, Modal, Select, Space } from 'antd'
import { useRef, useState } from 'react'
import { crdMutations } from './mutations'
import type { CRD } from './types'

export function DeleteCRDDefinitionModal({
  clusterId,
  definitions,
  onClose,
}: {
  clusterId: string
  definitions: CRD[]
  onClose: () => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { message } = App.useApp()
  const currentScope = usePlatformScopeStore()
  const snapshot = usePermissionSnapshot().data?.data
  const mutation = usePlatformMutation(crdMutations.removeDefinition(useQueryClient()), {
    label: ['删除 CRD 定义', 'Delete CRD definition'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = mutation.blocked
  const [selected, setSelected] = useState<CRD | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const submitting = useRef(false)
  const current = definitions.find((item) => item.name === selected?.name)
  const permitted = hasPermission(snapshot, 'platform.extensions.crds.delete')
  const unchanged = Boolean(selected?.uid && current?.uid === selected.uid)
  const allowed = Boolean(
    permitted &&
    currentScope.clusterId === clusterId &&
    unchanged &&
    current?.allowedActions?.includes('delete') &&
    !current.deletingAt,
  )
  const canDelete = allowed && confirmation === selected?.name && !platformWriteBlocked
  const submit = async () => {
    if (submitting.current || !canDelete || !selected?.uid) return
    submitting.current = true
    try {
      await mutation.mutateAsync({ clusterId, name: selected.name, expectedUid: selected.uid })
      void message.success(
        zh ? '已请求删除 CRD 及其所有实例' : 'CRD and instance deletion requested',
      )
      onClose()
    } catch {
      submitting.current = false
      // The mutation error stays visible so the same target can be reviewed and retried.
    }
  }

  return (
    <Modal
      open
      title={zh ? '删除 CRD 定义' : 'Delete CRD definition'}
      width={560}
      onCancel={() => {
        if (!platformWriteBlocked) onClose?.()
      }}
      onOk={() => void submit()}
      okText={zh ? '删除定义及实例' : 'Delete definition and instances'}
      cancelText={zh ? '取消' : 'Cancel'}
      confirmLoading={mutation.isPending}
      okButtonProps={{ danger: true, disabled: platformWriteBlocked || !canDelete }}
      cancelButtonProps={{ disabled: platformWriteBlocked }}
      closable={!platformWriteBlocked}
      keyboard={!platformWriteBlocked}
      mask={{ closable: !platformWriteBlocked }}
    >
      {mutation.feedback}

      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <Alert
          type="warning"
          showIcon
          title={zh ? '此操作会删除该 CRD 的所有实例' : 'All instances of this CRD will be deleted'}
          description={
            zh
              ? '包括所有命名空间中的实例，无法撤销。此操作不会卸载 Operator；仍在运行的控制器可能重新创建 CRD。Finalizer 可能延迟删除完成。'
              : 'Includes instances in every namespace and cannot be undone. This does not uninstall the operator; active controllers may recreate the CRD. Finalizers can delay completion.'
          }
        />
        <Form layout="vertical" style={{ width: '100%' }}>
          <Form.Item
            label={zh ? '选择具体 CRD 定义' : 'Choose one CRD definition'}
            htmlFor="crd-definition-name"
          >
            <Select
              id="crd-definition-name"
              value={selected?.name}
              disabled={platformWriteBlocked}
              placeholder={zh ? '选择需要清理的 CRD' : 'Select the CRD to remove'}
              showSearch={{ optionFilterProp: 'label' }}
              options={definitions.map((item) => ({
                value: item.name,
                label: `${item.name} · ${item.kind}`,
                disabled:
                  !item.uid || Boolean(item.deletingAt) || !item.allowedActions?.includes('delete'),
              }))}
              onChange={(name) => {
                setSelected(definitions.find((item) => item.name === name) ?? null)
                setConfirmation('')
                mutation.reset()
              }}
            />
          </Form.Item>
          <Form.Item
            label={zh ? '输入完整 CRD 名称确认' : 'Type the full CRD name to confirm'}
            htmlFor="crd-definition-confirmation"
            extra={selected?.name}
          >
            <Input
              id="crd-definition-confirmation"
              value={confirmation}
              disabled={!selected || platformWriteBlocked}
              autoComplete="off"
              onChange={(event) => setConfirmation(event.target.value)}
            />
          </Form.Item>
        </Form>
        {selected && !allowed ? (
          <Alert
            type="warning"
            showIcon
            title={
              zh
                ? '定义或权限已变化，请刷新目录后重新选择'
                : 'Definition or permission changed. Refresh the catalog and select again.'
            }
          />
        ) : null}
      </Space>
    </Modal>
  )
}
