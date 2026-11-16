import { localeText, useI18n } from '@/i18n'
import { ApiError, claimApiErrorPresentation } from '@/services/api-error'
import {
  latestSubmittedOperation,
  readableQueryData,
  writeOutcomeUnknown,
} from '@/utils/query-feedback'
import { useDocumentHidden } from '@/utils/use-document-hidden'
import { App, Button, Space, Typography, theme } from 'antd'
import { useCallback, useEffect, useId, useRef, type ReactNode } from 'react'
import { ExclamationCircleOutlined } from '@ant-design/icons'
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
  actions?: ReactNode
  description?: ReactNode
  readOnly?: boolean
  notificationKey?: string
  submittedAt?: number
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

export interface ReadRefreshTarget {
  label: string
  refetch: () => unknown
}

// Only explicit user refreshes notify; query errors themselves stay in their region.
export function useReadRefreshFeedback() {
  const { localeCode } = useI18n()
  const { notification } = App.useApp()
  const id = useId()
  const key = `soha-read-refresh-${id}`
  const refreshing = useRef(false)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      notification.destroy?.(key)
    }
  }, [key, notification])

  return useCallback(
    async (targets: readonly ReadRefreshTarget[]) => {
      if (refreshing.current || !targets.length) return
      refreshing.current = true
      try {
        const results = await Promise.all(
          targets.map(async ({ label, refetch }) => {
            try {
              const response = await refetch()
              return (Array.isArray(response) ? response : [response])
                .filter(
                  (item) => item && typeof item === 'object' && 'isError' in item && item.isError,
                )
                .map((result) => ({
                  label,
                  error:
                    result.error instanceof Error
                      ? result.error
                      : new Error(
                          localeText(
                            localeCode,
                            '读取失败，请稍后重试。',
                            'Reading failed. Try again later.',
                          ),
                        ),
                }))
            } catch (cause) {
              return [{ label, error: cause instanceof Error ? cause : new Error(String(cause)) }]
            }
          }),
        )
        if (!mounted.current) return
        const failures = results.flat()
        for (const { error } of failures) {
          // Cancel the transport fallback; the query owns its persistent regional state.
          const release = claimApiErrorPresentation(error)
          release?.()
        }
        const visible = failures.filter(
          ({ error }) => !(error instanceof ApiError && [401, 403].includes(error.status)),
        )
        if (!visible.length) {
          notification.destroy?.(key)
          return
        }
        notification.warning?.({
          key,
          title: localeText(localeCode, '刷新未完成', 'Refresh incomplete'),
          description: (
            <Space orientation="vertical" size={4}>
              <Typography.Text>
                {localeText(
                  localeCode,
                  `${[...new Set(visible.map((item) => item.label))].join('、')}未更新，可在对应区域重试。`,
                  `${[...new Set(visible.map((item) => item.label))].join(', ')} did not update. Retry in the corresponding section.`,
                )}
              </Typography.Text>
              <details className="soha-operation-feedback-details">
                <summary>{localeText(localeCode, '查看详情', 'View details')}</summary>
                {visible.map(({ label, error }, index) => (
                  <div key={index}>
                    {label}: {requestErrorMessage(error, localeCode)}
                  </div>
                ))}
              </details>
            </Space>
          ),
          style: { width: 'min(384px, calc(100vw - 48px))', background: 'var(--soha-bg-surface)' },
          duration: 0,
        })
      } finally {
        refreshing.current = false
      }
    },
    [key, localeCode, notification],
  )
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
  const refresh = useReadRefreshFeedback()
  useInlineApiError(canRead && query.isError ? query.error : null)
  if (!canRead || !query.isError) return null
  const authExpired = query.error instanceof ApiError && query.error.status === 401
  const denied = query.error instanceof ApiError && [401, 403].includes(query.error.status)
  const cached = readableQueryData(query) !== undefined
  return (
    <section
      className={`soha-read-feedback${cached ? ' is-stale' : ''}`}
      role={cached && !denied ? 'status' : 'alert'}
      aria-label={localeText(localeCode, `${label}读取状态`, `${label} reading status`)}
      aria-busy={query.isFetching}
    >
      <ExclamationCircleOutlined className="soha-read-feedback-icon" aria-hidden="true" />
      <span className="soha-read-feedback-title">
        {localeText(
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
      </span>
      {!denied && query.refetch ? (
        <Button
          aria-label={localeText(localeCode, `重试读取${label}`, `Retry reading ${label}`)}
          size="small"
          type="text"
          loading={query.isFetching}
          onClick={() => void refresh([{ label, refetch: query.refetch! }])}
        >
          {localeText(localeCode, '重试', 'Retry')}
        </Button>
      ) : null}
      <details className="soha-read-feedback-details">
        <summary>{localeText(localeCode, '详情', 'Details')}</summary>
        <div>{requestErrorMessage(query.error, localeCode)}</div>
      </details>
    </section>
  )
}

const notificationOwners = new Map<
  string,
  {
    owners: number
    notice?: { signature: string; showAt: number; expiresAt?: number; dismissed: boolean }
  }
>()

export function OperationFeedback({
  pending,
  error,
  label,
  target,
  receipt,
  actions,
  description,
  readOnly = false,
  notificationKey,
  submittedAt,
}: OperationFeedbackProps) {
  const { localeCode } = useI18n()
  const { notification } = App.useApp()
  const { token } = theme.useToken()
  const hidden = useDocumentHidden()
  const id = useId()
  const key = notificationKey ?? `soha-operation-${id}`
  useInlineApiError(error)
  const uncertain = !readOnly && writeOutcomeUnknown(error)
  const refreshUnavailable = error instanceof ApiError && error.code === 'auth_refresh_unavailable'
  const active = Boolean(pending || error || receipt)
  const signature = JSON.stringify([
    submittedAt,
    label,
    target,
    pending ? 'pending' : error ? 'error' : receipt?.message,
    uncertain,
    refreshUnavailable,
    error?.message,
    error instanceof ApiError ? error.requestId : undefined,
  ])

  // A hook's feedback can appear in its page and active dialog. They share one notice.
  useEffect(() => {
    const owner = notificationOwners.get(key) ?? { owners: 0 }
    owner.owners += 1
    notificationOwners.set(key, owner)
    return () => {
      owner.owners -= 1
      if (!owner.owners) {
        notificationOwners.delete(key)
        notification.destroy(key)
      }
    }
  }, [key, notification])

  useEffect(() => {
    const owner = notificationOwners.get(key)
    if (!owner) return
    if (!active) {
      owner.notice = undefined
      notification.destroy(key)
      return
    }
    const now = Date.now()
    if (owner.notice?.signature !== signature) {
      owner.notice = {
        signature,
        showAt: now + (pending ? 400 : 0),
        expiresAt: !pending && !error ? now + (receipt?.evidence ? 8_000 : 3_000) : undefined,
        dismissed: false,
      }
    }
    if (owner.notice.dismissed) return
    const show = () => {
      const current = owner.notice
      if (!current || current.signature !== signature || current.dismissed) return
      const duration = current.expiresAt ? (current.expiresAt - Date.now()) / 1_000 : 0
      if (current.expiresAt && duration <= 0) {
        current.dismissed = true
        notification.destroy(key)
        return
      }
      notification.open({
        key,
        className: 'soha-operation-feedback',
        style: { width: 'min(384px, calc(100vw - 48px))', background: 'var(--soha-bg-surface)' },
        placement: 'topRight',
        role: error ? 'alert' : 'status',
        type: error
          ? uncertain || refreshUnavailable
            ? 'warning'
            : 'error'
          : pending
            ? 'info'
            : 'success',
        duration,
        icon: pending ? (
          <span
            className="soha-feedback-live-dot"
            data-motion-paused={hidden}
            data-motion-disabled={!token.motion}
            aria-hidden
          />
        ) : undefined,
        title: pending
          ? localeText(localeCode, `${label}：正在提交请求`, `${label}: submitting request`)
          : error
            ? localeText(
                localeCode,
                uncertain ? `${label}：请求结果不明` : `${label}：操作请求失败`,
                uncertain ? `${label}: request outcome unknown` : `${label}: request failed`,
              )
            : receipt?.message,
        description: (
          <Space orientation="vertical" size={4}>
            {target ? <Typography.Text>{target}</Typography.Text> : null}
            {description}
            {error ? (
              <Typography.Text>
                {error instanceof ApiError && error.kind === 'network'
                  ? localeText(
                      localeCode,
                      '连接中断，未能获取服务响应。',
                      'The connection was interrupted before a response was received.',
                    )
                  : error.message}
              </Typography.Text>
            ) : null}
            {error instanceof ApiError && (error.requestId || error.kind === 'network') ? (
              <details className="soha-operation-feedback-details">
                <summary>{localeText(localeCode, '查看详情', 'View details')}</summary>
                <Typography.Text type="secondary">
                  {error.kind === 'network'
                    ? requestErrorMessage(error, localeCode)
                    : localeText(
                        localeCode,
                        `请求 ID: ${error.requestId}`,
                        `Request ID: ${error.requestId}`,
                      )}
                </Typography.Text>
              </details>
            ) : null}
            {uncertain ? (
              <Typography.Text>
                {localeText(
                  localeCode,
                  '请先核对操作记录，不要重复提交相同操作。',
                  'Check the operation records before submitting this operation again.',
                )}
              </Typography.Text>
            ) : null}
            {receipt?.evidence}
            {actions}
          </Space>
        ),
        // Closing the notice changes presentation only; the mutation owns its submission fence.
        onClose: () => {
          if (owner.notice?.signature === signature) owner.notice.dismissed = true
        },
      })
    }
    const timer = window.setTimeout(show, Math.max(0, owner.notice.showAt - now))
    return () => window.clearTimeout(timer)
  }, [
    active,
    actions,
    description,
    error,
    hidden,
    key,
    label,
    localeCode,
    notification,
    pending,
    receipt?.evidence,
    receipt?.message,
    refreshUnavailable,
    signature,
    target,
    token.motion,
    uncertain,
  ])

  return null
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
      notificationKey={`soha-operation-${latest.mutation.submittedAt}-${latest.label}-${latest.target ?? ''}`}
      submittedAt={latest.mutation.submittedAt}
      pending={latest.mutation.isPending}
      error={latest.mutation.error}
      receipt={latest.mutation.isSuccess ? latest.receipt : undefined}
    />
  ) : null
}
