import { Space, Typography } from 'antd'
import { Link } from 'react-router-dom'
import type { ComputeTaskDomain } from '@opensoha/contracts/gen/ts/sohaapi'
import { OperationFeedback as RequestOperationFeedback } from '@/components/request-feedback'
import type { OperationFeedbackProps } from '@/components/request-feedback'
import { latestSubmittedOperation } from '@/utils/query-feedback'
import { localeText, useI18n } from '@/i18n'

interface OperationReceipt {
  message: string
  taskId?: string
  taskIds?: string[]
  domain?: ComputeTaskDomain
  canViewTask?: boolean
}

interface ComputeOperation {
  label: string
  readOnly?: boolean
  target?: string
  mutation: { isPending: boolean; isSuccess: boolean; error: Error | null; submittedAt: number }
  receipt?: OperationReceipt
}

export function ComputeMutationFeedback({ operations }: { operations: ComputeOperation[] }) {
  const latest = latestSubmittedOperation(operations)
  return latest ? (
    <ComputeOperationFeedback
      label={latest.label}
      target={latest.target}
      pending={latest.mutation.isPending}
      error={latest.mutation.error}
      readOnly={latest.readOnly}
      receipt={latest.mutation.isSuccess ? latest.receipt : undefined}
    />
  ) : null
}

export function ComputeOperationFeedback({
  receipt,
  ...props
}: Omit<OperationFeedbackProps, 'receipt'> & { receipt?: OperationReceipt }) {
  const { localeCode } = useI18n()
  return (
    <RequestOperationFeedback
      {...props}
      receipt={
        receipt
          ? {
              message: receipt.message,
              evidence: (receipt.taskId ? [receipt.taskId] : (receipt.taskIds ?? [])).map(
                (taskId) => (
                  <Space wrap key={taskId}>
                    <Typography.Text copyable>{taskId}</Typography.Text>
                    {receipt.canViewTask && receipt.domain ? (
                      <Link
                        to={`/compute/tasks/operations?${new URLSearchParams({ domain: receipt.domain, taskId, view: 'logs' })}`}
                      >
                        {localeText(localeCode, '查看任务与证据', 'View task and evidence')}
                      </Link>
                    ) : (
                      <Typography.Text type="secondary">
                        {localeText(localeCode, '任务进度不可见', 'Task progress is unavailable')}
                      </Typography.Text>
                    )}
                  </Space>
                ),
              ),
            }
          : undefined
      }
    />
  )
}
