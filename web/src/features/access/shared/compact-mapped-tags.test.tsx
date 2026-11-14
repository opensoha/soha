/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import { renderCompactMappedTags } from './compact-mapped-tags'

describe('renderCompactMappedTags', () => {
  beforeAll(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
    globalThis.ResizeObserver = class ResizeObserver {
      disconnect() {}
      observe() {}
      unobserve() {}
    }
  })

  afterEach(() => {
    document.body.replaceChildren()
  })

  it('keeps long tag collections to a single compact summary', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        renderCompactMappedTags(
          ['view', 'list', 'watch', 'create', 'update'],
          {},
          '未配置',
          2,
          '动作',
        ),
      )
    })

    expect(container.textContent).toContain('view')
    expect(container.textContent).toContain('list')
    expect(container.textContent).toContain('+3')
    expect(container.querySelector('[aria-label="查看 5 个动作"]')).not.toBeNull()
    expect(container.textContent).not.toContain('watch')
    expect(container.querySelector('.soha-metadata-tag.ant-tag-processing')).not.toBeNull()
    expect(container.querySelector('.soha-access-compact-tag-more.ant-tag-default')).not.toBeNull()

    await act(async () => root.unmount())
  })

  it('keeps role and organization colors in both summaries and expanded collections', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    for (const [tone, label] of [
      ['purple', '角色'],
      ['cyan', '组织'],
    ] as const) {
      await act(async () => {
        root.render(renderCompactMappedTags(['a', 'b'], { a: '显示名称' }, '-', 1, label, tone))
      })
      expect(container.querySelector(`.soha-metadata-tag.ant-tag-${tone}`)?.textContent).toBe(
        '显示名称',
      )
      await act(async () => {
        container.querySelector<HTMLButtonElement>('button')?.click()
      })
      expect(
        document.querySelectorAll(`.soha-access-permission-popover-tag.ant-tag-${tone}`),
      ).toHaveLength(2)
      await act(async () => {
        root.render(null)
      })
    }
    await act(async () => root.unmount())
  })

  it('closes an expanded collection when focus moves to another action', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <div>
          {renderCompactMappedTags(['view', 'create'], {}, '未配置', 1, '动作')}
          <button type="button">编辑</button>
        </div>,
      )
    })

    const trigger = container.querySelector<HTMLButtonElement>('[aria-label="查看 2 个动作"]')
    expect(trigger).not.toBeNull()
    await act(async () => {
      trigger?.click()
    })
    expect(document.body.textContent).toContain('2 个动作')

    const editButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === '编辑',
    )
    await act(async () => {
      editButton?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      editButton?.click()
    })
    expect(document.body.textContent).not.toContain('2 个动作')

    await act(async () => root.unmount())
  })
})
