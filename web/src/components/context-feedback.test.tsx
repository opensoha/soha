/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ContextFeedback } from './context-feedback'
import { ManagementState } from './management-list'

let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.unstubAllGlobals()
})

it('keeps risk details and their action in the owning region', async () => {
  const inspect = vi.fn()
  await act(async () =>
    root.render(
      <ContextFeedback
        type="warning"
        title="原请求仍待核对"
        description="关闭此提示不会取消任务或解除提交保护。"
        action={<button onClick={inspect}>查看原记录</button>}
      />,
    ),
  )
  expect(container.querySelector('[role="alert"].soha-context-feedback')).not.toBeNull()
  expect(container.textContent).toContain('关闭此提示不会取消任务或解除提交保护。')
  await act(async () => container.querySelector('button')!.click())
  expect(inspect).toHaveBeenCalledTimes(1)
  expect(document.querySelector('.ant-notification')).toBeNull()
})

it('preserves an explicitly closable explanation and its close callback', async () => {
  const onClose = vi.fn()
  await act(async () => root.render(<ContextFeedback title="范围已截断" closable={{ onClose }} />))
  await act(async () =>
    container.querySelector<HTMLButtonElement>('.ant-alert-close-icon')!.click(),
  )
  expect(onClose).toHaveBeenCalledTimes(1)
})

it.each(['error', 'no-permission'] as const)(
  'uses the same compact region for ManagementState %s',
  async (kind) => {
    await act(async () =>
      root.render(
        <ManagementState kind={kind} title="当前数据不可用" description="保留相关上下文。" />,
      ),
    )
    expect(
      container.querySelector('.soha-management-state.is-context .soha-context-feedback'),
    ).not.toBeNull()
    expect(container.textContent).toContain('保留相关上下文。')
  },
)
