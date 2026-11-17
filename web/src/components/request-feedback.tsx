import { localeText, useI18n } from '@/i18n'
import { ApiError, claimApiErrorPresentation, claimApiReadPresentation } from '@/services/api-error'
import {
  latestSubmittedOperation,
  readableQueryData,
  writeOutcomeUnknown,
} from '@/utils/query-feedback'
import { useDocumentHidden } from '@/utils/use-document-hidden'
import { App, Button, Space, Typography, theme } from 'antd'
import { useCallback, useContext, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { ExclamationCircleOutlined } from '@ant-design/icons'
import { QueryClientContext } from '@tanstack/react-query'
import './request-feedback.css'

interface ReadState<T> {
  data?: T
  error: Error | null
  isError: boolean
  isFetching: boolean
  isEnabled?: boolean
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

const READ_RETRY_DELAYS = [15_000, 30_000, 60_000]
const READ_NOTICE_KEY = 'soha-read-feedback'
type ReadNotifier = ReturnType<typeof App.useApp>['notification']
type ReadOwner = {
  query: ReadState<unknown>
  label: string
  localeCode: 'zh_CN' | 'en_US'
  notification: ReadNotifier
  synthetic?: boolean
}
type ReadFailure = {
  key: string
  owners: Map<string, ReadOwner>
  error: Error | null
  attempts: number
  nextRetryAt?: number
  inFlight: boolean
  dismissed: boolean
  resolved: boolean
  releasePresentation?: () => void
}
const readFailures = new Map<string, ReadFailure>()
const readErrorKeys = new WeakMap<Error, string>()
let readNotifier: ReadNotifier | undefined

function readKey(error: Error | null, fallback: string) {
  if (error instanceof ApiError && error.path) return `${error.method ?? 'GET'}:${error.path}`
  if (!error) return fallback
  const key = readErrorKeys.get(error) ?? fallback
  readErrorKeys.set(error, key)
  return key
}

function deniedRead(error: Error | null) {
  return error instanceof ApiError && [401, 403].includes(error.status)
}

function readOwner(entry: ReadFailure) {
  return (
    [...entry.owners.values()].find((owner) => !owner.synthetic) ??
    entry.owners.values().next().value
  )
}

function readBusy(entry: ReadFailure) {
  return entry.inFlight || [...entry.owners.values()].some((owner) => owner.query.isFetching)
}

function canAutoRetry(entry: ReadFailure) {
  const owner = readOwner(entry)
  return (
    owner &&
    !owner.synthetic &&
    owner.query.refetch &&
    !(
      entry.error instanceof ApiError &&
      entry.error.method &&
      !['GET', 'HEAD'].includes(entry.error.method.toUpperCase())
    )
  )
}

function scheduleReadRetry(entry: ReadFailure) {
  if (
    !entry.dismissed &&
    !entry.resolved &&
    !readBusy(entry) &&
    canAutoRetry(entry) &&
    entry.attempts < READ_RETRY_DELAYS.length &&
    entry.nextRetryAt === undefined
  )
    entry.nextRetryAt = Date.now() + READ_RETRY_DELAYS[entry.attempts]
}

function responseReadErrors(response: unknown): Error[] {
  return (Array.isArray(response) ? response : [response])
    .filter((item) => item && typeof item === 'object' && 'isError' in item && item.isError)
    .map((item) => (item.error instanceof Error ? item.error : new Error('Reading failed')))
}

function cancelReadFallback(error: Error | null) {
  const release = claimApiErrorPresentation(error)
  release?.()
}

async function retryReads(entries: ReadFailure[]) {
  const requests = new Map<() => unknown, Promise<unknown>>()
  await Promise.all(
    entries.map(async (entry) => {
      const owner = readOwner(entry)
      if (!owner?.query.refetch || readBusy(entry) || entry.dismissed || entry.resolved) return
      entry.inFlight = true
      entry.attempts += 1
      entry.nextRetryAt = undefined
      publishReadNotice()
      try {
        let request = requests.get(owner.query.refetch)
        if (!request) {
          request = Promise.resolve().then(owner.query.refetch)
          requests.set(owner.query.refetch, request)
        }
        const result = await request
        if (readFailures.get(entry.key) !== entry) return
        const outcomes = Array.isArray(result) ? result : [result]
        const errors = responseReadErrors(outcomes)
        errors.forEach(cancelReadFallback)
        if (errors.length) {
          const scoped = errors.filter((error) => readKey(error, entry.key) === entry.key)
          const candidates = scoped.length ? scoped : errors
          entry.resolved =
            candidates.every(deniedRead) ||
            Boolean(
              Array.isArray(result) &&
              entry.error instanceof ApiError &&
              entry.error.path &&
              !scoped.length,
            )
          entry.error = candidates.find((error) => !deniedRead(error)) ?? candidates[0]
        } else if (
          outcomes.length > 0 &&
          outcomes.every(
            (item) =>
              item && typeof item === 'object' && 'isError' in item && item.isError === false,
          )
        ) {
          entry.resolved = true
        }
        // Void callbacks settle through their Query observer; they are not success evidence.
      } catch (cause) {
        if (readFailures.get(entry.key) !== entry) return
        entry.error = cause instanceof Error ? cause : new Error(String(cause))
        cancelReadFallback(entry.error)
        entry.resolved = deniedRead(entry.error)
      } finally {
        entry.inFlight = false
        if (readFailures.get(entry.key) === entry) {
          if (entry.resolved && [...entry.owners.values()].every((owner) => owner.synthetic)) {
            entry.releasePresentation?.()
            readFailures.delete(entry.key)
          }
          scheduleReadRetry(entry)
          publishReadNotice()
        }
      }
    }),
  )
}

function ReadFailureNotice({ entries, owner }: { entries: ReadFailure[]; owner: ReadOwner }) {
  const hidden = useDocumentHidden()
  const [now, setNow] = useState(Date.now)
  const currentEntries = useRef(entries)
  currentEntries.current = entries
  const scheduled = entries.some((entry) => entry.nextRetryAt !== undefined)
  const { localeCode } = owner
  useEffect(() => {
    if (!scheduled) return
    const timer = window.setInterval(() => {
      const time = Date.now()
      setNow(time)
      if (document.hidden) return
      const due = currentEntries.current.filter(
        (entry) =>
          !readBusy(entry) &&
          !entry.dismissed &&
          !entry.resolved &&
          entry.nextRetryAt !== undefined &&
          entry.nextRetryAt <= time,
      )
      if (due.length) void retryReads(due)
    }, 1_000)
    return () => window.clearInterval(timer)
  }, [scheduled])
  const retryable = entries.filter((entry) => readOwner(entry)?.query.refetch && !readBusy(entry))
  const busy = entries.some(readBusy)
  const deadlines = entries.flatMap((entry) =>
    entry.nextRetryAt === undefined ? [] : [entry.nextRetryAt],
  )
  const seconds = deadlines.length
    ? Math.max(0, Math.ceil((Math.min(...deadlines) - now) / 1_000))
    : undefined
  return (
    <Space orientation="vertical" size={8} className="soha-read-notice-content">
      {entries.length > 1
        ? entries.map((entry) => (
            <Typography.Text key={entry.key}>
              {readOwner(entry)?.label}
              {localeText(localeCode, '读取失败', ': read failed')}
            </Typography.Text>
          ))
        : null}
      {entries.some((entry) => {
        const query = readOwner(entry)?.query
        return query && readableQueryData(query) !== undefined
      }) ? (
        <Typography.Text type="secondary">
          {localeText(
            localeCode,
            '已保留可展示的上次结果。',
            'Available previous results are retained.',
          )}
        </Typography.Text>
      ) : null}
      <Typography.Text type="secondary" aria-live="off">
        {busy
          ? localeText(localeCode, '正在重新读取…', 'Retrying…')
          : hidden && seconds !== undefined
            ? localeText(
                localeCode,
                '页面在后台，自动重试已暂停。',
                'Automatic retry is paused while the page is hidden.',
              )
            : seconds !== undefined
              ? localeText(
                  localeCode,
                  `${seconds} 秒后自动重试`,
                  `Retrying automatically in ${seconds}s`,
                )
              : entries.some((entry) => readOwner(entry)?.query.refetch)
                ? localeText(
                    localeCode,
                    '自动重试已暂停，可手动重试。',
                    'Automatic retry is paused. You can retry manually.',
                  )
                : localeText(localeCode, '请稍后重新打开此页面。', 'Reopen this page later.')}
      </Typography.Text>
      <Space wrap>
        {entries.some((entry) => readOwner(entry)?.query.refetch) ? (
          <Button
            size="small"
            loading={busy}
            disabled={!retryable.length}
            aria-label={localeText(
              localeCode,
              entries.length === 1 ? `重试读取${owner.label}` : '重试读取失败数据',
              entries.length === 1 ? `Retry reading ${owner.label}` : 'Retry failed reads',
            )}
            onClick={() => void retryReads(retryable)}
          >
            {localeText(localeCode, '立即重试', 'Retry now')}
          </Button>
        ) : null}
        <details className="soha-operation-feedback-details">
          <summary>{localeText(localeCode, '查看详情', 'View details')}</summary>
          {entries.map((entry) => (
            <div key={entry.key}>
              {readOwner(entry)?.label}: {requestErrorMessage(entry.error, localeCode)}
            </div>
          ))}
        </details>
      </Space>
    </Space>
  )
}

function publishReadNotice() {
  const entries = [...readFailures.values()].filter(
    (entry) => !entry.dismissed && !entry.resolved && entry.owners.size,
  )
  if (!entries.length) {
    readNotifier?.destroy?.(READ_NOTICE_KEY)
    if (!readFailures.size) readNotifier = undefined
    return
  }
  const owner = readOwner(entries[0])!
  if (readNotifier && readNotifier.open !== owner.notification.open)
    readNotifier.destroy?.(READ_NOTICE_KEY)
  readNotifier = owner.notification
  const cached = readableQueryData(owner.query) !== undefined
  readNotifier.open?.({
    key: READ_NOTICE_KEY,
    className: 'soha-read-feedback-notification',
    title: localeText(
      owner.localeCode,
      entries.length > 1
        ? '部分数据读取失败'
        : cached
          ? `${owner.label}未更新，正在展示上次读取结果`
          : `${owner.label}读取失败`,
      entries.length > 1
        ? 'Some data could not be read'
        : cached
          ? `${owner.label}: showing the previous result`
          : `${owner.label}: read failed`,
    ),
    description: <ReadFailureNotice entries={entries} owner={owner} />,
    type: 'warning',
    role: 'status',
    placement: 'topRight',
    style: { width: 'min(384px, calc(100vw - 48px))', background: 'var(--soha-bg-surface)' },
    duration: 0,
    onClose: () => {
      entries.forEach((entry) => {
        entry.dismissed = true
        entry.nextRetryAt = undefined
      })
    },
  })
}

function leaveReadFailure(key: string | undefined, ownerId: string) {
  if (!key) return
  const entry = readFailures.get(key)
  if (!entry) return
  entry.owners.delete(ownerId)
  if (!entry.owners.size) {
    entry.releasePresentation?.()
    readFailures.delete(key)
  }
  publishReadNotice()
}

function ownReadFailure(key: string, ownerId: string, owner: ReadOwner, explicit = false) {
  let entry = readFailures.get(key)
  if (!entry) {
    entry = {
      key,
      owners: new Map(),
      error: owner.query.error,
      attempts: 0,
      inFlight: false,
      dismissed: false,
      resolved: false,
      releasePresentation: claimApiReadPresentation(owner.query.error),
    }
    readFailures.set(key, entry)
  }
  if (!owner.synthetic) {
    for (const [id, previous] of entry.owners) if (previous.synthetic) entry.owners.delete(id)
  }
  if (entry.resolved && owner.query.isError && owner.query.error !== entry.error) {
    entry.resolved = false
    entry.dismissed = false
    entry.attempts = 0
    entry.nextRetryAt = undefined
  }
  entry.owners.set(ownerId, owner)
  if (owner.query.isError) entry.error = owner.query.error
  if (explicit) {
    entry.dismissed = false
    entry.resolved = false
    entry.nextRetryAt = undefined
  }
  scheduleReadRetry(entry)
  publishReadNotice()
}

// An explicit multi-query refresh joins the same notice instead of adding another toast.
export function useReadRefreshFeedback() {
  const { localeCode } = useI18n()
  const { notification } = App.useApp()
  const queryClient = useContext(QueryClientContext)
  const id = useId()
  const refreshing = useRef(false)
  const mounted = useRef(true)
  const synthetic = useRef(new Set<string>())
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      for (const key of synthetic.current) leaveReadFailure(key, id)
    }
  }, [id])
  return useCallback(
    async (targets: readonly ReadRefreshTarget[]) => {
      if (refreshing.current || !targets.length) return
      refreshing.current = true
      try {
        const queryOwners = (error: Error) =>
          queryClient?.getQueryCache().findAll({
            predicate: (query) => query.state.error === error,
          }) ?? []
        const results = await Promise.all(
          targets.map(async ({ label, refetch }, index) => {
            try {
              return responseReadErrors(await refetch()).map((error) => ({
                error,
                label,
                refetch,
                index,
                queries: queryOwners(error),
              }))
            } catch (cause) {
              const error = cause instanceof Error ? cause : new Error(String(cause))
              return [{ error, label, refetch, index, queries: queryOwners(error) }]
            }
          }),
        )
        if (!mounted.current) return
        for (const key of synthetic.current) leaveReadFailure(key, id)
        synthetic.current.clear()
        for (const { error, label, refetch, index, queries } of results.flat()) {
          cancelReadFallback(error)
          if (deniedRead(error)) continue
          if (queries.length && queries.every((query) => !query.isActive())) continue
          const key = readKey(error, `${id}:${index}`)
          const entry = readFailures.get(key)
          if (entry && [...entry.owners.values()].some((owner) => !owner.synthetic)) {
            entry.dismissed = false
            entry.resolved = false
            entry.error = error
            scheduleReadRetry(entry)
            publishReadNotice()
          } else {
            synthetic.current.add(key)
            // No live Query owner means no permission/scope lifecycle for automatic retries.
            ownReadFailure(
              key,
              id,
              {
                label,
                localeCode,
                notification,
                synthetic: true,
                query: { error, isError: true, isFetching: false, refetch },
              },
              true,
            )
          }
        }
      } finally {
        refreshing.current = false
      }
    },
    [id, localeCode, notification, queryClient],
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
  const { notification } = App.useApp()
  const id = useId()
  const ownedKey = useRef<string>()
  const allowed = canRead && query.isEnabled !== false
  const denied = deniedRead(query.error)
  useInlineApiError(allowed && query.isError ? query.error : null)
  useEffect(() => {
    if (allowed && !denied && (query.isError || (query.isFetching && ownedKey.current))) {
      const key = readKey(query.error, ownedKey.current ?? id)
      if (ownedKey.current !== key) leaveReadFailure(ownedKey.current, id)
      ownedKey.current = key
      ownReadFailure(key, id, { query, label, localeCode, notification })
    } else {
      leaveReadFailure(ownedKey.current, id)
      ownedKey.current = undefined
    }
  })
  useEffect(() => () => leaveReadFailure(ownedKey.current, id), [id])
  if (!allowed || !query.isError || !denied) return null
  const authExpired = query.error instanceof ApiError && query.error.status === 401
  return (
    <section
      className="soha-read-feedback"
      role="alert"
      aria-label={localeText(localeCode, `${label}读取状态`, `${label} reading status`)}
    >
      <ExclamationCircleOutlined className="soha-read-feedback-icon" aria-hidden="true" />
      <span className="soha-read-feedback-title">
        {localeText(
          localeCode,
          authExpired ? `${label}登录状态失效` : `${label}无访问权限`,
          authExpired ? `${label}: sign-in expired` : `${label}: access denied`,
        )}
      </span>
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
