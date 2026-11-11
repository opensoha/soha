import { localeText, useI18n } from '@/i18n'
import { ApiError, claimApiErrorPresentation } from '@/services/api-error'
import {
  latestSubmittedOperation,
  readableQueryData,
  writeOutcomeUnknown,
} from '@/utils/query-feedback'
import { useDocumentHidden } from '@/utils/use-document-hidden'
import { Alert, Button, Space, Typography, theme } from 'antd'
import { useEffect, type ReactNode } from 'react'
import './request-feedback.css'

interface ReadState<T> {
  data?: T
  error: Error | null
  isError: boolean
  isFetching: boolean
  refetch?: () => unknown
}

export interface OperationFeedbackProps {
  pending?: boolean
  error?: Error | null
  label: string
  target?: string
  receipt?: { message: string; evidence?: ReactNode }
  readOnly?: boolean
}

export function useInlineApiError(error?: Error | null) {
  // Antd flushes queued notices in passive effects; claim after that flush.
  useEffect(() => claimApiErrorPresentation(error), [error])
}

export function requestErrorMessage(
  error: Error | null | undefined,
  localeCode: 'zh_CN' | 'en_US',
) {
  const requestId = error instanceof ApiError ? error.requestId : undefined
  return [
    error?.message,
    requestId
      ? localeText(localeCode, `请求 ID: ${requestId}`, `Request ID: ${requestId}`)
      : undefined,
  ]
    .filter(Boolean)
    .join(' · ')
}

export function ReadFeedback<T>({
  query,
  label,
  canRead = true,
}: {
  query: ReadState<T>
  label: string
  canRead?: boolean
}) {
  const { localeCode } = useI18n()
  useInlineApiError(canRead && query.isError ? query.error : null)
  if (!canRead || !query.isError) return null
  const authExpired = query.error instanceof ApiError && query.error.status === 401
  const denied = query.error instanceof ApiError && [401, 403].includes(query.error.status)
  const cached = readableQueryData(query) !== undefined
  return (
    <Alert
      className="soha-read-feedback"
      showIcon
      type={denied || !cached ? 'error' : 'warning'}
      title={localeText(
        localeCode,
        denied
          ? authExpired
            ? `${label}登录状态失效`
            : `${label}无访问权限`
          : cached
            ? `${label}未更新，正在展示上次读取结果`
            : `${label}读取失败`,
        denied
          ? authExpired
            ? `${label}: sign-in expired`
            : `${label}: access denied`
          : cached
            ? `${label}: showing the previous result`
            : `${label}: read failed`,
      )}
      description={requestErrorMessage(query.error, localeCode)}
      action={
        denied || !query.refetch ? undefined : (
          <Button
            aria-label={localeText(localeCode, `重试读取${label}`, `Retry reading ${label}`)}
            size="small"
            loading={query.isFetching}
            onClick={() => void query.refetch?.()}
          >
            {localeText(localeCode, '重试读取', 'Retry reading')}
          </Button>
        )
      }
    />
  )
}

export function OperationFeedback({
  pending,
  error,
  label,
  target,
  receipt,
  readOnly = false,
}: OperationFeedbackProps) {
  const { localeCode } = useI18n()
  const { token } = theme.useToken()
  const hidden = useDocumentHidden()
  useInlineApiError(error)
  if (!pending && !error && !receipt) return null
  const uncertain = !readOnly && writeOutcomeUnknown(error)
  const title = pending
    ? localeText(localeCode, `${label}：正在提交请求`, `${label}: submitting request`)
    : error
      ? localeText(
          localeCode,
          uncertain ? `${label}：请求结果不明` : `${label}：操作请求失败`,
          uncertain ? `${label}: request outcome unknown` : `${label}: request failed`,
        )
      : receipt?.message
  return (
    <Alert
      className="soha-operation-feedback"
      data-motion-paused={hidden}
      data-motion-disabled={!token.motion}
      type={error ? (uncertain ? 'warning' : 'error') : 'info'}
      showIcon
      title={
        <span role="status" aria-live="polite">
          {pending ? <span className="soha-feedback-live-dot" aria-hidden /> : null}
          {title}
        </span>
      }
      description={
        <Space orientation="vertical" size={4}>
          {target ? <Typography.Text>{target}</Typography.Text> : null}
          {error ? (
            <Typography.Text>{requestErrorMessage(error, localeCode)}</Typography.Text>
          ) : null}
          {uncertain ? (
            <Typography.Text>
              {localeText(
                localeCode,
                '请先核对任务与资源记录，不要重复提交相同操作。',
                'Check tasks and resource records before submitting this operation again.',
              )}
            </Typography.Text>
          ) : null}
          {receipt?.evidence}
        </Space>
      }
    />
  )
}

export function MutationFeedback({
  operations,
}: {
  operations: (Omit<OperationFeedbackProps, 'pending' | 'error'> & {
    mutation: { isPending: boolean; isSuccess: boolean; error: Error | null; submittedAt: number }
  })[]
}) {
  const latest = latestSubmittedOperation(operations)
  return latest ? (
    <OperationFeedback
      {...latest}
      pending={latest.mutation.isPending}
      error={latest.mutation.error}
      receipt={latest.mutation.isSuccess ? latest.receipt : undefined}
    />
  ) : null
}
