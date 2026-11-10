import { Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { workbenchQueries } from '../queries'
import { isReadOnlyAgentRun, isRunningExternalAgentRun } from '../agent-run-replay'

export function MessageSpecialist({
  id,
  sessionId,
  readOnly = false,
}: {
  id: string
  sessionId: string
  readOnly?: boolean
}) {
  const query = useQuery({
    ...workbenchQueries.agentRuns.session(sessionId),
    refetchInterval: (state) =>
      !readOnly &&
      state.state.data?.data.some(
        (run) =>
          run.id === id &&
          !isReadOnlyAgentRun(run) &&
          (isRunningExternalAgentRun(run) ||
            (run.status === 'canceled' && run.output?.cancellationPending === true)),
      )
        ? 2000
        : false,
  })
  const run = query.data?.data.find((item) => item.id === id)
  if (!run)
    return (
      <Typography.Text type="secondary">
        {query.isError ? '暂时无法读取专业任务状态。' : '正在读取专业任务状态…'}
      </Typography.Text>
    )
  const label: Record<string, string> = {
    queued: '等待专业助手',
    running: '专业核验中',
    completed: '专业核验完成',
    canceled: '已取消',
    failed: '核验失败',
    callback_timeout: '核验超时',
  }
  const historical = readOnly || isReadOnlyAgentRun(run)
  const stopping =
    !historical && run.status === 'canceled' && run.output?.cancellationPending === true
  const answer = run.output?.answer ?? run.output?.summary
  return (
    <div>
      <Tag
        color={
          historical
            ? 'default'
            : run.status === 'completed'
              ? 'success'
              : isRunningExternalAgentRun(run) || stopping
                ? 'processing'
                : 'default'
        }
      >
        {historical
          ? '旧版专业任务记录'
          : stopping
            ? '等待停止确认'
            : (label[run.status] ?? run.status)}
      </Tag>
      <Typography.Paragraph type="secondary">
        独立上下文 · 只读权限 · {historical ? '' : '随主任务取消 · '}已调用{' '}
        {run.toolExecutions?.length ?? 0} 次工具
      </Typography.Paragraph>
      {typeof answer === 'string' && answer ? (
        <Typography.Paragraph>{answer}</Typography.Paragraph>
      ) : null}
      {run.errorMessage ? (
        <Typography.Paragraph type="danger">{run.errorMessage}</Typography.Paragraph>
      ) : null}
    </div>
  )
}
