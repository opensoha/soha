import { useCallback, useId, useMemo, useRef, useState, type ReactNode } from 'react'
import { useMutation, type UseMutationOptions } from '@tanstack/react-query'
import { OperationFeedback } from '@/components/request-feedback'
import { localeText, useI18n } from '@/i18n'
import { writeOutcomeUnknown } from '@/utils/query-feedback'

export interface OperationMutationFeedback<TData, TVariables> {
  label: readonly [string, string]
  target: (variables: TVariables) => string
  readOnly?: boolean
  receipt?: (data: TData, variables: TVariables) => { message: string; evidence?: ReactNode }
  resultError?: (data: TData) => Error | undefined
}

// Keep the original mutation keys, invalidation and callbacks with their resource owner.
export function useOperationMutation<TData, TVariables = void, TContext = unknown>(
  options: UseMutationOptions<TData, Error, TVariables, TContext>,
  feedback: OperationMutationFeedback<TData, TVariables>,
) {
  const { localeCode } = useI18n()
  const label = localeText(localeCode, ...feedback.label)
  const feedbackKey = useId()
  const fence = useRef(false)
  const uncertain = useRef<{ variables: TVariables }>()
  const [target, setTarget] = useState('')
  const mutation = useMutation({
    ...options,
    onSuccess: async (...args) => {
      uncertain.current =
        !feedback.readOnly && writeOutcomeUnknown(feedback.resultError?.(args[0]))
          ? { variables: args[1] }
          : undefined
      await options.onSuccess?.(...args)
    },
    onError: async (...args) => {
      uncertain.current =
        !feedback.readOnly && writeOutcomeUnknown(args[0]) ? { variables: args[1] } : undefined
      await options.onError?.(...args)
    },
    onSettled: async (...args) => {
      try {
        await options.onSettled?.(...args)
      } finally {
        fence.current = false
      }
    },
  })
  const resetMutation = mutation.reset
  const reset = useCallback(() => {
    if (!fence.current && !uncertain.current) resetMutation()
  }, [resetMutation])
  const resultError = mutation.isSuccess ? feedback.resultError?.(mutation.data) : undefined
  const outcomeUnknown = !feedback.readOnly && writeOutcomeUnknown(mutation.error ?? resultError)
  const currentTarget = useRef(feedback.target)
  currentTarget.current = feedback.target
  const begin = useCallback((variables: TVariables) => {
    if (fence.current || uncertain.current) return false
    const submittedTarget = currentTarget.current(variables)
    fence.current = true
    setTarget(submittedTarget)
    return true
  }, [])
  const submit = mutation.mutate
  const submitAsync = mutation.mutateAsync
  const mutate = useCallback<typeof mutation.mutate>(
    (variables, callbacks) => {
      if (begin(variables)) submit(variables, callbacks)
    },
    [begin, submit],
  )
  const mutateAsync = useCallback<typeof mutation.mutateAsync>(
    (variables, callbacks) => {
      if (!begin(variables))
        return Promise.reject(
          new Error(
            localeText(
              localeCode,
              '原请求尚待核对，请勿重复提交。',
              'Check the original request before submitting again.',
            ),
          ),
        )
      return submitAsync(variables, callbacks)
    },
    [begin, localeCode, submitAsync],
  )
  const receipt = mutation.isSuccess
    ? (feedback.receipt?.(mutation.data, mutation.variables as TVariables) ?? {
        message: localeText(localeCode, `${label}：请求已返回`, `${label}: request returned`),
        evidence: feedback.readOnly
          ? localeText(localeCode, '预检未执行资源变更。', 'Preflight did not change resources.')
          : localeText(
              localeCode,
              '请核对对应记录，运行状态以实际服务观察结果为准。',
              'Review the corresponding records. Runtime state follows the observed service state.',
            ),
      })
    : undefined
  const visibleReceipt = outcomeUnknown
    ? {
        message: receipt?.message ?? '',
        evidence: (
          <>
            {receipt?.evidence}
            <p>
              {localeText(
                localeCode,
                '关闭提示不会解除此操作的防重复提交保护。',
                'Closing this notice does not release the duplicate-submission protection.',
              )}
            </p>
          </>
        ),
      }
    : receipt

  const renderedFeedback = useMemo(
    () => (
      <OperationFeedback
        notificationKey={`soha-operation-${feedbackKey}`}
        submittedAt={mutation.submittedAt}
        label={label}
        target={target}
        pending={mutation.isPending}
        error={mutation.error ?? resultError}
        readOnly={feedback.readOnly}
        receipt={visibleReceipt}
      />
    ),
    [
      feedbackKey,
      mutation.submittedAt,
      label,
      target,
      mutation.isPending,
      mutation.error,
      resultError,
      feedback.readOnly,
      visibleReceipt,
    ],
  )

  return {
    ...mutation,
    blocked: mutation.isPending || outcomeUnknown,
    outcomeUnknown,
    mutate,
    mutateAsync,
    reset,
    // The operation owner must verify an authoritative receipt before resolving uncertainty.
    resolveUnknown: (variables: TVariables) => {
      if (
        fence.current ||
        !uncertain.current ||
        !outcomeUnknown ||
        uncertain.current.variables !== variables ||
        mutation.variables !== variables
      )
        return
      uncertain.current = undefined
      mutation.reset()
    },
    feedback: renderedFeedback,
  }
}
