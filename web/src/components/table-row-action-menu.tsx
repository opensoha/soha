import { ManagementIconButton } from '@/components/management-list'
import { localeText, useI18n } from '@/i18n'
import { MoreOutlined } from '@ant-design/icons'
import type { MenuProps } from 'antd'
import { Dropdown } from 'antd'
import { cloneElement, isValidElement, type ReactElement } from 'react'

type MenuEntry = NonNullable<NonNullable<MenuProps['items']>[number]>

function hideDecorativeMenuIcon(item: MenuEntry): MenuEntry {
  let next = item
  if ('icon' in next && isValidElement(next.icon)) {
    const iconProps = next.icon.props as { 'aria-hidden'?: boolean | 'true' | 'false' }
    if (iconProps['aria-hidden'] === undefined) {
      next = {
        ...next,
        icon: cloneElement(next.icon as ReactElement<{ 'aria-hidden'?: boolean }>, {
          'aria-hidden': true,
        }),
      }
    }
  }
  if ('children' in next && Array.isArray(next.children)) {
    next = {
      ...next,
      children: next.children.map((child) => (child ? hideDecorativeMenuIcon(child) : child)),
    } as MenuEntry
  }
  return next
}

export interface TableRowActionMenuProps {
  ariaLabel?: string
  disabled?: boolean
  items: MenuProps['items']
  loading?: boolean
  onSelect?: (key: string) => void
  targetLabel?: string
}

export function TableRowActionMenu({
  ariaLabel,
  disabled = false,
  items,
  loading = false,
  onSelect,
  targetLabel,
}: TableRowActionMenuProps) {
  const { localeCode } = useI18n()
  const rawItems = (items ?? []).filter((item): item is MenuEntry => Boolean(item))
  const normalizedItems: MenuEntry[] = []

  for (const item of rawItems) {
    if (item.type === 'divider') {
      if (
        normalizedItems.length === 0 ||
        normalizedItems[normalizedItems.length - 1]?.type === 'divider'
      ) {
        continue
      }
    }
    normalizedItems.push(hideDecorativeMenuIcon(item))
  }

  while (normalizedItems[normalizedItems.length - 1]?.type === 'divider') {
    normalizedItems.pop()
  }

  if (normalizedItems.length === 0) return null

  const defaultLabel = targetLabel
    ? localeText(localeCode, `更多操作：${targetLabel}`, `More actions: ${targetLabel}`)
    : localeText(localeCode, '更多操作', 'More actions')

  return (
    <span className="soha-table-row-action-menu" onClick={(event) => event.stopPropagation()}>
      <Dropdown
        destroyOnHidden
        disabled={disabled}
        placement="bottomRight"
        trigger={['click']}
        menu={{
          items: normalizedItems,
          onClick: onSelect ? ({ key }) => onSelect(String(key)) : undefined,
        }}
      >
        <ManagementIconButton
          aria-haspopup="menu"
          aria-label={ariaLabel ?? defaultLabel}
          className="soha-table-row-action-trigger"
          disabled={disabled}
          icon={<MoreOutlined aria-hidden="true" />}
          loading={loading}
          size="small"
          tooltip={localeText(localeCode, '更多操作', 'More actions')}
        />
      </Dropdown>
    </span>
  )
}
