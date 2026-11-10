import type { WorkloadContainer } from '@/types'
import type { PodContainerStatus } from './types'

export function podContainerTerminated(container: Pick<WorkloadContainer, 'state'> | undefined) {
  return container?.state?.split(':', 1)[0] === 'terminated'
}

export function containerTone(status: PodContainerStatus) {
  if (status.state === 'running')
    return status.kind !== 'init' && status.ready ? 'success' : 'warning'
  if (status.state === 'waiting') {
    return /error|errimagepull|backoff|invalidimagename|containercannotrun/i.test(
      status.reason ?? '',
    )
      ? 'danger'
      : 'warning'
  }
  if (status.state === 'terminated') {
    if (status.exitCode === undefined) return 'unknown'
    if (status.exitCode !== 0) return 'danger'
    return status.kind === 'sidecar' ? 'warning' : 'success'
  }
  return 'unknown'
}

export function containerLabel(status: PodContainerStatus, zh: boolean) {
  const kind =
    status.kind === 'init'
      ? zh
        ? '初始化容器'
        : 'Init container'
      : status.kind === 'sidecar'
        ? zh
          ? '常驻初始化容器'
          : 'Restartable init container'
        : zh
          ? '容器'
          : 'Container'
  const state =
    status.state === 'running'
      ? status.ready
        ? zh
          ? '运行中，已就绪'
          : 'Running, ready'
        : zh
          ? '运行中，未就绪'
          : 'Running, not ready'
      : status.state === 'waiting'
        ? zh
          ? '等待启动'
          : 'Waiting'
        : status.state === 'terminated'
          ? status.exitCode === 0 && status.kind !== 'sidecar'
            ? zh
              ? '正常完成'
              : 'Completed'
            : status.exitCode === 0
              ? zh
                ? '已退出，等待重启'
                : 'Exited, awaiting restart'
              : zh
                ? '已终止'
                : 'Terminated'
          : zh
            ? '状态未知'
            : 'Unknown state'
  return [
    `${kind} ${status.name}：${state}`,
    status.reason,
    status.exitCode === undefined ? undefined : `${zh ? '退出码' : 'Exit code'} ${status.exitCode}`,
  ]
    .filter(Boolean)
    .join(' · ')
}
