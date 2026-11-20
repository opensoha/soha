/** @vitest-environment jsdom */
import { ApiError } from '@/services/api-error'
import type { AssistantMeshAsset } from '@opensoha/contracts/gen/ts/sohaapi'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { AssistantMesh } from './assistant-mesh'

const requests = vi.hoisted(() => ({ getEnvelope: vi.fn(), post: vi.fn() }))
vi.mock('@/services/api-client', async () => ({
  ApiError: (await import('@/services/api-error')).ApiError,
  api: requests,
}))

async function mount() {
  const container = document.body.appendChild(document.createElement('div'))
  const root = createRoot(container)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <App>
        <QueryClientProvider client={client}>
          <AssistantMesh owner="author" sessions={[{ id: 's1', title: 'Heya' }]} sessionId="s1" />
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

function click(selector: string, label?: string) {
  return act(async () => {
    const target = Array.from(document.querySelectorAll<HTMLElement>(selector)).find(
      (node) => !label || node.textContent?.trim() === label,
    )
    expect(target).toBeDefined()
    target?.click()
  })
}

it('hides the sharing entry when Core disables the feature', async () => {
  requests.getEnvelope.mockRejectedValue(new ApiError(403, 'disabled', { method: 'GET' }))
  const unmount = await mount()
  try {
    await vi.waitFor(() => expect(requests.getEnvelope).toHaveBeenCalled())
    expect(document.querySelector('[aria-label="能力共享"]')).toBeNull()
  } finally {
    await unmount()
  }
})

it('runs the published digest even when the owner has a newer draft', async () => {
  const input = { kind: 'skill', title: 'Run pinned asset', body: 'Known good' } as const
  const asset: AssistantMeshAsset = {
    id: 'asset-1',
    kind: 'skill',
    ownerUserId: 'author',
    draft: { ...input, body: 'New draft' },
    draftDigest: 'draft-digest',
    published: {
      input,
      digest: 'published-digest',
      version: 1,
      audience: { visibility: 'private' },
    },
  }
  requests.getEnvelope.mockImplementation(async (path: string) =>
    path.endsWith('/asset-1') ? { data: asset } : { items: [asset] },
  )
  requests.post.mockResolvedValue({ data: { asset, runId: 'agent:skill' } })
  const unmount = await mount()
  try {
    await vi.waitFor(() => expect(document.querySelector('[aria-label="能力共享"]')).toBeTruthy())
    await click('[aria-label="能力共享"]')
    await vi.waitFor(() => expect(document.querySelector('.ant-card-head-title')).toBeTruthy())
    await click('.ant-card-hoverable')
    const placeholder = 'textarea[placeholder="试跑提示词"]'
    await vi.waitFor(() => expect(document.querySelector(placeholder)).toBeTruthy())
    await act(async () => {
      const textarea = document.querySelector<HTMLTextAreaElement>(placeholder)!
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set?.call(
        textarea,
        'Check status',
      )
      textarea.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await click('button', '运行已发布版本')
    await vi.waitFor(() =>
      expect(requests.post).toHaveBeenCalledWith(
        '/copilot/assistant-mesh/assets/asset-1/actions/run',
        expect.objectContaining({ expectedDigest: 'published-digest', sessionId: 's1' }),
      ),
    )
  } finally {
    await unmount()
  }
})
