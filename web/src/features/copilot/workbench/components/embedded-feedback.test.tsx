/** @vitest-environment jsdom */
import { webcrypto } from 'node:crypto'
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { useAuthStore } from '@/stores/auth-store'
import { memoryApi } from '../../memory/api'
import { memoryKeys } from '../../memory/keys'
import type { MemoryRecord } from '../../memory/types'
import { gatewayApi } from '../../gateway/api'
import { workbenchApi } from '../api'
import { workbenchKeys } from '../keys'
import { ChatMemory } from './chat-memory'
import { MessageApproval } from './message-approval'
import { PersonalFeed } from './personal-feed'

vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: () => true,
}))
vi.mock('./markdown', () => ({
  WorkbenchMarkdown: ({ content }: { content: string }) => <p>{content}</p>,
  safeSourceURL: (value: string) => value,
}))

let host: HTMLDivElement
let root: ReturnType<typeof createRoot>
let client: QueryClient
const policy = {
  id: 'personal',
  version: 'v1',
  ownerTypes: ['user'] as ['user'],
  defaultTtl: 864e11,
  maximumTtl: 864e11,
  minimumConfidence: 1,
  explicitWriteOnly: true,
  enabled: true,
}
const record: MemoryRecord = {
  id: 'memory-private',
  ownerType: 'user',
  ownerId: 'owner',
  scopeHash: 'sha256:scope',
  fact: 'private memory fact',
  sourceType: 'explicit_user',
  sourceRefs: [],
  confidence: 1,
  validFrom: '2026-10-17T00:00:00Z',
  expiresAt: '2026-10-18T00:00:00Z',
  policyId: 'personal',
  policyVersion: 'v1',
  status: 'active',
  createdAt: '2026-10-17T00:00:00Z',
}
type Approval = NonNullable<Awaited<ReturnType<typeof gatewayApi.approvals.list>>['data']>[number]
const approval: Approval = {
  id: 'approval-1',
  status: 'pending',
  strategy: 'manual',
  actorType: 'user',
  actorId: 'owner',
  riskLevel: 'mutate',
  requiresApproval: true,
  createdAt: '2026-10-17T00:00:00Z',
  updatedAt: '2026-10-17T00:00:00Z',
  toolName: 'change',
  toolInput: { target: 'original' },
  summary: 'Review',
}
function button(text: string) {
  const result = Array.from(document.body.querySelectorAll<HTMLButtonElement>('button')).find(
    (item) => item.textContent?.replace(/\s/g, '') === text,
  )
  expect(result, `button ${text}`).toBeTruthy()
  return result!
}
async function render(element: ReactNode) {
  await act(async () =>
    root.render(
      <App>
        <QueryClientProvider client={client}>{element}</QueryClientProvider>
      </App>,
    ),
  )
}
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('crypto', webcrypto)
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
    })),
  )
  useAuthStore.setState({
    user: {
      userId: 'owner',
      userName: 'Owner',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    },
  })
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  vi.spyOn(memoryApi.records, 'list').mockResolvedValue({ data: [record] })
  vi.spyOn(memoryApi.policies, 'list').mockResolvedValue({ data: [policy] })
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  host.remove()
  useAuthStore.setState({ user: null })
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('hides denied cached memory and clears its draft and selection', async () => {
  const select = vi.fn()
  await render(
    <ChatMemory
      selected={[record.id]}
      initialFact="private draft"
      onSelect={select}
      onClose={vi.fn()}
    />,
  )
  await vi.waitFor(() => expect(document.body.textContent).toContain(record.fact))
  vi.mocked(memoryApi.records.list).mockRejectedValue(
    new ApiError(403, 'Forbidden', { method: 'GET' }),
  )
  await act(async () => {
    await client.invalidateQueries({ queryKey: memoryKeys.records() })
  })
  await vi.waitFor(() => expect(document.body.textContent).toContain('个人记忆无访问权限'))
  expect(document.body.textContent).not.toContain(record.fact)
  expect(
    document.body.querySelector<HTMLTextAreaElement>('[aria-label="记忆内容"]')?.value,
  ).not.toBe('private draft')
  expect(select).toHaveBeenCalledWith([])
})

it('preserves a memory draft and fences an uncertain save including dialog close', async () => {
  const close = vi.fn()
  const put = vi
    .spyOn(memoryApi.records, 'put')
    .mockRejectedValue(
      new ApiError(503, 'Unknown receipt', { method: 'POST', requestId: 'memory-request' }),
    )
  await render(
    <ChatMemory selected={[]} initialFact="keep this draft" onSelect={vi.fn()} onClose={close} />,
  )
  await vi.waitFor(() => expect(button('确认保存个人记忆').disabled).toBe(false))
  await act(async () => button('确认保存个人记忆').click())
  await vi.waitFor(() => expect(document.body.textContent).toContain('保存个人记忆：请求结果不明'))
  expect(document.body.querySelector<HTMLTextAreaElement>('[aria-label="记忆内容"]')?.value).toBe(
    'keep this draft',
  )
  expect(button('确认保存个人记忆').disabled).toBe(true)
  expect(button('完成').disabled).toBe(true)
  await act(async () => {
    button('确认保存个人记忆').click()
    button('完成').click()
  })
  expect(put).toHaveBeenCalledTimes(1)
  expect(close).not.toHaveBeenCalled()
})

