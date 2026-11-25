/** @vitest-environment jsdom */
import { ApiError } from '@/services/api-error'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { beforeEach, expect, it, vi } from 'vitest'
import type {
  AssistantMeshDiscoveredAssistant,
  AssistantMeshInquiry,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AssistantReception } from './assistant-reception'

const requests = vi.hoisted(() => ({
  getEnvelope: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))
vi.mock('@/services/api-client', async () => ({
  ApiError: (await import('@/services/api-error')).ApiError,
  api: requests,
}))

const profile = { open: false, away: false, intro: '可以问平台问题', retentionDays: 30 }
const assistant: AssistantMeshDiscoveredAssistant = {
  ownerId: 'colleague',
  name: '同事甲',
  intro: '平台值班',
  away: true,
  topics: ['发布排障', '容量'],
}

function inquiry(overrides: Partial<AssistantMeshInquiry> = {}): AssistantMeshInquiry {
  return {
    id: 'inquiry-1',
    callerId: 'owner',
    ownerId: 'colleague',
    callerName: '我',
    ownerName: '同事甲',
    question: '发布失败怎么查？',
    status: 'pending',
    createdAt: '2026-11-21T08:00:00Z',
    dueAt: '2026-11-22T08:00:00Z',
    ...overrides,
  }
}

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
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
})

async function mount() {
  const container = document.body.appendChild(document.createElement('div'))
  const root = createRoot(container)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <App>
        <QueryClientProvider client={client}>
          <AssistantReception owner="owner" />
        </QueryClientProvider>
      </App>,
    ),
  )
  return async () => {
    await act(async () => root.unmount())
    client.clear()
    container.remove()
    vi.resetAllMocks()
  }
}

function click(text: string) {
  return act(async () => {
    const target = Array.from(document.querySelectorAll<HTMLElement>('button,[role="tab"]')).find(
      (node) =>
        `${node.textContent ?? ''}${node.getAttribute('aria-label') ?? ''}`
          .replace(/\s/g, '')
          .includes(text.replace(/\s/g, '')),
    )
    expect(target, `click ${text}`).toBeDefined()
    target?.click()
  })
}

