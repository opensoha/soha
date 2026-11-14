import { App, Popconfirm, Space, Typography } from 'antd'
import { FileTextOutlined, RedoOutlined, StopOutlined } from '@ant-design/icons'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import type { ComputeTaskView } from '@opensoha/contracts/gen/ts/sohaapi'
import { ManagementIconButton } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { localeText, useI18n } from '@/i18n'
import { ComputeMutationFeedback } from '../feedback'
import { computeMutations } from '../mutations'

const { Text } = Typography

export function latestTaskForResource(
  tasks: ComputeTaskView[],
  resourceKind: string,
  resourceId: string,
) {
  return tasks.find((task) =>
    task.resources.some((resource) => resource.kind === resourceKind && resource.id === resourceId),
  )
}

export function ResourceTaskActions({
  task,
  resourceKind,
  resourceId,
}: {
  task?: ComputeTaskView
  resourceKind: string
  resourceId: string
}) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const cancelMutation = useMutation(computeMutations.cancelTask(queryClient))
  const retryMutation = useMutation(computeMutations.retryTask(queryClient))

  if (!task) return <Text type="secondary">-</Text>

  const openLogs = () => {
    const search = new URLSearchParams({
      domain: task.domain,
      resourceKind,
      resourceId,
      taskId: task.id,
      view: 'logs',
    })
    navigate(`/compute/tasks/operations?${search.toString()}`)
  }

  const mutate = (action: 'cancel' | 'retry') => {
    if (cancelMutation.isPending || retryMutation.isPending) return
    const mutation = action === 'cancel' ? cancelMutation : retryMutation
    mutation.mutate(
      { domain: task.domain, taskId: task.id },
      {
        onSuccess: () =>
          void message.success(
            action === 'cancel'
              ? localeText(localeCode, '取消请求已提交', 'Cancellation requested')
              : localeText(localeCode, '重试请求已提交', 'Retry requested'),
          ),
      },
    )
  }

  return (
    <div>
      <Space size={4} wrap={false}>
        <StatusTag value={task.normalizedStatus} />
        {task.availableActions.includes('logs') ? (
          <ManagementIconButton
            aria-label={localeText(localeCode, '查看最近任务日志', 'View latest task logs')}
            size="small"
            tooltip={localeText(localeCode, '查看日志', 'View logs')}
            icon={<FileTextOutlined />}
            onClick={openLogs}
          />
        ) : null}
        {task.availableActions.includes('cancel') ? (
          <Popconfirm
            title={localeText(localeCode, '确认取消任务？', 'Cancel this task?')}
            onConfirm={() => mutate('cancel')}
          >
            <ManagementIconButton
              aria-label={localeText(localeCode, '取消最近任务', 'Cancel latest task')}
              size="small"
              danger
              tooltip={localeText(localeCode, '取消', 'Cancel')}
              icon={<StopOutlined />}
              disabled={cancelMutation.isPending || retryMutation.isPending}
              loading={cancelMutation.isPending}
            />
          </Popconfirm>
        ) : null}
        {task.availableActions.includes('retry') ? (
          <Popconfirm
            title={localeText(localeCode, '确认重试任务？', 'Retry this task?')}
            onConfirm={() => mutate('retry')}
          >
            <ManagementIconButton
              aria-label={localeText(localeCode, '重试最近任务', 'Retry latest task')}
              size="small"
              tooltip={localeText(localeCode, '重试', 'Retry')}
              icon={<RedoOutlined />}
              disabled={cancelMutation.isPending || retryMutation.isPending}
              loading={retryMutation.isPending}
            />
          </Popconfirm>
        ) : null}
      </Space>
      <ComputeMutationFeedback
        operations={[
          {
            label: localeText(localeCode, '取消任务', 'Cancel task'),
            mutation: cancelMutation,
            target: cancelMutation.variables?.taskId,
            receipt: {
              message: localeText(localeCode, '取消请求已提交', 'Cancellation requested'),
            },
          },
          {
            label: localeText(localeCode, '重试任务', 'Retry task'),
            mutation: retryMutation,
            target: retryMutation.variables?.taskId,
            receipt: { message: localeText(localeCode, '重试请求已提交', 'Retry requested') },
          },
        ]}
      />
    </div>
  )
}
