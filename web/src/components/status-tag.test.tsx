/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { act } from 'react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { MetadataTag, StatusTag } from './status-tag'

const captured = vi.hoisted(() => ({
  props: null as Record<string, unknown> | null,
}))

vi.mock('antd', () => ({
  Tag: ({ children, ...props }: { children?: ReactNode }) => {
    captured.props = props
    return <span>{children}</span>
  },
}))

describe('StatusTag', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  })

  afterEach(() => {
    captured.props = null
    document.body.replaceChildren()
  })

  it('uses the shared compact filled tag treatment', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<StatusTag value="supported" />)
    })

    expect(captured.props).toMatchObject({
      className: 'soha-status-tag',
      color: 'success',
      variant: 'filled',
    })

    await act(async () => root.unmount())
  })

  it('keeps a localized label while resolving color from the status value', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<StatusTag label="阻塞" value=" blocked " />)
    })

    expect(container.textContent).toBe('阻塞')
    expect(captured.props).toMatchObject({
      className: 'soha-status-tag',
      color: 'error',
      variant: 'filled',
    })

    await act(async () => root.unmount())
  })

  it('covers infrastructure provisioning and agent failure states', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<StatusTag value="provisioning" />)
    })
    expect(captured.props?.color).toBe('processing')

    await act(async () => {
      root.render(<StatusTag value="agent_failed" />)
    })
    expect(captured.props?.color).toBe('error')

    await act(async () => root.unmount())
  })

  it('localizes canonical status values when no explicit label is supplied', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<StatusTag value="running" />)
    })

    expect(container.textContent).toBe('运行中')
    await act(async () => root.unmount())
  })

  it('uses the same filled treatment for categorical metadata', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<MetadataTag label="构建" tone="purple" />)
    })

    expect(container.textContent).toBe('构建')
    expect(captured.props).toMatchObject({
      className: 'soha-metadata-tag',
      color: 'purple',
      variant: 'filled',
    })

    await act(async () => root.unmount())
  })
})
