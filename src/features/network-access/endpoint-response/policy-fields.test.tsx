/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { Form } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { networkAccessQueries } from '../queries'
import { EndpointResponsePolicyFields } from './policy-fields'

// Render option labels directly: this suite checks authorization of cached data.
vi.mock('antd', async (original) => ({
  ...(await original<typeof import('antd')>()),
  Select: ({ options }: { options?: { value?: string | number; label?: ReactNode }[] }) => (
    <span>
      {options?.map((option) => (
        <span key={option.value}>{option.label}</span>
      ))}
    </span>
  ),
}))
let root: Root | undefined
beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
})
afterEach(async () => {
  await act(async () => root?.unmount())
  root = undefined
  document.body.innerHTML = ''
})
async function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity, retry: false } },
  })
  const keys = [
    networkAccessQueries.sites({ limit: 200 }).queryKey,
    networkAccessQueries.nasBindings({ limit: 200 }).queryKey,
    networkAccessQueries.devices({ limit: 200 }).queryKey,
  ]
  const names = ['Private site', 'Private NAS', 'Private device']
  const common = {
    status: 'active' as const,
    createdAt: '2026-10-23T00:00:00Z',
    updatedAt: '2026-10-23T00:00:00Z',
  }
  client.setQueryData(networkAccessQueries.sites({ limit: 200 }).queryKey, [
    { ...common, id: 'site-1', name: names[0] },
  ])
  client.setQueryData(networkAccessQueries.nasBindings({ limit: 200 }).queryKey, [
    {
      ...common,
      id: 'nas-1',
      name: names[1],
      nasId: 'nas-1',
      runtimeId: 'radius-1',
      siteId: 'site-1',
      coaSupported: true,
      disconnectSupported: true,
    },
  ])
  client.setQueryData(networkAccessQueries.devices({ limit: 200 }).queryKey, [
    {
      ...common,
      id: 'device-1',
      name: names[2],
      ownerUserId: 'user-1',
      platform: 'macos',
      postureStatus: 'unknown',
      postureVersion: 1,
    },
  ])
  const node = document.createElement('div')
  document.body.appendChild(node)
  root = createRoot(node)
  const render = async (allowed: boolean) => {
    await act(async () =>
      root!.render(
        <QueryClientProvider client={client}>
          <Form>
            <EndpointResponsePolicyFields can={() => allowed} />
          </Form>
        </QueryClientProvider>,
      ),
    )
  }
  await render(true)
  names.forEach((name) => expect(node.textContent).toContain(name))
  return { client, keys, names, node, render }
}
describe('response policy scope options', () => {
  it('drops all cached names when current permissions are revoked', async () => {
    const { names, node, render } = await setup()
    await render(false)
    names.forEach((name) => expect(node.textContent).not.toContain(name))
  })
  it.each([401, 403, 503])('handles a cached scope read failing with %i', async (status) => {
    const { client, keys, names, node } = await setup()
    await act(async () => {
      keys.forEach((key) =>
        client
          .getQueryCache()
          .find({ queryKey: key })!
          .setState({
            status: 'error',
            error: new ApiError(status, 'Scope read failed', {
              method: 'GET',
              path: '/network-access/sites',
              requestId: 'scope-read',
            }),
          }),
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    names.forEach((name) => {
      if (status === 503) expect(node.textContent).toContain(name)
      else expect(node.textContent).not.toContain(name)
    })
  })
})
