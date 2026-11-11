import { useRef, useState, type ReactNode } from 'react'
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
  const fence = useRef(false)
  const uncertain = useRef(false)
  const [target, setTarget] = useState('')
  const mutation = useMutation({
    ...options,
    onSuccess: async (...args) => {
      uncertain.current = !feedback.readOnly && writeOutcomeUnknown(feedback.resultError?.(args[0]))
      await options.onSuccess?.(...args)
    },
    onError: async (...args) => {
      uncertain.current = !feedback.readOnly && writeOutcomeUnknown(args[0])
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
  const resultError = mutation.isSuccess ? feedback.resultError?.(mutation.data) : undefined
  const outcomeUnknown = !feedback.readOnly && writeOutcomeUnknown(mutation.error ?? resultError)
  const begin = (variables: TVariables) => {
    if (fence.current || uncertain.current) return false
    const submittedTarget = feedback.target(variables)
    fence.current = true
    setTarget(submittedTarget)
    return true
  }
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
                '当前页已暂停再次提交；普通刷新不会确认或解锁原请求。请保留请求 ID 核对审计与资源记录。',
                'Further submissions are paused on this page. Refresh does not confirm or unlock the original request. Keep the request ID to check audit and resource records.',
              )}
            </p>
          </>
        ),
      }
    : receipt

  return {
    ...mutation,
    blocked: mutation.isPending || outcomeUnknown,
    outcomeUnknown,
    mutate: ((variables, callbacks) => {
      if (begin(variables)) mutation.mutate(variables, callbacks)
    }) as typeof mutation.mutate,
    mutateAsync: ((variables, callbacks) => {
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
      return mutation.mutateAsync(variables, callbacks)
    }) as typeof mutation.mutateAsync,
    reset: () => {
      if (!fence.current && !uncertain.current) mutation.reset()
    },
    feedback: (
      <OperationFeedback
        label={label}
        target={target}
        pending={mutation.isPending}
        error={mutation.error ?? resultError}
        readOnly={feedback.readOnly}
        receipt={visibleReceipt}
      />
    ),
  }
}
