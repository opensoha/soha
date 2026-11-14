import type { CSSProperties, MouseEvent, ReactNode } from 'react'
import { Button, Form, Steps } from 'antd'
import type { FormInstance, FormProps } from 'antd'
import { localeText, useI18n } from '@/i18n'
import './step-form.css'

type StepFormFieldNames = Parameters<FormInstance['validateFields']>[0]

export interface StepFormStep {
  children: ReactNode
  fieldNames?: StepFormFieldNames
  title: ReactNode
}

interface StepFormProps extends Omit<FormProps, 'children'> {
  cancelText?: ReactNode
  contentMaxWidth?: number | string
  current: number
  loading?: boolean
  nextText?: ReactNode
  onCancel?: (event: MouseEvent<HTMLElement>) => void
  onCurrentChange: (current: number) => void
  previousText?: ReactNode
  steps: StepFormStep[]
  submitText?: ReactNode
}

export function StepForm({
  cancelText,
  className,
  contentMaxWidth = 640,
  current,
  form,
  layout = 'vertical',
  loading,
  nextText,
  onCancel,
  onCurrentChange,
  previousText,
  steps,
  submitText,
  ...formProps
}: StepFormProps) {
  const { localeCode } = useI18n()
  const resolvedCancelText = cancelText ?? localeText(localeCode, '取消', 'Cancel')
  const resolvedNextText = nextText ?? localeText(localeCode, '下一步', 'Next')
  const resolvedPreviousText = previousText ?? localeText(localeCode, '上一步', 'Previous')
  const resolvedSubmitText = submitText ?? localeText(localeCode, '保存', 'Save')
  const activeStep = steps[current]
  const contentStyle = {
    '--soha-step-form-content-max-width':
      typeof contentMaxWidth === 'number' ? `${contentMaxWidth}px` : contentMaxWidth,
  } as CSSProperties

  const stopFormSubmit = (event: MouseEvent<HTMLElement>) => {
    event.preventDefault()
    event.stopPropagation()
  }

  const goNext = (event: MouseEvent<HTMLElement>) => {
    stopFormSubmit(event)
    const next = Math.min(current + 1, steps.length - 1)
    if (!form) {
      onCurrentChange(next)
      return
    }
    void form
      .validateFields(activeStep?.fieldNames)
      .then(() => onCurrentChange(next))
      .catch(() => undefined)
  }

  const goPrevious = (event: MouseEvent<HTMLElement>) => {
    stopFormSubmit(event)
    onCurrentChange(Math.max(current - 1, 0))
  }

  const submitForm = (event: MouseEvent<HTMLElement>) => {
    if (!form) return
    event.preventDefault()
    form.submit()
  }

  return (
    <Form
      {...formProps}
      className={['soha-step-form', className].filter(Boolean).join(' ')}
      form={form}
      layout={layout}
    >
      <Steps
        className="soha-step-form__steps"
        current={current}
        items={steps.map((step) => ({ title: step.title }))}
        responsive={false}
        size="small"
      />
      <div className="soha-step-form__content" style={contentStyle}>
        {steps.map((step, index) => (
          <div key={index} hidden={index !== current}>
            {step.children}
          </div>
        ))}
      </div>
      <div className="soha-step-form__actions" style={contentStyle}>
        {onCancel ? (
          <Button
            htmlType="button"
            onClick={(event) => {
              stopFormSubmit(event)
              onCancel(event)
            }}
          >
            {resolvedCancelText}
          </Button>
        ) : null}
        {current > 0 ? (
          <Button htmlType="button" onClick={goPrevious}>
            {resolvedPreviousText}
          </Button>
        ) : null}
        {current < steps.length - 1 ? (
          <Button htmlType="button" type="primary" onClick={goNext}>
            {resolvedNextText}
          </Button>
        ) : (
          <Button htmlType="submit" loading={loading} onClick={submitForm} type="primary">
            {resolvedSubmitText}
          </Button>
        )}
      </div>
    </Form>
  )
}
