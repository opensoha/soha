import { TABLE_ACTIONS_COLUMN_CLASS_NAME } from '@/components/resource-actions'
import { requestErrorMessage, useInlineApiError } from '@/components/request-feedback'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { LoadingOutlined } from '@ant-design/icons'
import { Alert, Modal, type MenuProps, type TableColumnsType } from 'antd'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { KubernetesResourceActionsMenu } from './kubernetes-resource-actions-menu'

export function kubernetesDeleteConfirmation(
  target: { name: string; namespace?: string | null },
  localeCode: 'zh_CN' | 'en_US',
  description?: ReactNode,
) {
  return {
    title: localeCode === 'zh_CN' ? `确认删除 ${target.name}？` : `Delete ${target.name}?`,
    description: (
      <>
        <p>
          {description ??
            (localeCode === 'zh_CN'
              ? '此操作不可恢复，删除后集群资源立即消失。'
              : 'This deletes the resource immediately and cannot be undone.')}
        </p>
        <p>{[target.namespace, target.name].filter(Boolean).join(' / ')}</p>
      </>
    ),
  }
}

export type KubernetesResourceAction = {
  key: string
  label: string
  icon?: ReactNode
  disabled?: boolean
  reason?: string
  danger?: boolean
  loading?: boolean
  run: () => unknown | Promise<unknown>
  prepare?: () => void
  feedback?: ReactNode
  confirm?: { title: ReactNode; description?: ReactNode; okText?: string }
}

/** Presentation only: resource owners provide permissions, immutable targets and mutations. */
export function useKubernetesResourceActions<T>({
  resourceKey,
  resourceLabel,
  records,
  getName,
  getKey,
  actions,
  scopeKey,
}: {
  resourceKey: string
  resourceLabel: string
  records: readonly T[]
  getName: (record: T) => string
  /** Include the UID when the resource API uses UID-based confirmation. */
  getKey: (record: T) => string
  actions: (record: T) => KubernetesResourceAction[]
  scopeKey?: string
}) {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const scope = JSON.stringify([resourceKey, scopeKey ?? [clusterId, namespace]])
  const [target, setTarget] = useState<{
    scope: string
    record: T
    key: string
    actionKey: string
    token: number
  } | null>(null)
  const currentTarget = useRef(target)
  currentTarget.current = target
  const currentScope = useRef(scope)
  currentScope.current = scope
  const sequence = useRef(0)
  const submitting = useRef<number | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  useInlineApiError(error)
  useEffect(() => {
    setTarget(null)
    setError(null)
  }, [scope])
  const liveRecord =
    target?.scope === scope ? records.find((record) => getKey(record) === target.key) : undefined
  const liveAction =
    liveRecord === undefined
      ? undefined
      : actions(liveRecord).find((item) => item.key === target?.actionKey)
  const displayAction =
    liveAction ??
    (target ? actions(target.record).find((item) => item.key === target.actionKey) : undefined)
  const ready = Boolean(target?.scope === scope && liveAction && !liveAction.disabled && !pending)
  const selectAction = (record: T, key: string) => {
    const current = records.find((item) => getKey(item) === getKey(record))
    const action =
      current === undefined ? undefined : actions(current).find((item) => item.key === key)
    if (!action || action.disabled) return
    if (action.confirm) {
      action.prepare?.()
      setError(null)
      setTarget({
        scope,
        record: current as T,
        key: getKey(current as T),
        actionKey: key,
        token: ++sequence.current,
      })
    } else {
      // The request owner keeps its existing durable feedback and write fence.
      void Promise.resolve(action.run()).catch(() => undefined)
    }
  }
  const column: TableColumnsType<T>[number] = {
    key: 'actions',
    title: '',
    fixed: 'right',
    width: 64,
    align: 'center',
    className: `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-kubernetes-actions-column`,
    render: (_value, record) => {
      const entries = actions(record)
      const items: NonNullable<MenuProps['items']> = []
      let ordinary = false
      let divided = false
      for (const entry of entries) {
        if (entry.danger && ordinary && !divided) {
          items.push({ key: '__danger-divider', type: 'divider' })
          divided = true
        }
        if (!entry.danger) ordinary = true
        items.push({
          key: entry.key,
          danger: entry.danger,
          disabled: entry.disabled,
          icon: entry.loading ? (
            <LoadingOutlined aria-hidden />
          ) : entry.icon ? (
            <span aria-hidden>{entry.icon}</span>
          ) : undefined,
          label:
            entry.disabled && entry.reason ? (
              <span className="soha-kubernetes-action-menu-label">
                <span>{entry.label}</span>
                <span className="soha-kubernetes-action-menu-reason">{entry.reason}</span>
              </span>
            ) : (
              entry.label
            ),
        })
      }
      return (
        <KubernetesResourceActionsMenu
          resourceLabel={resourceLabel}
          name={getName(record)}
          localeCode={localeCode}
          items={items}
          onSelect={(key) => {
            selectAction(record, key)
          }}
        />
      )
    },
  }
  const confirmation = (
    <Modal
      destroyOnHidden
      open={Boolean(target && target.scope === scope)}
      title={displayAction?.confirm?.title}
      okText={displayAction?.confirm?.okText ?? displayAction?.label}
      cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
      okButtonProps={{
        danger: displayAction?.danger,
        disabled: !ready,
        autoInsertSpace: false,
        'aria-label': displayAction?.confirm?.okText ?? displayAction?.label,
        'aria-busy': pending || displayAction?.loading || undefined,
      }}
      cancelButtonProps={{ disabled: pending }}
      confirmLoading={pending || displayAction?.loading}
      onCancel={() => {
        if (submitting.current === null) {
          setTarget(null)
          setError(null)
        }
      }}
      onOk={async () => {
        if (!ready || !target || !liveAction || submitting.current !== null) return
        const token = target.token
        submitting.current = token
        setPending(true)
        setError(null)
        try {
          await liveAction.run()
          setTarget((current) => (current?.token === token ? null : current))
        } catch (failure) {
          if (currentScope.current === scope && currentTarget.current?.token === token)
            setError(failure instanceof Error ? failure : new Error(String(failure)))
        } finally {
          submitting.current = null
          setPending(false)
        }
      }}
    >
      {displayAction?.confirm?.description}
      {target && !liveAction ? (
        <Alert
          showIcon
          type="warning"
          title={
            localeCode === 'zh_CN'
              ? '资源或操作权限已变化，请关闭后重新选择。'
              : 'The resource or action permission changed. Close and select it again.'
          }
        />
      ) : null}
      {liveAction?.disabled && liveAction.reason ? (
        <Alert showIcon type="warning" title={liveAction.reason} />
      ) : null}
      {displayAction?.feedback}
      {!displayAction?.feedback && error ? (
        <Alert showIcon type="error" title={requestErrorMessage(error, localeCode)} />
      ) : null}
    </Modal>
  )
  return {
    column,
    selectAction,
    confirmation,
    isConfirming: Boolean(target && target.scope === scope),
    hasActions: records.some((record) => actions(record).length > 0),
  }
}