function typeInto(selector: string, value: string) {
  return act(async () => {
    const input = document.querySelector<HTMLTextAreaElement>(selector)
    expect(input, selector).toBeTruthy()
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set?.call(input, value)
    input!.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

it('previews plainly, then sends once confirmed with a stable retry request id', async () => {
  let sendCalls = 0
  requests.getEnvelope.mockImplementation(async (path: string) => {
    if (path.endsWith('/profile')) return { data: profile }
    if (path.includes('direction=sent')) return { items: sendCalls >= 2 ? [inquiry()] : [] }
    throw new Error(`unexpected GET ${path}`)
  })
  requests.post.mockImplementation(async (path: string, body: { question?: string }) => {
    if (path.endsWith('/assistants/search')) return { data: [assistant] }
    if (path.endsWith('/inquiries/preview')) {
      return { data: { question: body.question, blocked: false } }
    }
    if (path === '/copilot/assistant-mesh/inquiries') {
      sendCalls += 1
      if (sendCalls === 1) throw new ApiError(502, 'response lost', { method: 'POST', path })
      return {
        data: inquiry({ question: body.question, status: 'pending' }),
      }
    }
    throw new Error(`unexpected POST ${path}`)
  })

  const unmount = await mount()
  try {
    await vi.waitFor(() => expect(document.querySelector('[aria-label="助手问询"]')).toBeTruthy())
    await click('助手问询')
    await vi.waitFor(() => expect(document.body.textContent).toContain('同事甲'))
    await click('同事甲')

    const editor = 'textarea[aria-label="问题内容"]'
    await vi.waitFor(() => expect(document.querySelector(editor)).toBeTruthy())
    await typeInto(editor, '发布失败怎么查？')
    await click('预览发送内容')
    await vi.waitFor(() =>
      expect(requests.post).toHaveBeenCalledWith('/copilot/assistant-mesh/inquiries/preview', {
        question: '发布失败怎么查？',
      }),
    )
    expect(sendCalls).toBe(0)

    await click('确认发送')
    await vi.waitFor(() => expect(sendCalls).toBe(1))
    await vi.waitFor(() => expect(document.body.textContent).toContain('重试原问询'))
    expect(document.body.textContent).not.toContain('关闭提示')
    await act(async () => {
      document.querySelector<HTMLButtonElement>('.ant-drawer-close')?.click()
    })
    await click('助手问询')
    await click('重试原问询')
    await vi.waitFor(() => expect(sendCalls).toBe(2))

    const sends = requests.post.mock.calls.filter(
      ([path]) => path === '/copilot/assistant-mesh/inquiries',
    )
    const first = sends[0][1] as { requestId: string; ownerId: string; question: string }
    const second = sends[1][1] as { requestId: string; ownerId: string; question: string }
    expect(first).toMatchObject({
      ownerId: 'colleague',
      question: '发布失败怎么查？',
    })
    expect(first.requestId).toBeTruthy()
    expect(second.requestId).toBe(first.requestId)
    await vi.waitFor(() => expect(document.body.textContent).toContain('已转交本人'))
  } finally {
    await unmount()
  }
})

it('shows only the current owner inbox and replies privately to pending inquiries', async () => {
  let replied = false
  requests.getEnvelope.mockImplementation(async (path: string) => {
    if (path.endsWith('/profile')) return { data: profile }
    if (path.includes('direction=inbox')) {
      return {
        items: [
          inquiry({
            ownerId: 'owner',
            callerId: 'asker',
            callerName: '提问同事',
            status: replied ? 'replied' : 'pending',
            answer: replied ? '先看发布记录。' : undefined,
          }),
          inquiry({
            id: 'other',
            ownerId: 'someone-else',
            question: '别人的问题',
            callerName: '他人',
          }),
          inquiry({
            id: 'answered',
            question: '已答复的问题',
            status: 'answered',
            answer: '历史答复',
          }),
        ],
      }
    }
    if (path.includes('direction=sent')) return { items: [] }
    throw new Error(`unexpected GET ${path}`)
  })
  requests.post.mockImplementation(async (path: string) => {
    if (path.endsWith('/assistants/search')) return { data: [] }
    if (path === '/copilot/assistant-mesh/inquiries/inquiry-1/reply') {
      replied = true
      return {
        data: inquiry({
          status: 'replied',
          answer: '先看发布记录。',
          closedAt: '2026-11-21T09:00:00Z',
        }),
      }
    }
    throw new Error(`unexpected POST ${path}`)
  })

  const unmount = await mount()
  try {
    await vi.waitFor(() => expect(document.querySelector('[aria-label="助手问询"]')).toBeTruthy())
    await click('助手问询')
    await click('收件箱')
    await vi.waitFor(() => expect(document.body.textContent).toContain('发布失败怎么查？'))

    expect(document.body.textContent).not.toContain('别人的问题')
    expect(document.body.textContent).not.toContain('发布共享')
    const replyButtons = Array.from(document.querySelectorAll('button')).filter(
      (button) => button.textContent?.trim() === '回复该同事',
    )
    expect(replyButtons).toHaveLength(1)

    await click('回复该同事')
    const reply = 'textarea[aria-label="仅回复该同事的内容"]'
    await vi.waitFor(() => expect(document.querySelector(reply)).toBeTruthy())
    await typeInto(reply, '先看发布记录。')
    await click('仅回复该同事')
    await vi.waitFor(() =>
      expect(requests.post).toHaveBeenCalledWith(
        '/copilot/assistant-mesh/inquiries/inquiry-1/reply',
        { answer: '先看发布记录。' },
      ),
    )
    await vi.waitFor(() => expect(document.body.textContent).toContain('同事已回复'))
  } finally {
    await unmount()
  }
})

it('reads the same Core delegation inbox and hides unpublished preview in Web', async () => {
  const path = '/copilot/assistant-mesh/delegations'
  let declined = false
  requests.getEnvelope.mockImplementation(async (url: string) => {
    if (url.endsWith('/profile')) return { data: profile }
    if (url === path + '?direction=inbox') return {
      items: [{
        id: 'delegate-1', callerId: 'colleague', callerName: '同事甲', ownerId: 'owner',
        ownerName: '我', capability: 'desktop.os-version.v1', field: 'buildVersion',
        status: declined ? 'declined' : 'awaiting_confirmation',
        result: 'PRIVATE_PREVIEW_MARKER', createdAt: '2026-11-21T08:00:00Z', expiresAt: '2026-11-21T12:00:00Z',
      }],
    }
    throw new Error(`unexpected GET ${url}`)
  })
  requests.post.mockImplementation(async (url: string) => {
    if (url === path + '/delegate-1/decline') {
      declined = true
      return { data: {} }
    }
    throw new Error(`unexpected POST ${url}`)
  })
  const unmount = await mount()
  try {
    await vi.waitFor(() => expect(document.querySelector('[aria-label="助手问询"]')).toBeTruthy())
    await click('助手问询')
    await click('本机委托')
    await vi.waitFor(() => expect(document.body.textContent).toContain('等待回传确认'))
    expect(document.body.textContent).not.toContain('PRIVATE_PREVIEW_MARKER')
    await click('拒绝委托')
    await vi.waitFor(() => expect(document.body.textContent).toContain('已拒绝'))
    expect(requests.post).toHaveBeenCalledWith(path + '/delegate-1/decline', {})
  } finally {
    await unmount()
  }
})
