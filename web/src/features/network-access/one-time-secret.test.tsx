/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { NetworkEnrollmentPane } from './enrollment-pane'
import { NetworkAccessGrantPane } from './access-grant-pane'
const state = vi.hoisted(() => ({
  error: null as Error | null,
  create: vi.fn(),
  values: {
    runtimeId: 'runtime',
    runtimeKind: 'nas',
    deviceId: 'device',
    subjectId: 'subject',
    siteId: 'site',
    networkSpaceId: 'space',
    resourceIdsText: 'resource',
    ttlSeconds: 300,
  },
  form: { resetFields: vi.fn(), setFieldsValue: vi.fn(), validateFields: vi.fn() },
}))
vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useQuery: () => ({
    data: [],
    error: state.error,
    isError: Boolean(state.error),
    isEnabled: true,
    isLoading: false,
    isFetching: false,
  }),
}))
vi.mock('./mutations', () => ({
  networkAccessMutations: {
    enrollments: {
      create: () => ({ mutationKey: ['test-enrollment'], mutationFn: state.create, gcTime: 0 }),
      revoke: () => ({ mutationFn: vi.fn() }),
    },
    accessGrants: {
      create: () => ({ mutationKey: ['test-grant'], mutationFn: state.create, gcTime: 0 }),
      revoke: () => ({ mutationFn: vi.fn() }),
    },
  },
}))
vi.mock('./table-pane', () => ({
  TablePane: ({ createAction }: { createAction: ReactNode }) => <div>{createAction}</div>,
}))
vi.mock('@/components/request-feedback', () => ({ OperationFeedback: () => null }))
vi.mock('antd', () => {
  const Box = ({ children }: { children?: ReactNode }) => <div>{children}</div>
  return {
    Form: Object.assign(Box, { useForm: () => [state.form], Item: Box }),
    Input: Object.assign(Box, { TextArea: Box }),
    InputNumber: Box,
    Select: Box,
    Space: Box,
    Alert: Box,
    Typography: { Text: Box, Paragraph: Box },
    Popover: Box,
    Popconfirm: Box,
    Modal: ({
      open,
      children,
      onOk,
      title,
    }: {
      open: boolean
      children: ReactNode
      onOk: () => void
      title: ReactNode
    }) =>
      open ? (
        <section role="dialog">
          {title}
          {children}
          <button onClick={onOk}>Confirm</button>
        </section>
      ) : null,
    Button: ({
      children,
      onClick,
      disabled,
    }: {
      children: ReactNode
      onClick: () => void
      disabled: boolean
    }) => (
      <button disabled={disabled} onClick={onClick}>
        {children}
      </button>
    ),
  }
})
afterEach(() => {
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  state.error = null
})
for (const kind of ['enrollment', 'grant'] as const) {
  for (const { late, unmount } of [
    { late: false, unmount: false },
    { late: true, unmount: false },
    { late: true, unmount: true },
  ])
    it(`${kind} discards a token after loss of access (late=${late}, unmount=${unmount})`, async () => {
      vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
      state.form.validateFields.mockResolvedValue(state.values)
      const client = new QueryClient()
      const container = document.createElement('div')
      document.body.append(container)
      const root = createRoot(container)
      let mounted = true
      const render = () =>
        act(async () =>
          root.render(
            <QueryClientProvider client={client}>
              {kind === 'enrollment' ? (
                <NetworkEnrollmentPane canCreate canRevoke />
              ) : (
                <NetworkAccessGrantPane canCreate canRevoke />
              )}
            </QueryClientProvider>,
          ),
        )
      let resolve!: (value: unknown) => void
      const result = {
        token: 'one-time-canary',
        enrollment: { runtimeId: 'runtime' },
        grant: { id: 'grant' },
      }
      state.create.mockImplementation(() =>
        late
          ? new Promise((r) => {
              resolve = r
            })
          : Promise.resolve(result),
      )
      try {
        await render()
        await act(async () => {
          container.querySelector('button')!.click()
        })
        await act(async () => {
          container.querySelector<HTMLButtonElement>('section button:last-child')!.click()
        })
        if (!late) expect(container.textContent).toContain('one-time-canary')
        if (unmount) {
          await act(async () => root.unmount())
          mounted = false
        } else {
          state.error = new ApiError(403, 'denied')
          await render()
          expect(container.textContent).not.toContain('one-time-canary')
          state.error = null
          await render()
        }
        if (late)
          await act(async () => {
            resolve(result)
            await new Promise((r) => setTimeout(r, 0))
          })
        expect(container.textContent).not.toContain('one-time-canary')
        expect(container.querySelector('[role="dialog"]')).toBeNull()
        await vi.waitFor(() =>
          expect(
            client
              .getMutationCache()
              .getAll()
              .some((m) => JSON.stringify(m.state.data)?.includes('one-time-canary')),
          ).toBe(false),
        )
      } finally {
        if (mounted) await act(async () => root.unmount())
        client.clear()
        container.remove()
      }
    })
}
