/** @vitest-environment jsdom */

import { DeleteOutlined, EditOutlined } from '@ant-design/icons'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { TableRowActionMenu } from './table-row-action-menu'

vi.mock('@/i18n', () => ({
  localeText: (locale: string, zh: string, en: string) => (locale === 'zh_CN' ? zh : en),
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback = '') => fallback,
  }),
}))

describe('TableRowActionMenu', () => {
  let container: HTMLDivElement
  let root: Root

  beforeAll(() => {
    ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(async () => {
    if (root) {
      await act(async () => {
        root.unmount()
      })
    }
    container?.remove()
    document.body.innerHTML = ''
  })

  it('returns null when only dividers or empty items are provided', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    await act(async () => {
      root.render(<TableRowActionMenu items={[{ type: 'divider' }, null]} targetLabel="node-1" />)
    })

    expect(container.innerHTML).toBe('')
  })

  it('marks decorative menu icons aria-hidden and trims redundant dividers', async () => {
    const onEdit = vi.fn()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    await act(async () => {
      root.render(
        <TableRowActionMenu
          targetLabel="worker-01"
          items={[
            { type: 'divider' },
            { key: 'edit', label: '编辑', icon: <EditOutlined />, onClick: onEdit },
            { type: 'divider' },
            { type: 'divider' },
            { key: 'delete', label: '删除', icon: <DeleteOutlined />, danger: true },
            { type: 'divider' },
          ]}
        />,
      )
    })

    const trigger = container.querySelector<HTMLButtonElement>(
      'button[aria-label="更多操作：worker-01"]',
    )
    expect(trigger).not.toBeNull()

    await act(async () => {
      trigger!.click()
    })

    const dropdown = document.body.querySelector('.ant-dropdown:not(.ant-dropdown-hidden)')
    expect(dropdown).not.toBeNull()
    expect(dropdown!.querySelectorAll('.ant-dropdown-menu-item-divider')).toHaveLength(1)

    const icons = dropdown!.querySelectorAll('.ant-dropdown-menu-item .anticon')
    expect(icons.length).toBeGreaterThanOrEqual(2)
    icons.forEach((icon) => {
      expect(icon.getAttribute('aria-hidden')).toBe('true')
    })

    const editItem = Array.from(dropdown!.querySelectorAll<HTMLElement>('[role="menuitem"]')).find(
      (item) => item.textContent === '编辑',
    )
    expect(editItem).toBeDefined()

    await act(async () => {
      editItem!.click()
    })
    expect(onEdit).toHaveBeenCalledTimes(1)
  })
})
