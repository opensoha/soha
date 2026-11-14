import type { FormInstance } from 'antd'
import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { StepFormModal } from './step-form-modal'

const lifecycle = vi.hoisted(() => ({
  cancel: undefined as (() => void) | undefined,
  closable: undefined as boolean | undefined,
  keyboard: undefined as boolean | undefined,
  bodyClassName: undefined as string | undefined,
  bodyStyle: undefined as Record<string, unknown> | undefined,
  centered: undefined as boolean | undefined,
  contentMaxWidth: undefined as number | string | undefined,
  destroyOnHidden: undefined as boolean | undefined,
  disabled: undefined as boolean | undefined,
  modalStyle: undefined as Record<string, unknown> | undefined,
  preserve: undefined as boolean | undefined,
  title: undefined as ReactNode,
  titleStyle: undefined as Record<string, unknown> | undefined,
}))

vi.mock('antd', () => ({
  Modal: ({
    children,
    closable,
    keyboard,
    onCancel,
    centered,
    classNames,
    destroyOnHidden,
    style,
    styles,
    title,
  }: {
    closable?: boolean
    keyboard?: boolean
    onCancel?: () => void
    children?: ReactNode
    centered?: boolean
    classNames?: { body?: string }
    destroyOnHidden?: boolean
    style?: Record<string, unknown>
    styles?: { body?: Record<string, unknown>; title?: Record<string, unknown> }
    title?: ReactNode
  }) => {
    lifecycle.cancel = onCancel
    lifecycle.closable = closable
    lifecycle.keyboard = keyboard
    lifecycle.bodyClassName = classNames?.body
    lifecycle.bodyStyle = styles?.body
    lifecycle.centered = centered
    lifecycle.destroyOnHidden = destroyOnHidden
    lifecycle.modalStyle = style
    lifecycle.title = title
    lifecycle.titleStyle = styles?.title
    return <>{children}</>
  },
}))

vi.mock('./step-form', () => ({
  StepForm: ({
    contentMaxWidth,
    disabled,
    preserve,
  }: {
    contentMaxWidth?: number | string
    disabled?: boolean
    preserve?: boolean
  }) => {
    lifecycle.contentMaxWidth = contentMaxWidth
    lifecycle.disabled = disabled
    lifecycle.preserve = preserve
    return null
  },
}))

describe('StepFormModal lifecycle', () => {
  beforeEach(() => {
    lifecycle.bodyClassName = undefined
    lifecycle.bodyStyle = undefined
    lifecycle.centered = undefined
    lifecycle.contentMaxWidth = undefined
    lifecycle.destroyOnHidden = undefined
    lifecycle.disabled = undefined
    lifecycle.modalStyle = undefined
    lifecycle.preserve = undefined
    lifecycle.title = undefined
    lifecycle.titleStyle = undefined
  })

  it('discards the external form store after closing', () => {
    renderToStaticMarkup(
      <StepFormModal
        contentMaxWidth={800}
        current={0}
        disabled
        form={{} as FormInstance<Record<string, unknown>>}
        onClose={() => undefined}
        onCurrentChange={() => undefined}
        onFinish={() => undefined}
        open
        steps={[]}
        title="Test form"
      />,
    )

    expect(lifecycle.contentMaxWidth).toBe(800)
    expect(lifecycle.destroyOnHidden).toBe(true)
    expect(lifecycle.disabled).toBe(true)
    expect(lifecycle.preserve).toBe(false)
    expect(lifecycle.bodyClassName).toBe('soha-step-form-modal__body')
    expect(lifecycle.bodyStyle).toMatchObject({
      maxHeight: 'calc(100dvh - 160px)',
      overflowY: 'auto',
      overscrollBehavior: 'contain',
    })
    expect(lifecycle.centered).not.toBe(true)
    expect(lifecycle.modalStyle).toMatchObject({ top: 32, marginTop: 0 })
    expect(lifecycle.title).toBe('Test form')
    expect(lifecycle.titleStyle).toBeUndefined()
  })

  it('blocks every modal close path only when explicitly requested', () => {
    const onClose = vi.fn()
    renderToStaticMarkup(
      <StepFormModal closeDisabled onClose={onClose} open title="Pending">
        <span>Review</span>
      </StepFormModal>,
    )
    expect(lifecycle.closable).toBe(false)
    expect(lifecycle.keyboard).toBe(false)
    lifecycle.cancel?.()
    expect(onClose).not.toHaveBeenCalled()
    renderToStaticMarkup(
      <StepFormModal onClose={onClose} open title="Read only">
        <span>View</span>
      </StepFormModal>,
    )
    expect(lifecycle.closable).toBe(true)
    expect(lifecycle.keyboard).toBe(true)
    lifecycle.cancel?.()
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('hosts an existing StepForm without rendering a second form', () => {
    const html = renderToStaticMarkup(
      <StepFormModal
        feedback={<span>request failure persists here</span>}
        bodyClassName="feature-step-form"
        onClose={() => undefined}
        open
        title="Composed form"
      >
        <span>existing step form</span>
      </StepFormModal>,
    )

    expect(html).toContain('existing step form')
    expect(html).toContain('request failure persists here')
    expect(lifecycle.contentMaxWidth).toBeUndefined()
    expect(lifecycle.bodyClassName).toBe('soha-step-form-modal__body feature-step-form')
    expect(lifecycle.title).toBe('Composed form')
  })
})
