/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import type { components } from '@opensoha/contracts/gen/ts/sohaapi'
import { AgentActionsCard } from './agent-actions-card'

type Check = components['schemas']['KubernetesAgentActionCheck']

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    dataSource,
    columns,
  }: {
    dataSource: Check[]
    columns: Array<{
      title: string
      dataIndex: keyof Check
      render?: (value: string | undefined) => ReactNode
    }>
  }) => (
    <table>
      <thead>
        <tr>
          {columns.map((column) => (
            <th key={column.dataIndex}>{column.title}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {dataSource.map((check) => (
          <tr key={check.action}>
            {columns.map((column) => (
              <td key={column.dataIndex}>
                {column.render ? column.render(check[column.dataIndex]) : check[column.dataIndex]}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  ),
}))

let root: Root | undefined
beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
afterEach(async () => {
  await act(async () => root?.unmount())
  document.body.innerHTML = ''
})

async function renderCard(checks?: Check[]) {
  const container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  await act(async () => root!.render(<AgentActionsCard checks={checks} />))
  return container
}

describe('AgentActionsCard', () => {
  it('keeps missing diagnostics unverified', async () => {
    const container = await renderCard()
    expect(container.textContent).toContain('当前连接未返回动作诊断，能力尚未核验。')
    expect(container.querySelector('table')).toBeNull()
  })

  it('separates implementation, configured denial and unverified target access', async () => {
    const container = await renderCard([
      {
        action: 'platform.nodes.drain',
        implementation: 'supported',
        configured: 'denied',
        kubernetesRBAC: 'unknown',
        scope: 'cluster',
      },
    ])
    const cells = Array.from(container.querySelectorAll('td'), (cell) => cell.textContent)
    expect(cells).toEqual(['platform.nodes.drain', '已实现', '拒绝', '未核验', '—'])
    expect(container.textContent).toContain('集群范围 RBAC')
  })
})
