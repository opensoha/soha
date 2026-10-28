import { ManagementIconButton } from '@/components/management-list'
import { MoreOutlined } from '@ant-design/icons'
import type { MenuProps } from 'antd'
import { Dropdown } from 'antd'
import { useEffect, useId, useRef, useState } from 'react'

export function PodActionsMenu({
  name,
  localeCode,
  items,
  onSelect,
}: {
  name: string
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
  return (
    <Dropdown
      destroyOnHidden
      placement="bottomRight"
      trigger={['click']}
      open={open}
      onOpenChange={setOpen}
      menu={{
        id: menuId,
        onKeyDownCapture: (event) => {
          if (event.key !== 'Escape' && event.key !== 'Tab') return
          if (event.key === 'Escape') event.preventDefault()
          event.stopPropagation()
          setOpen(false)
          triggerRef.current?.focus()
        },
        'aria-label': localeCode === 'zh_CN' ? `Pod 操作：${name}` : `Pod actions: ${name}`,
        items,
        onClick: ({ key }) => {
          setOpen(false)
          triggerRef.current?.focus()
          onSelect(key)
        },
      }}
    >
      <ManagementIconButton
        ref={triggerRef}
        className="soha-pod-actions-trigger"
        size="small"
        color="default"
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
    </Dropdown>
  )
}
