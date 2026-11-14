import { ManagementIconButton } from '@/components/management-list'
import { DownOutlined, MoreOutlined } from '@ant-design/icons'
import type { MenuProps } from 'antd'
import { Button, Dropdown } from 'antd'
import { useEffect, useId, useRef, useState } from 'react'
import '../platform-shared.css'

export function KubernetesResourceActionsMenu({
  name,
  resourceLabel,
  triggerLabel,
  loading,
  disabled,
  localeCode,
  items,
  onSelect,
}: {
  name: string
  resourceLabel: string
  triggerLabel?: string
  loading?: boolean
  disabled?: boolean
  localeCode: 'zh_CN' | 'en_US'
  items: MenuProps['items']
  onSelect: (key: string) => void
}) {
  const [open, setOpen] = useState(false)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuId = useId()
  useEffect(() => {
    if (!open) return
    // Cancel pending menu focus when a quick Escape closes it before the next frame.
    const frame = requestAnimationFrame(() => {
      const menu = document.getElementById(menuId)
      if (document.activeElement !== menu && menu?.contains(document.activeElement)) return
      const firstItem = menu?.querySelector<HTMLElement>(
        '[role="menuitem"]:not([aria-disabled="true"])',
      )
      const focusTarget = firstItem ?? menu
      focusTarget?.focus()
    })
    return () => cancelAnimationFrame(frame)
  }, [open, menuId])
  useEffect(() => {
    if (!items?.length) setOpen(false)
  }, [items?.length])
  if (!items?.length) return null
  // Keep the menu as one intrinsic control; the table's icon-group measurement
  // must not replace the resource column's explicit 64px interaction budget.
  return (
    <span className="soha-kubernetes-action-menu" onClick={(event) => event.stopPropagation()}>
      <Dropdown
        disabled={disabled}
        destroyOnHidden
        placement="bottomRight"
        trigger={['click']}
        open={open}
        onOpenChange={setOpen}
        menu={{
          id: menuId,
          onKeyDownCapture: (event) => {
            if (
              event.key === 'Enter' &&
              event.target instanceof Element &&
              event.target.closest('a[href]')
            ) {
              // Let the native link click close the menu without selecting twice.
              event.stopPropagation()
              return
            }
            if (event.key !== 'Escape' && event.key !== 'Tab') return
            if (event.key === 'Escape') event.preventDefault()
            event.stopPropagation()
            setOpen(false)
            triggerRef.current?.focus()
          },
          'aria-label':
            localeCode === 'zh_CN'
              ? `${resourceLabel} 操作：${name}`
              : `${resourceLabel} actions: ${name}`,
          items,
          onClick: ({ key, domEvent }) => {
            // Enter must not activate the trigger after focus returns to it.
            if ('key' in domEvent && domEvent.key === 'Enter') domEvent.preventDefault()
            setOpen(false)
            triggerRef.current?.focus()
            onSelect(key)
          },
        }}
      >
        {triggerLabel ? (
          <Button
            ref={triggerRef}
            autoInsertSpace={false}
            size="small"
            disabled={disabled}
            loading={loading}
            aria-label={triggerLabel}
            aria-busy={loading || undefined}
            aria-haspopup="menu"
            aria-expanded={open}
            onKeyDown={(event) => {
              if (event.key === 'ArrowDown') {
                event.preventDefault()
                setOpen(true)
              }
            }}
          >
            {triggerLabel} <DownOutlined aria-hidden />
          </Button>
        ) : (
          <ManagementIconButton
            ref={triggerRef}
            className="soha-kubernetes-actions-trigger"
            size="small"
            color="default"
            disabled={disabled}
            aria-label={localeCode === 'zh_CN' ? `更多操作：${name}` : `More actions: ${name}`}
            aria-haspopup="menu"
            aria-expanded={open}
            tooltip={localeCode === 'zh_CN' ? '更多操作' : 'More actions'}
            icon={<MoreOutlined aria-hidden />}
            onKeyDown={(event) => {
              if (event.key === 'ArrowDown') {
                event.preventDefault()
                setOpen(true)
              }
            }}
          />
        )}
      </Dropdown>
    </span>
  )
}