it('keeps the original memory policy version when the directory changes behind a draft', async () => {
  const put = vi.spyOn(memoryApi.records, 'put')
  await render(
    <ChatMemory
      selected={[]}
      initialFact="policy-bound draft"
      onSelect={vi.fn()}
      onClose={vi.fn()}
    />,
  )
  await vi.waitFor(() => expect(button('确认保存个人记忆').disabled).toBe(false))
  vi.mocked(memoryApi.policies.list).mockResolvedValue({ data: [{ ...policy, version: 'v2' }] })
  await act(async () => {
    await client.invalidateQueries({ queryKey: memoryKeys.policies() })
  })
  await vi.waitFor(() => expect(document.body.textContent).toContain('原记忆策略已变化或不可用'))
  expect(button('确认保存个人记忆').disabled).toBe(true)
  expect(put).not.toHaveBeenCalled()
})

it('does not repeat an uncertain approval decision after the pending record is reread', async () => {
  vi.spyOn(gatewayApi.approvals, 'list').mockResolvedValue({ data: [approval] })
  const decide = vi
    .spyOn(gatewayApi.approvals, 'decide')
    .mockRejectedValue(new ApiError(503, 'Unknown decision', { method: 'POST' }))
  await render(<MessageApproval id="approval-1" />)
  await vi.waitFor(() => expect(button('拒绝').disabled).toBe(false))
  await act(async () => button('拒绝').click())
  await vi.waitFor(() => expect(document.body.textContent).toContain('提交审批决定：请求结果不明'))
  expect(button('拒绝').disabled).toBe(true)
  await act(async () => button('拒绝').click())
  expect(decide).toHaveBeenCalledTimes(1)
  expect(decide).toHaveBeenCalledWith('approval-1', 'reject')
})

it('closes the approval confirmation but retains an uncertain original-target decision', async () => {
  vi.spyOn(gatewayApi.approvals, 'list').mockResolvedValue({ data: [approval] })
  const decide = vi
    .spyOn(gatewayApi.approvals, 'decide')
    .mockRejectedValue(new ApiError(503, 'Unknown decision', { method: 'POST' }))
  await render(<MessageApproval id={approval.id} />)
  await vi.waitFor(() => expect(button('批准').disabled).toBe(false))
  await act(async () => button('批准').click())
  expect(document.body.textContent).toContain('批准并执行已保存的变更？')
  expect(document.body.querySelector('.ant-modal-confirm-content')?.textContent).toContain(
    'original',
  )
  expect(decide).not.toHaveBeenCalled()
  await act(async () => button('批准并执行').click())
  await vi.waitFor(() => expect(document.body.textContent).toContain('提交审批决定：请求结果不明'))
  await vi.waitFor(() => expect(document.body.querySelector('.ant-modal-confirm')).toBeNull())
  expect(button('批准').disabled).toBe(true)
  await act(async () => button('批准').click())
  expect(decide).toHaveBeenCalledTimes(1)
  expect(decide).toHaveBeenCalledWith(approval.id, 'approve')
})

it('hides a denied personal feed instead of retaining cached private cards', async () => {
  type Feed = NonNullable<Awaited<ReturnType<typeof workbenchApi.feed.list>>['data']>
  vi.spyOn(workbenchApi.feed, 'list').mockResolvedValue({
    data: {
      items: [
        {
          id: 'feed-1',
          title: 'Private feed',
          body: 'private feed body',
          reason: 'User preference',
          liked: false,
          sources: [],
          createdAt: '2026-10-17T00:00:00Z',
        },
      ],
      state: 'idle',
      settings: { instructions: 'Private preference', searchTool: 'search', autoUpdate: false },
      searchTools: [{ name: 'search', title: 'Search' }],
    } satisfies Feed,
  })
  await render(<PersonalFeed owner="owner" onDiscuss={vi.fn()} />)
  await vi.waitFor(() => expect(document.body.textContent).toContain('private feed body'))
  vi.mocked(workbenchApi.feed.list).mockRejectedValue(
    new ApiError(403, 'Forbidden', { method: 'GET' }),
  )
  await act(async () => {
    await client.invalidateQueries({ queryKey: workbenchKeys.feed('owner') })
  })
  await vi.waitFor(() => expect(document.body.textContent).toContain('个人动态无访问权限'))
  expect(document.body.textContent).not.toContain('private feed body')
})
