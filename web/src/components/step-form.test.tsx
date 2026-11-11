/** @vitest-environment jsdom */

import type { MouseEventHandler, ReactNode } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import type { FormInstance } from 'antd'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { StepForm } from './step-form'

vi.mock('antd', () => ({
  Button: ({
    children,
    htmlType,
    onClick,
  }: {
    children?: ReactNode
    htmlType?: 'button' | 'reset' | 'submit'
    onClick?: MouseEventHandler<HTMLButtonElement>
  }) => (
    <button type={htmlType} onClick={onClick}>
      {children}
    </button>
  ),
  Form: ({ children, onFinish }: { children?: ReactNode; onFinish?: () => void }) => (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        onFinish?.()
      }}
    >
      {children}
    </form>
  ),
  Steps: () => null,
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

describe('StepForm', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
  })

  it('submits an external Ant Design form exactly once', async () => {
    const submit = vi.fn()
    const onFinish = vi.fn()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    await act(async () => {
      root.render(
        <StepForm
          current={0}
          form={{ submit } as unknown as FormInstance<Record<string, unknown>>}
          onCurrentChange={() => undefined}
          onFinish={onFinish}
          steps={[{ title: 'Details', children: <div>Details</div> }]}
          submitText="Submit"
        />,
      )
    })

    await act(async () => {
      container.querySelector('button')?.click()
    })

    expect(submit).toHaveBeenCalledTimes(1)
    expect(onFinish).not.toHaveBeenCalled()
  })
})
