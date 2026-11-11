import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Collapse, Space, Typography } from 'antd'
import { CheckOutlined, ClockCircleOutlined, CloseOutlined } from '@ant-design/icons'
import { localeText, useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { virtualizationQueries } from '../queries'
import { operationKindLabel, virtualMachineDisplayStatus } from '../virtualization-model'
import type { VirtualMachine, VirtualizationOperation } from '../virtualization-types'
import { vmOperationState, vmRecordMatchesOperation } from './operation-feedback-model'

export function VMOperationFeedback({
  operation,
  vm,
  onHistory,
  canViewProgress,
}: {
  operation: VirtualizationOperation
  vm?: VirtualMachine
  onHistory: () => void
  canViewProgress: boolean
}) {
  const { localeCode } = useI18n()
  const [logsOpen, setLogsOpen] = useState(false)
  const state = vmOperationState(operation.status)
  const active = canViewProgress && (state === 'queued' || state === 'active')
  const recordMatches = vmRecordMatchesOperation(operation, vm)
  const logsQuery = useQuery({
    ...virtualizationQueries.operationLogs(operation.id, logsOpen && canViewProgress),
    refetchInterval: (query) =>
      active && logsOpen && query.state.status !== 'error' ? 4_000 : false,
    retry: false,
  })
  const label = !canViewProgress
    ? localeText(
        localeCode,
        '请求已受理，任务进度不可见',
        'Request accepted, task progress is unavailable',
      )
    : {
        queued: localeText(localeCode, '已受理，等待执行', 'Accepted, waiting to execute'),
        active:
          operation.status === 'canceling'
            ? localeText(localeCode, '正在请求取消', 'Cancellation requested')
            : localeText(localeCode, '任务执行中', 'Task in progress'),
        success: recordMatches
          ? localeText(
              localeCode,
              '任务完成，资源记录已更新',
              'Task completed, resource record updated',
            )
          : localeText(
              localeCode,
              '任务完成，资源记录待核对',
              'Task completed, resource record needs checking',
            ),
        error: localeText(localeCode, '任务执行失败', 'Task failed'),
        uncertain: localeText(
          localeCode,
          '任务回调超时，执行结果待核对',
          'Task callback timed out, check the outcome',
        ),
        canceled: localeText(localeCode, '任务已取消', 'Task canceled'),
        unknown: localeText(
          localeCode,
          `任务状态未知 · ${operation.status || '-'}`,
          `Unknown task state · ${operation.status || '-'}`,
        ),
      }[state]
  return (
    <section
      className={`soha-vm-operation is-${state}`}
      aria-label={localeText(localeCode, '当前操作任务', 'Current operation')}
    >
      <div className="soha-vm-operation-heading">
        <div className="soha-vm-operation-main">
          <span className="soha-vm-operation-icon" aria-hidden="true">
            {state === 'success' ? (
              <CheckOutlined />
            ) : state === 'error' ? (
              <CloseOutlined />
            ) : (
              <ClockCircleOutlined />
            )}
          </span>
          <div>
            <div className="soha-vm-eyebrow">{operationKindLabel(operation, localeCode)}</div>
            <h2 role="status" aria-live="polite">
              {active ? <span className="soha-vm-live-dot" /> : null}
              {label}
            </h2>
          </div>
        </div>
        {canViewProgress ? (
          <Button size="small" onClick={onHistory}>
            {localeText(localeCode, '任务历史', 'Task history')}
          </Button>
        ) : null}
      </div>
      <div className="soha-vm-operation-evidence">
        <span>
          {localeText(localeCode, '任务', 'Task')}{' '}
          <Typography.Text copyable>{operation.id}</Typography.Text>
        </span>
        <span>
          {localeText(localeCode, '更新', 'Updated')}{' '}
          {formatDateTime(operation.updatedAt || operation.createdAt)}
        </span>
      </div>
      <ol
        className="soha-vm-task-stages"
        aria-label={localeText(localeCode, '任务进程', 'Task stages')}
      >
        <li className="is-done">
          <span>1</span>
          {localeText(localeCode, '请求已受理', 'Request accepted')}
        </li>
        <li
          className={
            state === 'active'
              ? 'is-current'
              : state === 'success'
                ? 'is-done'
                : state === 'error'
                  ? 'is-error'
                  : ''
          }
        >
          <span>2</span>
          {localeText(localeCode, 'Provider 执行', 'Provider execution')}
        </li>
        <li className={state === 'success' ? 'is-done' : state === 'error' ? 'is-error' : ''}>
          <span>3</span>
          {localeText(localeCode, '核对任务结果', 'Check task outcome')}
        </li>
      </ol>
      <p className="soha-vm-operation-message">
        {operation.message ||
          localeText(localeCode, '暂未返回任务消息。', 'No task message returned yet.')}
      </p>
      {state === 'success' ? (
        <p className="soha-vm-record-note">
          {localeText(localeCode, '当前 VM 记录', 'Current VM record')}：
          {virtualMachineDisplayStatus(vm) || '-'} ·{' '}
          {localeText(
            localeCode,
            '运行结果仍以 Provider 后续同步为准，任务完成不代表 Guest 已就绪。',
            'Provider synchronization determines the runtime result. A completed task does not establish guest readiness.',
          )}
        </p>
      ) : null}
      {['uncertain', 'unknown', 'canceled'].includes(state) ? (
        <p className="soha-vm-record-note">
          {localeText(
            localeCode,
            '请先核对任务日志和资源记录。取消或超时不代表 Provider 的操作已撤销。',
            'Check the task logs and resource record. Cancellation or timeout does not establish that the provider action was undone.',
          )}
        </p>
      ) : null}
      {!canViewProgress ? (
        <p className="soha-vm-record-note">
          {localeText(
            localeCode,
            '当前账号没有虚拟机任务查看权限。可刷新资源记录核对电源状态；任务终态不可见时，本页不会再次提交电源操作。',
            'Your account cannot view VM tasks. You can refresh the resource record, but further power requests remain blocked while the task outcome is unavailable.',
          )}
        </p>
      ) : (
        <Collapse
          ghost
          size="small"
          activeKey={logsOpen ? ['logs'] : []}
          onChange={(keys) => setLogsOpen(keys.length > 0)}
          items={[
            {
              key: 'logs',
              label: localeText(localeCode, '任务日志与证据', 'Task logs and evidence'),
              children: (
                <>
                  {logsQuery.isError ? (
                    <Alert
                      type="warning"
                      showIcon
                      title={localeText(localeCode, '日志未更新', 'Logs not updated')}
                      description={logsQuery.error.message}
                      action={
                        <Button size="small" onClick={() => void logsQuery.refetch()}>
                          {localeText(localeCode, '重试读取', 'Retry reading')}
                        </Button>
                      }
                    />
                  ) : null}
                  {logsQuery.isLoading ? (
                    <p>{localeText(localeCode, '正在读取日志…', 'Reading logs…')}</p>
                  ) : null}
                  {logsQuery.data ? (
                    <pre className="soha-vm-task-log">
                      {logsQuery.data
                        .map(
                          (item) =>
                            `[${formatDateTime(item.createdAt)}] ${item.logLevel || 'info'} ${item.message}`,
                        )
                        .join('\n') ||
                        localeText(localeCode, '暂未返回任务日志。', 'No task logs returned yet.')}
                    </pre>
                  ) : null}
                  <Space>
                    <Typography.Text type="secondary">
                      {localeText(
                        localeCode,
                        '此处只读取证据，不会重新提交电源请求。',
                        'Reading evidence does not resubmit a power request.',
                      )}
                    </Typography.Text>
                  </Space>
                </>
              ),
            },
          ]}
        />
      )}
    </section>
  )
}
