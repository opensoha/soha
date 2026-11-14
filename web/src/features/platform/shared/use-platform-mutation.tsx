import { requestErrorMessage } from '@/components/request-feedback'
import {
  useOperationMutation,
  type OperationMutationFeedback,
} from '@/components/use-operation-mutation'
import { localeText, useI18n } from '@/i18n'
import { writeOutcomeUnknown } from '@/utils/query-feedback'
import { type UseMutationOptions } from '@tanstack/react-query'

export function usePlatformMutation<TData, TVariables = void, TContext = unknown>(
  options: UseMutationOptions<TData, Error, TVariables, TContext>,
  feedback: OperationMutationFeedback<TData, TVariables>,
) {
  const { localeCode } = useI18n()
  const label = localeText(localeCode, ...feedback.label)
  return useOperationMutation(options, {
    ...feedback,
    receipt:
      feedback.receipt ??
      (() => ({
        message: localeText(localeCode, `${label}：请求已返回`, `${label}: request returned`),
        evidence: feedback.readOnly
          ? localeText(localeCode, '预检未执行资源变更。', 'Preflight did not change resources.')
          : localeText(
              localeCode,
              '请核对下方资源记录，实际就绪状态以集群观察结果为准。',
              'Review the resource records. Readiness follows the observed cluster state.',
            ),
      })),
  })
}

export function platformBatchUnknown(results: readonly PromiseSettledResult<unknown>[]) {
  const uncertain = results.find(
    (item) => item.status === 'rejected' && writeOutcomeUnknown(item.reason),
  )
  return uncertain?.status === 'rejected' ? (uncertain.reason as Error) : undefined
}

export function platformBatchReceipt(
  results: readonly PromiseSettledResult<unknown>[],
  targets: string[],
  localeCode: 'zh_CN' | 'en_US',
) {
  const accepted = results.filter((item) => item.status === 'fulfilled').length
  return {
    message: localeText(
      localeCode,
      `批量请求已返回：${accepted} 项成功，${results.length - accepted} 项失败或待核对`,
      `Batch returned: ${accepted} requests succeeded; ${results.length - accepted} failed or need checking`,
    ),
    evidence: (
      <ul>
        {results.map((item, index) => (
          <li key={`${index}/${targets[index]}`}>
            {targets[index]} ·{' '}
            {item.status === 'fulfilled'
              ? localeText(
                  localeCode,
                  '请求成功，实际状态以资源记录为准',
                  'Request succeeded; check resource state',
                )
              : `${localeText(localeCode, writeOutcomeUnknown(item.reason) ? '结果不明' : '请求失败', writeOutcomeUnknown(item.reason) ? 'Outcome unknown' : 'Request failed')} · ${requestErrorMessage(item.reason instanceof Error ? item.reason : new Error(String(item.reason)), localeCode)}`}
          </li>
        ))}
      </ul>
    ),
  }
}

export function platformResourceTarget(target: {
  scope?: { clusterId: string | null; namespace?: string | null }
  clusterId?: string
  namespace?: string | null
  name?: string
  releaseName?: string
  resourceName?: string
  sessionId?: string
}) {
  return [
    target.scope?.clusterId ?? target.clusterId,
    target.scope?.namespace ?? target.namespace,
    target.name ?? target.releaseName ?? target.resourceName ?? target.sessionId,
  ]
    .filter(Boolean)
    .join(' / ')
}
